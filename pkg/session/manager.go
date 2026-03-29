package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type sessionStore interface {
	GetActiveWorkspace(ctx context.Context, senderID, channel string) (*core.ActiveWorkspace, error)
	GetActiveWorkspaceSessionBySessionID(ctx context.Context, sessionID int64) (*core.ActiveWorkspaceSession, error)
	SetActiveWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) error
	GetActiveSessionForWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) (*core.ActiveWorkspaceSession, error)
	SetActiveSessionForWorkspace(ctx context.Context, senderID, channel string, workspaceID, sessionID int64) error
	ClearActiveSessionForWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) error
	GetWorkspaceByID(ctx context.Context, id int64) (*core.Workspace, error)
	GetWorkspaceByName(ctx context.Context, name string) (*core.Workspace, error)
	GetWorkspaceByAlias(ctx context.Context, alias string) (*core.Workspace, error)
	GetSessionByID(ctx context.Context, id int64) (*core.Session, error)
	GetSessionByTmuxSession(ctx context.Context, tmuxSession string) (*core.Session, error)
	GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error)
	GetActiveSessionForSender(ctx context.Context, senderID, channel string, workspaceID int64) (*core.Session, error)
	CreateSession(ctx context.Context, workspaceID int64, agent, slug, tmuxSession string) (*core.Session, error)
	UpdateSessionStatus(ctx context.Context, id int64, status string) error
	TouchSession(ctx context.Context, id int64) error
	ListSessions(ctx context.Context) ([]core.Session, error)
	ListActiveSessionsForWorkspace(ctx context.Context, workspaceID int64) ([]core.Session, error)
	CountActiveSessionsForWorkspace(ctx context.Context, workspaceID int64) (int, error)
	UpdateWorkspaceMetadata(ctx context.Context, id int64, metadata json.RawMessage) error
}

type AdapterRegistry interface {
	GetAdapter(agent string) (executor.AgentAdapter, error)
	KnownAgents() []string
}

type Manager struct {
	store      sessionStore
	adapters   AdapterRegistry
	proxy      *executor.SecurityProxy
	responseCh chan core.ResponseEvent
	watcher    *Watcher
	reaper     *Reaper
	cfg        ManagerConfig
	mu         sync.Mutex
}

type ManagerConfig struct {
	ResponsesDir    string
	SoftIdleTimeout time.Duration
	HardIdleTimeout time.Duration
	ReaperInterval  time.Duration
}

func NewManager(store sessionStore, adapters AdapterRegistry, proxy *executor.SecurityProxy, cfg ManagerConfig) *Manager {
	if cfg.SoftIdleTimeout == 0 {
		cfg.SoftIdleTimeout = 30 * time.Minute
	}
	if cfg.HardIdleTimeout == 0 {
		cfg.HardIdleTimeout = 2 * time.Hour
	}
	if cfg.ReaperInterval == 0 {
		cfg.ReaperInterval = 5 * time.Minute
	}
	if cfg.ResponsesDir == "" {
		cfg.ResponsesDir = executor.DefaultResponseDir()
	}

	responseCh := make(chan core.ResponseEvent, 100)
	watcher := NewWatcher(cfg.ResponsesDir, responseCh, store)
	reaper := NewReaper(store, adapters, ReaperConfig{
		SoftIdleTimeout: cfg.SoftIdleTimeout,
		HardIdleTimeout: cfg.HardIdleTimeout,
		Interval:        cfg.ReaperInterval,
	})

	return &Manager{
		store:      store,
		adapters:   adapters,
		proxy:      proxy,
		responseCh: responseCh,
		watcher:    watcher,
		reaper:     reaper,
		cfg:        cfg,
	}
}

func (m *Manager) Send(ctx context.Context, senderID, channel, message string) error {
	sess, info, err := m.GetOrCreateActiveSession(ctx, senderID, channel, "")
	if err != nil {
		return err
	}

	adapter, err := m.adapters.GetAdapter(sess.Agent)
	if err != nil {
		return fmt.Errorf("getting adapter for agent %q: %w", sess.Agent, err)
	}

	if err := adapter.Send(ctx, *info, message); err != nil {
		return fmt.Errorf("adapter send: %w", err)
	}

	if err := m.store.TouchSession(ctx, sess.ID); err != nil {
		slog.Warn("failed to touch session", "session_id", sess.ID, "error", err)
	}

	if m.proxy != nil {
		if flags := m.proxy.Scan(message); len(flags) > 0 {
			slog.Warn("security flags in message", "flags", flags)
		}
	}

	return nil
}

