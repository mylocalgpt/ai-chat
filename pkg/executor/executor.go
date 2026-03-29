package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

var ErrRemoteNotSupported = errors.New("remote workspace execution not yet supported")

// TmuxRunner abstracts tmux operations for agent session management.
type TmuxRunner interface {
	NewSession(name, workDir string) error
	HasSession(name string) bool
	KillSession(name string) error
	SendKeys(session, text string) error
	CapturePaneRaw(session string, lines int) (string, error)
	ListSessions() ([]string, error)
}

type sessionStore interface {
	CreateSession(ctx context.Context, workspaceID int64, agent, slug, tmuxSession string) (*core.Session, error)
	GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error)
	UpdateSessionStatus(ctx context.Context, id int64, status string) error
	TouchSession(ctx context.Context, id int64) error
	ListSessions(ctx context.Context) ([]core.Session, error)
	GetWorkspaceByID(ctx context.Context, id int64) (*core.Workspace, error)
}

type SessionLiveInfo struct {
	Session core.Session
	IsAlive bool
}

type Executor struct {
	store        sessionStore
	tmux         TmuxRunner
	registry     *HarnessRegistry
	responsesDir string
}

func NewExecutor(store sessionStore, tmux TmuxRunner, registry *HarnessRegistry, responsesDir string) *Executor {
	if responsesDir == "" {
		responsesDir = DefaultResponseDir()
	}
	return &Executor{
		store:        store,
		tmux:         tmux,
		registry:     registry,
		responsesDir: responsesDir,
	}
}

func (e *Executor) Execute(ctx context.Context, ws core.Workspace, agent, message string) (string, error) {
	log := slog.With("workspace", ws.Name, "agent", agent)

	if ws.Host != "" {
		return "", ErrRemoteNotSupported
	}

	if !e.registry.IsAdapter(agent) {
		return "", fmt.Errorf("executor: no adapter for agent %q", agent)
	}

	return e.executeAdapter(ctx, log, ws, agent, message)
}

func (e *Executor) executeAdapter(ctx context.Context, log *slog.Logger, ws core.Workspace, agent, message string) (string, error) {
	log.Info("executing via adapter")

	adapter, err := e.registry.GetAdapter(agent)
	if err != nil {
		return "", fmt.Errorf("executor: %w", err)
	}

	sess, err := e.store.GetActiveSession(ctx, ws.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("executor: lookup session: %w", err)
	}

	if sess != nil && sess.Agent == agent {
		info := e.buildSessionInfo(ws, sess)
		if !adapter.IsAlive(info) {
			log.Info("adapter session not alive, will respawn", "session_id", sess.ID)
			_ = e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired))
			sess = nil
		}
	} else if sess != nil {
		log.Info("switching agent, killing old session", "old_agent", sess.Agent, "session_id", sess.ID)
		if oldAdapter, err := e.registry.GetAdapter(sess.Agent); err == nil {
			oldInfo := e.buildSessionInfo(ws, sess)
			_ = oldAdapter.Stop(ctx, oldInfo)
		}
		_ = e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired))
		sess = nil
	}

	if sess == nil {
		sessionName, slug, err := NewSessionName(ws.Name)
		if err != nil {
			return "", fmt.Errorf("executor: generate session name: %w", err)
		}
		log = log.With("tmux_session", sessionName)

		log.Info("spawning new adapter session")

		tempSess := &core.Session{
			TmuxSession: sessionName,
			Slug:        slug,
			Agent:       agent,
		}
		info := e.buildSessionInfo(ws, tempSess)

		if err := adapter.Spawn(ctx, info); err != nil {
			return "", fmt.Errorf("executor: spawn adapter: %w", err)
		}

		newSess, err := e.store.CreateSession(ctx, ws.ID, agent, slug, sessionName)
		if err != nil {
			_ = adapter.Stop(ctx, info)
			return "", fmt.Errorf("executor: create session record: %w", err)
		}
		sess = newSess
	} else {
		log.Info("reusing existing adapter session", "session_id", sess.ID)
	}

	info := e.buildSessionInfo(ws, sess)

	// Prefer streaming path when the adapter supports it.
	if sa, ok := adapter.(StreamingAdapter); ok {
		return e.executeStreaming(ctx, log, sess, info, sa, message)
	}

	if err := adapter.Send(ctx, info, message); err != nil {
		return "", fmt.Errorf("executor: adapter send: %w", err)
	}

	if err := AppendMessage(info.ResponseFile, ResponseMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return "", fmt.Errorf("executor: append user message: %w", err)
	}

	response, err := LatestAgentMessage(info.ResponseFile)
	if err != nil {
		return "", fmt.Errorf("executor: read response: %w", err)
	}

	if err := e.store.TouchSession(ctx, sess.ID); err != nil {
		log.Warn("failed to touch session", "error", err)
	}

	return response, nil
}

