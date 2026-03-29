package app

import (
	"context"
	"log/slog"
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
		forwardResponses(ctx, st, manager.ResponseCh(), channel)
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

func forwardResponses(ctx context.Context, st messageStore, events <-chan core.ResponseEvent, channel core.Channel) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := channel.Send(ctx, core.OutboundMessage{
				Channel:     event.Channel,
				RecipientID: event.SenderID,
				Content:     event.Content,
			}); err != nil {
				slog.Warn("failed to send response event", "error", err)
				continue
			}
			if err := st.CreateMessage(ctx, &core.Message{
				Channel:   event.Channel,
				SenderID:  event.SenderID,
				Content:   event.Content,
				Direction: core.OutboundDirection,
				Status:    core.StatusDone,
			}); err != nil {
				slog.Warn("failed to persist response event", "error", err)
			}
		}
	}
}