func (m *Manager) GetOrCreateActiveSession(ctx context.Context, senderID, channel, agent string) (*core.Session, *core.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, err := m.getCurrentWorkspaceLocked(ctx, senderID, channel)
	if err != nil {
		return nil, nil, err
	}
	if agent == "" {
		agent = getDefaultAgent(ws)
	}

	sess, info, err := m.restoreMappedSessionLocked(ctx, senderID, channel, ws)
	if err != nil {
		return nil, nil, err
	}
	if sess != nil {
		if sess.Agent == agent || agent == "" {
			return sess, info, nil
		}
		if err := m.expireSessionLocked(ctx, ws, sess); err != nil {
			return nil, nil, err
		}
		if err := m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID); err != nil {
			return nil, nil, fmt.Errorf("clearing active session: %w", err)
		}
	}

	sess, info, err = m.createSessionLocked(ctx, ws.ID, ws, agent)
	if err != nil {
		return nil, nil, err
	}

	if err := m.store.SetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID, sess.ID); err != nil {
		return nil, nil, fmt.Errorf("setting active session: %w", err)
	}

	return sess, info, nil
}

func (m *Manager) CreateSession(ctx context.Context, workspaceID int64, agent string) (*core.SessionInfo, error) {
	ws, err := m.store.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("getting workspace: %w", err)
	}

	if agent == "" {
		agent = getDefaultAgent(ws)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, info, err := m.createSessionLocked(ctx, workspaceID, ws, agent)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (m *Manager) CreateSessionForSender(ctx context.Context, senderID, channel, agent string) (*core.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, err := m.getCurrentWorkspaceLocked(ctx, senderID, channel)
	if err != nil {
		return nil, err
	}
	if agent == "" {
		agent = getDefaultAgent(ws)
	}

	sess, info, err := m.createSessionLocked(ctx, ws.ID, ws, agent)
	if err != nil {
		return nil, err
	}
	if err := m.store.SetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID, sess.ID); err != nil {
		return nil, fmt.Errorf("setting active session: %w", err)
	}
	return info, nil
}

func (m *Manager) SwitchActiveSession(ctx context.Context, senderID, channel string, workspaceID, sessionID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, err := m.getCurrentWorkspaceLocked(ctx, senderID, channel)
	if err != nil {
		return err
	}
	if ws.ID != workspaceID {
		return fmt.Errorf("switching active session: workspace mismatch")
	}

	sess, err := m.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("getting session: %w", err)
	}
	if sess.WorkspaceID != workspaceID {
		return fmt.Errorf("switching active session: session %d is not in workspace %d", sessionID, workspaceID)
	}

	adapter, err := m.adapters.GetAdapter(sess.Agent)
	if err == nil {
		info := m.buildSessionInfo(ws, sess)
		if !adapter.IsAlive(*info) {
			if err := m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, workspaceID); err != nil {
				return fmt.Errorf("clearing stale active session: %w", err)
			}
			return fmt.Errorf("switching active session: %w", store.ErrNotFound)
		}
	}

	if err := m.store.SetActiveSessionForWorkspace(ctx, senderID, channel, workspaceID, sessionID); err != nil {
		return fmt.Errorf("setting active session: %w", err)
	}
	return nil
}

func (m *Manager) createSessionLocked(ctx context.Context, workspaceID int64, ws *core.Workspace, agent string) (*core.Session, *core.SessionInfo, error) {
	sessionName, slug, err := executor.NewSessionName(ws.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("generating session name: %w", err)
	}

	adapter, err := m.adapters.GetAdapter(agent)
	if err != nil {
		return nil, nil, fmt.Errorf("getting adapter for agent %q: %w", agent, err)
	}

	tempSess := &core.Session{
		TmuxSession: sessionName,
		Slug:        slug,
		Agent:       agent,
	}
	info := m.buildSessionInfo(ws, tempSess)

	if err := adapter.Spawn(ctx, *info); err != nil {
		return nil, nil, fmt.Errorf("spawning adapter: %w", err)
	}

	sess, err := m.store.CreateSession(ctx, workspaceID, agent, slug, sessionName)
	if err != nil {
		_ = adapter.Stop(ctx, *info)
		return nil, nil, fmt.Errorf("creating session record: %w", err)
	}

	info = m.buildSessionInfo(ws, sess)
	return sess, info, nil
}

