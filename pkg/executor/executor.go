package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// ErrRemoteNotSupported is returned when Execute is called with a workspace
// whose Host field is non-empty.
var ErrRemoteNotSupported = errors.New("remote workspace execution not yet supported")

// tmuxRunner abstracts tmux operations for testability.
type tmuxRunner interface {
	NewSession(name, workDir string) error
	HasSession(name string) bool
	KillSession(name string) error
	SendKeys(session, text string) error
	CapturePaneRaw(session string, lines int) (string, error)
	ListSessions() ([]string, error)
}

// sessionStore abstracts store operations used by the executor.
type sessionStore interface {
	CreateSession(ctx context.Context, workspaceID int64, agent, slug, tmuxSession string) (*core.Session, error)
	GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error)
	UpdateSessionStatus(ctx context.Context, id int64, status string) error
	TouchSession(ctx context.Context, id int64) error
	ListSessions(ctx context.Context) ([]core.Session, error)
}

// SessionInfo enriches a core.Session with live tmux state.
type SessionInfo struct {
	Session core.Session
	IsAlive bool
}

// Executor ties the store, tmux wrapper, and harness registry together. It is
// the public API of the executor package.
type Executor struct {
	store    sessionStore
	tmux     tmuxRunner
	registry *HarnessRegistry
}

// NewExecutor returns a new Executor.
func NewExecutor(store sessionStore, tmux tmuxRunner, registry *HarnessRegistry) *Executor {
	return &Executor{
		store:    store,
		tmux:     tmux,
		registry: registry,
	}
}

// Execute sends a message to an AI agent in the given workspace and returns
// the response. It handles session reuse, spawning, and lifecycle tracking.
func (e *Executor) Execute(ctx context.Context, ws core.Workspace, agent, message string) (string, error) {
	log := slog.With("workspace", ws.Name, "agent", agent)

	// Remote workspaces not yet supported.
	if ws.Host != "" {
		return "", ErrRemoteNotSupported
	}

	// CLI path: no session tracking.
	if !e.registry.IsTmux(agent) {
		log.Info("executing via CLI harness")
		harness, err := e.registry.GetCLI(agent)
		if err != nil {
			return "", fmt.Errorf("executor: %w", err)
		}
		return harness.Execute(ctx, ws.Path, message)
	}

	// Tmux path: session lifecycle.

	// Look up existing active session.
	sess, err := e.store.GetActiveSession(ctx, ws.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("executor: lookup session: %w", err)
	}

	// Validate the existing session if found.
	if sess != nil {
		sess = e.validateSession(ctx, log, sess, agent)
	}

	// Spawn a new session if needed.
	if sess == nil {
		sessionName, slug, err := NewSessionName(ws.Name)
		if err != nil {
			return "", fmt.Errorf("executor: generate session name: %w", err)
		}
		log = log.With("tmux_session", sessionName)

		log.Info("spawning new session")
		if err := e.tmux.NewSession(sessionName, ws.Path); err != nil {
			return "", fmt.Errorf("executor: create tmux session: %w", err)
		}

		harness, err := e.registry.GetTmux(agent)
		if err != nil {
			_ = e.tmux.KillSession(sessionName)
			return "", fmt.Errorf("executor: %w", err)
		}

		if err := harness.Spawn(ctx, sessionName); err != nil {
			_ = e.tmux.KillSession(sessionName)
			return "", fmt.Errorf("executor: spawn agent: %w", err)
		}

		newSess, err := e.store.CreateSession(ctx, ws.ID, agent, slug, sessionName)
		if err != nil {
			_ = e.tmux.KillSession(sessionName)
			return "", fmt.Errorf("executor: create session record: %w", err)
		}
		sess = newSess
	} else {
		log.Info("reusing existing session", "session_id", sess.ID)
	}

	// Send message and read response.
	harness, err := e.registry.GetTmux(agent)
	if err != nil {
		return "", fmt.Errorf("executor: %w", err)
	}

	snapshot, err := harness.SendMessage(ctx, sess.TmuxSession, message)
	if err != nil {
		return "", fmt.Errorf("executor: send message: %w", err)
	}

	response, err := harness.ReadResponse(ctx, sess.TmuxSession, snapshot)
	if err != nil {
		if errors.Is(err, ErrResponseTimeout) {
			_ = e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionCrashed))
			log.Warn("response timed out, session marked crashed")
		}
		return "", fmt.Errorf("executor: read response: %w", err)
	}

	// Parse status from the response.
	status := harness.DetectStatus(response)
	log.Info("agent status detected", "state", status.State, "detail", status.Detail)

	switch status.State {
	case AgentCrashed:
		_ = e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionCrashed))
		return "", fmt.Errorf("executor: agent crashed: %s", status.Detail)
	case AgentExited:
		_ = e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired))
		return "", fmt.Errorf("executor: agent exited: %s", status.Detail)
	}

	// Touch session to update last_activity.
	if err := e.store.TouchSession(ctx, sess.ID); err != nil {
		log.Warn("failed to touch session", "error", err)
	}

	return response, nil
}

