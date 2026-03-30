package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/session"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type Runtime struct {
	Store          *store.Store
	Router         *router.Router
	SessionManager *session.Manager
	SecurityProxy  *executor.SecurityProxy
	ServerManager  *executor.ServerManager
	cancelReaper   context.CancelFunc
}

type RuntimeConfig struct {
	ResponsesDir    string
	SoftIdleTimeout time.Duration
	HardIdleTimeout time.Duration
	ReaperInterval  time.Duration
}

type messageStore interface {
	CreateMessage(ctx context.Context, msg *core.Message) error
}

func NewRuntime(st *store.Store, tmux executor.TmuxRunner, cfg RuntimeConfig) *Runtime {
	proxy := executor.NewSecurityProxy()
	serverMgr := executor.NewServerManager()
	registry := executor.NewHarnessRegistry(tmux, serverMgr, proxy)
	cancelReaper := serverMgr.StartReaper(time.Minute, 10*time.Minute)

	manager := session.NewManager(st, registry, proxy, session.ManagerConfig{
		ResponsesDir:    cfg.ResponsesDir,
		SoftIdleTimeout: cfg.SoftIdleTimeout,
		HardIdleTimeout: cfg.HardIdleTimeout,
		ReaperInterval:  cfg.ReaperInterval,
	})

	return &Runtime{
		Store:          st,
		Router:         router.NewRouter(st, manager),
		SessionManager: manager,
		SecurityProxy:  proxy,
		ServerManager:  serverMgr,
		cancelReaper:   cancelReaper,
	}
}

// Shutdown stops the server manager reaper and shuts down all managed servers.
func (r *Runtime) Shutdown() {
	if r.cancelReaper != nil {
		r.cancelReaper()
	}
	if r.ServerManager != nil {
		r.ServerManager.Shutdown()
	}
}

// NewRuntimeWithRegistry creates a Runtime using a pre-built adapter registry.
// This is intended for tests that need to inject mock adapters.
func NewRuntimeWithRegistry(st *store.Store, registry session.AdapterRegistry, cfg RuntimeConfig) *Runtime {
	proxy := executor.NewSecurityProxy()
	manager := session.NewManager(st, registry, proxy, session.ManagerConfig{
		ResponsesDir:    cfg.ResponsesDir,
		SoftIdleTimeout: cfg.SoftIdleTimeout,
		HardIdleTimeout: cfg.HardIdleTimeout,
		ReaperInterval:  cfg.ReaperInterval,
	})

	return &Runtime{
		Store:          st,
		Router:         router.NewRouter(st, manager),
		SessionManager: manager,
		SecurityProxy:  proxy,
	}
}

func StartBackground(ctx context.Context, st messageStore, manager *session.Manager, channel core.Channel) *sync.WaitGroup {
	wg := StartManagerBackground(ctx, manager)
	wg.Add(1)

	go func() {
		defer wg.Done()
		forwardResponses(ctx, st, manager, channel)
	}()

	return wg
}

func StartManagerBackground(ctx context.Context, manager *session.Manager) *sync.WaitGroup {
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := manager.Run(ctx); err != nil && err != ctx.Err() {
			slog.Error("session manager error", "error", err)
		}
	}()

	return &wg
}

func forwardResponses(ctx context.Context, st messageStore, manager *session.Manager, channel core.Channel) {
	events := manager.ResponseCh()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}

			reqID := core.NewRequestID()
			slog.Debug("forwarding response", "req", reqID, "session", event.SessionName, "sender", event.SenderID)

			content := event.Content
			streamed := false

			if event.Events != nil {
				sc, ok := channel.(core.StreamingChannel)
				if ok {
					chatID, err := strconv.ParseInt(event.SenderID, 10, 64)
					if err != nil {
						slog.Warn("failed to parse chatID for streaming", "senderID", event.SenderID, "error", err)
						drainEvents(event.Events)
					} else {
						replyToID, _ := strconv.Atoi(event.ReplyToID)
						result, err := sc.SendStreaming(ctx, chatID, replyToID, event.AgentSessionID, event.Events)
						if err != nil {
							slog.Warn("streaming delivery failed", "error", err, "session", event.SessionName, "sender", event.SenderID)
							drainEvents(event.Events)
							continue
						}
						content = result.Text
						streamed = true

						if event.ResponseFile != "" {
							if err := executor.AppendMessage(event.ResponseFile, executor.ResponseMessage{
								Role:    "agent",
								Content: content,
							}); err != nil {
								slog.Warn("failed to persist streaming response to file",
									"path", event.ResponseFile, "error", err)
							}
							// Tell the watcher this file was already delivered
							// via streaming so it does not re-emit the event.
							manager.MarkResponseDelivered(event.ResponseFile)
						}

						// Compute MsgIdx from response file after writing.
						msgIdx := agentMessageIndex(event.ResponseFile)

						// Route through SendResponse if channel supports it.
						if rs, ok := channel.(core.ResponseSender); ok && content != "" {
							params := core.ResponseParams{
								ChatID:       chatID,
								ReplyToID:    event.ReplyToID,
								Content:      content,
								SessionName:  event.SessionName,
								SessionSlug:  event.SessionSlug,
								MsgIdx:       msgIdx,
								Workspace:    event.Workspace,
								InputTokens:  result.InputTokens,
								OutputTokens: result.OutputTokens,
								Cost:         result.Cost,
							}
							if err := rs.SendResponse(ctx, params); err != nil {
								slog.Warn("SendResponse failed, falling back to Send", "error", err)
								_ = channel.Send(ctx, core.OutboundMessage{
									Channel:     event.Channel,
									RecipientID: event.SenderID,
									Content:     content,
								})
							}
						} else if content != "" {
							// Channel does not implement ResponseSender; use plain Send.
							if err := channel.Send(ctx, core.OutboundMessage{
								Channel:     event.Channel,
								RecipientID: event.SenderID,
								Content:     content,
							}); err != nil {
								slog.Warn("failed to send streamed response", "error", err)
							}
						}
					}
				} else {
					slog.Info("channel does not support streaming, falling back to Send()",
						"channel", fmt.Sprintf("%T", channel))
					drainEvents(event.Events)
				}
			}

			if !streamed {
				if err := channel.Send(ctx, core.OutboundMessage{
					Channel:     event.Channel,
					RecipientID: event.SenderID,
					Content:     content,
				}); err != nil {
					slog.Warn("failed to send response event", "error", err)
					continue
				}
			}

			if err := st.CreateMessage(ctx, &core.Message{
				Channel:   event.Channel,
				SenderID:  event.SenderID,
				Content:   content,
				Direction: core.OutboundDirection,
				Status:    core.StatusDone,
			}); err != nil {
				slog.Warn("failed to persist response event", "error", err)
			}

			slog.Debug("response delivered", "req", reqID)
		}
	}
}

// drainEvents consumes all remaining events from a channel to prevent goroutine leaks.
func drainEvents(events <-chan core.AgentEvent) {
	for range events {
	}
}

// agentMessageIndex returns the zero-indexed position of the last agent message
// in the response file. Returns 0 if the file cannot be read or has no agent messages.
func agentMessageIndex(responseFile string) int {
	if responseFile == "" {
		return 0
	}
	rf, err := executor.ReadResponseFile(responseFile)
	if err != nil {
		slog.Warn("response file unreadable", "file", responseFile, "err", err)
		return 0
	}
	count := 0
	for _, m := range rf.Messages {
		if m.Role == "agent" {
			count++
		}
	}
	if count > 0 {
		return count - 1
	}
	return 0
}