func (m *Manager) ClearSession(ctx context.Context, senderID, channel string) (*core.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, err := m.getCurrentWorkspaceLocked(ctx, senderID, channel)
	if err != nil {
		return nil, err
	}

	agent := getDefaultAgent(ws)
	activeSession, err := m.store.GetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
	if err == nil && activeSession != nil {
		sess, err := m.store.GetSessionByID(ctx, activeSession.SessionID)
		if err == nil {
			agent = sess.Agent
			if err := m.expireSessionLocked(ctx, ws, sess); err != nil {
				return nil, err
			}
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("getting active session mapping: %w", err)
	}

	sess, info, err := m.createSessionLocked(ctx, ws.ID, ws, agent)
	if err != nil {
		return nil, err
	}

	if err := m.store.SetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID, sess.ID); err != nil {
		return nil, fmt.Errorf("setting active session: %w", err)
	}

	return info, nil
}

func (m *Manager) KillSession(ctx context.Context, senderID, channel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, err := m.getCurrentWorkspaceLocked(ctx, senderID, channel)
	if err != nil {
		return err
	}

	activeSession, err := m.store.GetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting active session mapping: %w", err)
	}

	sess, err := m.store.GetSessionByID(ctx, activeSession.SessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
		}
		return fmt.Errorf("getting session: %w", err)
	}
	if err := m.expireSessionLocked(ctx, ws, sess); err != nil {
		return err
	}
	if err := m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID); err != nil {
		return fmt.Errorf("clearing active session: %w", err)
	}

	return nil
}

func (m *Manager) GetStatus(ctx context.Context, senderID, channel string) (*router.StatusInfo, error) {
	ws, err := m.getCurrentWorkspaceLocked(ctx, senderID, channel)
	if err != nil {
		return nil, err
	}

	info := &router.StatusInfo{
		Workspace: ws,
		Agent:     getDefaultAgent(ws),
	}

	count, err := m.store.CountActiveSessionsForWorkspace(ctx, ws.ID)
	if err != nil {
		slog.Warn("failed to count sessions", "error", err)
		count = 0
	}
	info.SessionCount = count

	activeSession, err := m.store.GetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
	if err == nil && activeSession != nil {
		sess, err := m.store.GetSessionByID(ctx, activeSession.SessionID)
		if err == nil {
			adapter, err := m.adapters.GetAdapter(sess.Agent)
			if err == nil {
				sessionInfo := m.buildSessionInfo(ws, sess)
				if adapter.IsAlive(*sessionInfo) {
					info.ActiveSession = sessionInfo
				} else {
					_ = m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
				}
			}
		} else if errors.Is(err, store.ErrNotFound) || err == core.ErrNotFound {
			_ = m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
		}
	}

	return info, nil
}

func (m *Manager) SetAgent(ctx context.Context, senderID, channel, agent string) (*core.SessionInfo, error) {
	found := false
	for _, a := range m.adapters.KnownAgents() {
		if a == agent {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown agent: %q", agent)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ws, err := m.getCurrentWorkspaceLocked(ctx, senderID, channel)
	if err != nil {
		return nil, err
	}

	metadata := make(map[string]any)
	if ws.Metadata != nil {
		if err := json.Unmarshal(ws.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("parsing workspace metadata: %w", err)
		}
	}
	metadata["default_agent"] = agent
	newMetadata, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshaling workspace metadata: %w", err)
	}

	if err := m.store.UpdateWorkspaceMetadata(ctx, ws.ID, newMetadata); err != nil {
		return nil, fmt.Errorf("updating workspace metadata: %w", err)
	}
	ws.Metadata = newMetadata

	activeSession, err := m.store.GetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
	if err == nil && activeSession != nil {
		sess, err := m.store.GetSessionByID(ctx, activeSession.SessionID)
		if err == nil && sess.Agent != agent {
			if err := m.expireSessionLocked(ctx, ws, sess); err != nil {
				return nil, err
			}
			if err := m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID); err != nil {
				return nil, fmt.Errorf("clearing active session: %w", err)
			}
			return nil, nil
		}
	}

	if activeSession, err = m.store.GetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID); err == nil && activeSession != nil {
		sess, err := m.store.GetSessionByID(ctx, activeSession.SessionID)
		if err == nil {
			sessionInfo := m.buildSessionInfo(ws, sess)
			return sessionInfo, nil
		}
	}

	return nil, nil
}