// executeStreaming sends a message via a StreamingAdapter, drains the event
// channel, writes both user and agent messages to the response file, and
// returns the agent's response text.
func (e *Executor) executeStreaming(ctx context.Context, log *slog.Logger, sess *core.Session, info core.SessionInfo, sa StreamingAdapter, message string) (string, error) {
	ch, err := sa.SendStream(ctx, info, message)
	if err != nil {
		return "", fmt.Errorf("executor: streaming send: %w", err)
	}

	var finalText string
	var deltaBuffer strings.Builder
	for evt := range ch {
		switch evt.Type {
		case core.EventTextDelta:
			deltaBuffer.WriteString(evt.Text)
		case core.EventText:
			finalText = evt.Text
		case core.EventStepFinish:
			if evt.Tokens != nil {
				log.Info("step finished",
					"session_id", sess.ID,
					"input_tokens", evt.Tokens.Input,
					"output_tokens", evt.Tokens.Output,
					"cost", evt.Cost,
					"reason", evt.Reason,
				)
			}
		case core.EventError:
			return "", fmt.Errorf("executor: agent error: %s", evt.Text)
		}
	}

	if finalText == "" {
		finalText = deltaBuffer.String()
	}

	if err := AppendMessage(info.ResponseFile, ResponseMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return "", fmt.Errorf("executor: append user message: %w", err)
	}
	if finalText != "" {
		if err := AppendMessage(info.ResponseFile, ResponseMessage{
			Role:      "agent",
			Content:   finalText,
			Timestamp: time.Now().UTC(),
		}); err != nil {
			return "", fmt.Errorf("executor: append agent message: %w", err)
		}
	}

	if err := e.store.TouchSession(ctx, sess.ID); err != nil {
		log.Warn("failed to touch session", "error", err)
	}

	return finalText, nil
}

func (e *Executor) buildSessionInfo(ws core.Workspace, sess *core.Session) core.SessionInfo {
	return core.SessionInfo{
		Name:           sess.TmuxSession,
		Slug:           sess.Slug,
		Workspace:      ws.Name,
		WorkspacePath:  ws.Path,
		Agent:          sess.Agent,
		ResponseFile:   ResponseFilePath(e.responsesDir, sess.TmuxSession),
		AgentSessionID: sess.AgentSessionID,
	}
}

func (e *Executor) SpawnSession(ctx context.Context, ws core.Workspace, agent string) error {
	if ws.Host != "" {
		return ErrRemoteNotSupported
	}

	if !e.registry.IsAdapter(agent) {
		return fmt.Errorf("executor: no adapter for agent %q", agent)
	}

	return e.spawnAdapterSession(ctx, ws, agent)
}

func (e *Executor) spawnAdapterSession(ctx context.Context, ws core.Workspace, agent string) error {
	adapter, err := e.registry.GetAdapter(agent)
	if err != nil {
		return fmt.Errorf("executor: %w", err)
	}

	sessionName, slug, err := NewSessionName(ws.Name)
	if err != nil {
		return fmt.Errorf("executor: generate session name: %w", err)
	}

	tempSess := &core.Session{
		TmuxSession: sessionName,
		Slug:        slug,
		Agent:       agent,
	}
	info := e.buildSessionInfo(ws, tempSess)

	if err := adapter.Spawn(ctx, info); err != nil {
		return fmt.Errorf("executor: spawn adapter: %w", err)
	}

	_, err = e.store.CreateSession(ctx, ws.ID, agent, slug, sessionName)
	if err != nil {
		_ = adapter.Stop(ctx, info)
		return fmt.Errorf("executor: create session record: %w", err)
	}

	return nil
}

func (e *Executor) KillSession(ctx context.Context, workspaceID int64, agent string) error {
	sess, err := e.store.GetActiveSession(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("executor: kill session lookup: %w", err)
	}

	if e.registry.IsAdapter(agent) {
		adapter, err := e.registry.GetAdapter(agent)
		if err != nil {
			return fmt.Errorf("executor: %w", err)
		}
		info := core.SessionInfo{
			Name:  sess.TmuxSession,
			Slug:  sess.Slug,
			Agent: sess.Agent,
		}
		if err := adapter.Stop(ctx, info); err != nil {
			slog.Warn("failed to stop adapter session", "tmux_session", sess.TmuxSession, "error", err)
		}
	}

	if err := e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired)); err != nil {
		return fmt.Errorf("executor: kill session update: %w", err)
	}
	return nil
}

func (e *Executor) ListSessions(ctx context.Context) ([]SessionLiveInfo, error) {
	sessions, err := e.store.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("executor: list sessions: %w", err)
	}

	infos := make([]SessionLiveInfo, len(sessions))
	for i, sess := range sessions {
		alive := false
		if adapter, adapterErr := e.registry.GetAdapter(sess.Agent); adapterErr == nil {
			if _, isStreaming := adapter.(StreamingAdapter); isStreaming {
				// Serve-based sessions: check server health via the adapter.
				// Look up the workspace path for the server manager key.
				var workspacePath string
				if ws, wsErr := e.store.GetWorkspaceByID(ctx, sess.WorkspaceID); wsErr == nil {
					workspacePath = ws.Path
				}
				info := core.SessionInfo{
					Name:           sess.TmuxSession,
					Slug:           sess.Slug,
					Agent:          sess.Agent,
					AgentSessionID: sess.AgentSessionID,
					WorkspacePath:  workspacePath,
				}
				alive = adapter.IsAlive(info)
			} else if sess.TmuxSession != "" {
				// Tmux-based sessions: check tmux session existence.
				alive = e.tmux.HasSession(sess.TmuxSession)
			}
		} else if sess.TmuxSession != "" {
			// Adapter not found; fall back to tmux check.
			alive = e.tmux.HasSession(sess.TmuxSession)
		}
		infos[i] = SessionLiveInfo{
			Session: sess,
			IsAlive: alive,
		}
	}
	return infos, nil
}