// validateSession checks if an existing session is still valid and usable.
// Returns nil if the session should be replaced.
func (e *Executor) validateSession(ctx context.Context, log *slog.Logger, sess *core.Session, agent string) *core.Session {
	if !e.tmux.HasSession(sess.TmuxSession) {
		log.Warn("tmux session dead, marking crashed", "session_id", sess.ID)
		_ = e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionCrashed))
		return nil
	}

	if sess.Agent != agent {
		log.Info("switching agent, killing old session", "old_agent", sess.Agent, "session_id", sess.ID)
		_ = e.tmux.KillSession(sess.TmuxSession)
		_ = e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired))
		return nil
	}

	return sess
}

// SpawnSession creates a new tmux session and agent process for the given
// workspace without sending a message. Used by MCP session_restart.
func (e *Executor) SpawnSession(ctx context.Context, ws core.Workspace, agent string) error {
	if ws.Host != "" {
		return ErrRemoteNotSupported
	}

	sessionName, slug, err := NewSessionName(ws.Name)
	if err != nil {
		return fmt.Errorf("executor: generate session name: %w", err)
	}

	if err := e.tmux.NewSession(sessionName, ws.Path); err != nil {
		return fmt.Errorf("executor: create tmux session: %w", err)
	}

	harness, err := e.registry.GetTmux(agent)
	if err != nil {
		_ = e.tmux.KillSession(sessionName)
		return fmt.Errorf("executor: %w", err)
	}

	if err := harness.Spawn(ctx, sessionName); err != nil {
		_ = e.tmux.KillSession(sessionName)
		return fmt.Errorf("executor: spawn agent: %w", err)
	}

	_, err = e.store.CreateSession(ctx, ws.ID, agent, slug, sessionName)
	if err != nil {
		_ = e.tmux.KillSession(sessionName)
		return fmt.Errorf("executor: create session record: %w", err)
	}

	return nil
}

// KillSession finds the active session for a workspace and destroys it.
func (e *Executor) KillSession(ctx context.Context, workspaceID int64, agent string) error {
	sess, err := e.store.GetActiveSession(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // no active session to kill
		}
		return fmt.Errorf("executor: kill session lookup: %w", err)
	}

	if e.tmux.HasSession(sess.TmuxSession) {
		if err := e.tmux.KillSession(sess.TmuxSession); err != nil {
			slog.Warn("failed to kill tmux session", "tmux_session", sess.TmuxSession, "error", err)
		}
	}

	if err := e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired)); err != nil {
		return fmt.Errorf("executor: kill session update: %w", err)
	}
	return nil
}

// ListSessions returns all sessions enriched with live tmux liveness.
func (e *Executor) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	sessions, err := e.store.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("executor: list sessions: %w", err)
	}

	infos := make([]SessionInfo, len(sessions))
	for i, sess := range sessions {
		infos[i] = SessionInfo{
			Session: sess,
			IsAlive: sess.TmuxSession != "" && e.tmux.HasSession(sess.TmuxSession),
		}
	}
	return infos, nil
}