func (m *Manager) getCurrentWorkspaceLocked(ctx context.Context, senderID, channel string) (*core.Workspace, error) {
	activeWorkspace, err := m.store.GetActiveWorkspace(ctx, senderID, channel)
	if err != nil {
		return nil, fmt.Errorf("getting active workspace: %w", err)
	}
	ws, err := m.store.GetWorkspaceByID(ctx, activeWorkspace.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("getting workspace: %w", err)
	}
	return ws, nil
}

func (m *Manager) restoreMappedSessionLocked(ctx context.Context, senderID, channel string, ws *core.Workspace) (*core.Session, *core.SessionInfo, error) {
	activeSession, err := m.store.GetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("getting active session mapping: %w", err)
	}
	if activeSession == nil {
		return nil, nil, nil
	}

	sess, err := m.store.GetSessionByID(ctx, activeSession.SessionID)
	if errors.Is(err, store.ErrNotFound) {
		if clearErr := m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID); clearErr != nil {
			return nil, nil, fmt.Errorf("clearing stale active session: %w", clearErr)
		}
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("getting session: %w", err)
	}

	adapter, err := m.adapters.GetAdapter(sess.Agent)
	if err != nil {
		return nil, nil, fmt.Errorf("getting adapter for agent %q: %w", sess.Agent, err)
	}
	info := m.buildSessionInfo(ws, sess)
	if !adapter.IsAlive(*info) {
		if err := m.expireSessionLocked(ctx, ws, sess); err != nil {
			return nil, nil, err
		}
		if err := m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID); err != nil {
			return nil, nil, fmt.Errorf("clearing stale active session: %w", err)
		}
		return nil, nil, nil
	}
	return sess, info, nil
}

func (m *Manager) expireSessionLocked(ctx context.Context, ws *core.Workspace, sess *core.Session) error {
	adapter, err := m.adapters.GetAdapter(sess.Agent)
	if err == nil {
		info := m.buildSessionInfo(ws, sess)
		if stopErr := adapter.Stop(ctx, *info); stopErr != nil {
			slog.Warn("failed to stop session", "session_id", sess.ID, "error", stopErr)
		}
	}
	if err := m.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired)); err != nil {
		return fmt.Errorf("updating session status: %w", err)
	}
	return nil
}

func (m *Manager) ResponseCh() <-chan core.ResponseEvent {
	return m.responseCh
}

func (m *Manager) Run(ctx context.Context) error {
	if _, err := m.ReconcileOnStartup(ctx); err != nil {
		slog.Warn("startup reconciliation failed", "error", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := m.watcher.Run(ctx); err != nil && err != ctx.Err() {
			slog.Warn("watcher exited with error", "error", err)
		}
	}()

	go func() {
		defer wg.Done()
		m.reaper.Run(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func (m *Manager) buildSessionInfo(ws *core.Workspace, sess *core.Session) *core.SessionInfo {
	return &core.SessionInfo{
		Name:          sess.TmuxSession,
		Slug:          sess.Slug,
		Workspace:     ws.Name,
		WorkspacePath: ws.Path,
		Agent:         sess.Agent,
		ResponseFile:  executor.ResponseFilePath(m.cfg.ResponsesDir, sess.TmuxSession),
	}
}

func getDefaultAgent(ws *core.Workspace) string {
	if ws.Metadata != nil {
		var metadata struct {
			DefaultAgent string `json:"default_agent"`
		}
		if err := json.Unmarshal(ws.Metadata, &metadata); err == nil && metadata.DefaultAgent != "" {
			return metadata.DefaultAgent
		}
	}
	return "opencode"
}
