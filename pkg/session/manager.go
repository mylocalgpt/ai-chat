package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	GetSessionByAgentSessionID(ctx context.Context, agentSessionID string) (*core.Session, error)
	GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error)
	GetActiveSessionForSender(ctx context.Context, senderID, channel string, workspaceID int64) (*core.Session, error)
	CreateSession(ctx context.Context, workspaceID int64, agent, slug, tmuxSession string) (*core.Session, error)
	UpdateSessionStatus(ctx context.Context, id int64, status string) error
	TouchSession(ctx context.Context, id int64) error
	ListSessions(ctx context.Context) ([]core.Session, error)
	ListActiveSessionsForWorkspace(ctx context.Context, workspaceID int64) ([]core.Session, error)
	CountActiveSessionsForWorkspace(ctx context.Context, workspaceID int64) (int, error)
	UpdateWorkspaceMetadata(ctx context.Context, id int64, metadata json.RawMessage) error
	UpdateAgentSessionID(ctx context.Context, id int64, agentSessionID string) error
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
	pending    map[string]pendingConfirmation
	mu         sync.Mutex
}

type pendingConfirmation struct {
	SenderID    string
	Channel     string
	WorkspaceID int64
	SessionID   int64
	Token       string
	Message     string
	ExpiresAt   time.Time
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
	watcher := NewWatcher(cfg.ResponsesDir, responseCh, store, proxy)
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
		pending:    make(map[string]pendingConfirmation),
	}
}

func (m *Manager) Send(ctx context.Context, senderID, channel, message, replyToMsgID string) error {
	sess, info, err := m.GetOrCreateActiveSession(ctx, senderID, channel, "")
	if err != nil {
		return err
	}

	decision := m.evaluateSecurity(message)
	switch decision.Action {
	case core.SecurityActionBlock:
		return &core.SecurityDecisionError{Decision: decision}
	case core.SecurityActionConfirm:
		token, err := m.storePendingConfirmation(senderID, channel, sess.WorkspaceID, sess.ID, message)
		if err != nil {
			return err
		}
		decision.PendingID = token
		return &core.SecurityDecisionError{Decision: decision}
	default:
		return m.dispatchMessage(ctx, sess, info, message, senderID, channel, replyToMsgID)
	}
}

func (m *Manager) SendToSession(ctx context.Context, senderID, channel string, sessionID int64, message, replyToMsgID string) error {
	ws, sess, info, err := m.loadSessionTarget(ctx, sessionID, true)
	if err != nil {
		return err
	}
	decision := m.evaluateSecurity(message)
	switch decision.Action {
	case core.SecurityActionBlock:
		return &core.SecurityDecisionError{Decision: decision}
	case core.SecurityActionConfirm:
		token, err := m.storePendingConfirmation(senderID, channel, ws.ID, sess.ID, message)
		if err != nil {
			return err
		}
		decision.PendingID = token
		return &core.SecurityDecisionError{Decision: decision}
	default:
		return m.dispatchMessage(ctx, sess, info, message, senderID, channel, replyToMsgID)
	}
}

func (m *Manager) HandleSecurityDecision(ctx context.Context, senderID, channel, token string, approved bool) (string, error) {
	pending, ok := m.takePendingConfirmation(senderID, channel, token)
	if !ok {
		return "That confirmation has expired or was already used.", nil
	}
	if !approved {
		return "Cancelled.", nil
	}

	ws, err := m.store.GetWorkspaceByID(ctx, pending.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("getting workspace: %w", err)
	}
	sess, err := m.store.GetSessionByID(ctx, pending.SessionID)
	if err != nil {
		return "", fmt.Errorf("getting session: %w", err)
	}
	info := m.buildSessionInfo(ws, sess)
	if err := m.dispatchMessage(ctx, sess, info, pending.Message, pending.SenderID, pending.Channel, ""); err != nil {
		return "", err
	}
	return "Message approved and sent.", nil
}

func (m *Manager) ClearSessionByID(ctx context.Context, senderID, channel string, sessionID int64) (*core.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, sess, _, err := m.loadSessionTarget(ctx, sessionID, false)
	if err != nil {
		return nil, err
	}
	if err := m.expireSessionLocked(ctx, ws, sess); err != nil {
		return nil, err
	}
	newSess, info, err := m.createSessionLocked(ctx, ws.ID, ws, sess.Agent)
	if err != nil {
		return nil, err
	}
	if err := m.store.SetActiveWorkspace(ctx, senderID, channel, ws.ID); err != nil {
		return nil, fmt.Errorf("setting active workspace: %w", err)
	}
	if err := m.store.SetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID, newSess.ID); err != nil {
		return nil, fmt.Errorf("setting active session: %w", err)
	}
	return info, nil
}

func (m *Manager) KillSessionByID(ctx context.Context, senderID, channel string, sessionID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, sess, _, err := m.loadSessionTarget(ctx, sessionID, false)
	if err != nil {
		return err
	}
	if err := m.expireSessionLocked(ctx, ws, sess); err != nil {
		return err
	}
	activeSession, err := m.store.GetActiveSessionForWorkspace(ctx, senderID, channel, ws.ID)
	if err == nil && activeSession != nil && activeSession.SessionID == sessionID {
		if err := m.store.ClearActiveSessionForWorkspace(ctx, senderID, channel, ws.ID); err != nil {
			return fmt.Errorf("clearing active session: %w", err)
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

	// Persist the agent's own session ID if the adapter supports streaming.
	if sa, ok := adapter.(executor.StreamingAdapter); ok {
		if agentSessID := sa.GetAgentSessionID(*info); agentSessID != "" {
			if err := m.store.UpdateAgentSessionID(ctx, sess.ID, agentSessID); err != nil {
				slog.Warn("failed to persist agent session ID", "session_id", sess.ID, "error", err)
			} else {
				sess.AgentSessionID = agentSessID
			}
		}
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
	wg.Add(3)

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

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanupExpiredPendingConfirmations()
			}
		}
	}()

	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func (m *Manager) buildSessionInfo(ws *core.Workspace, sess *core.Session) *core.SessionInfo {
	return &core.SessionInfo{
		Name:           sess.TmuxSession,
		Slug:           sess.Slug,
		Workspace:      ws.Name,
		WorkspacePath:  ws.Path,
		Agent:          sess.Agent,
		ResponseFile:   executor.ResponseFilePath(m.cfg.ResponsesDir, sess.TmuxSession),
		AgentSessionID: sess.AgentSessionID,
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

func (m *Manager) dispatchMessage(ctx context.Context, sess *core.Session, info *core.SessionInfo, message, senderID, channel, replyToMsgID string) error {
	adapter, err := m.adapters.GetAdapter(sess.Agent)
	if err != nil {
		return fmt.Errorf("getting adapter for agent %q: %w", sess.Agent, err)
	}

	// Prefer streaming path when the adapter supports it.
	if sa, ok := adapter.(executor.StreamingAdapter); ok {
		return m.dispatchStreaming(ctx, sess, info, sa, message, senderID, channel, replyToMsgID)
	}

	if err := adapter.Send(ctx, *info, message); err != nil {
		return fmt.Errorf("adapter send: %w", err)
	}
	if err := executor.AppendMessage(info.ResponseFile, executor.ResponseMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append user message: %w", err)
	}
	if err := m.store.TouchSession(ctx, sess.ID); err != nil {
		slog.Warn("failed to touch session", "session_id", sess.ID, "error", err)
	}
	return nil
}

// dispatchStreaming sends a message via a StreamingAdapter and forwards the
// event channel to forwardResponses() via a ResponseEvent. The caller
// (forwardResponses) handles event consumption, delivery, and response file
// persistence. This differs from the non-streaming path where the agent
// response is handled inline.
func (m *Manager) dispatchStreaming(ctx context.Context, sess *core.Session, info *core.SessionInfo, sa executor.StreamingAdapter, message, senderID, channel, replyToMsgID string) error {
	ch, err := sa.SendStream(ctx, *info, message)
	if err != nil {
		return fmt.Errorf("streaming send: %w", err)
	}

	// Write user message to response file (same as before).
	if err := executor.AppendMessage(info.ResponseFile, executor.ResponseMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append user message: %w", err)
	}

	// Send the event channel to forwardResponses via ResponseEvent.
	m.responseCh <- core.ResponseEvent{
		SessionName:    info.Name,
		SessionID:      sess.ID,
		SenderID:       senderID,
		Channel:        channel,
		Events:         ch,
		ReplyToID:      replyToMsgID,
		AgentSessionID: info.AgentSessionID,
		ResponseFile:   info.ResponseFile,
		SessionSlug:    info.Slug,
		Workspace:      info.WorkspacePath,
	}

	if err := m.store.TouchSession(ctx, sess.ID); err != nil {
		slog.Warn("failed to touch session", "session_id", sess.ID, "error", err)
	}
	return nil
}

func (m *Manager) evaluateSecurity(message string) core.SecurityDecision {
	if m.proxy == nil {
		return core.SecurityDecision{Action: core.SecurityActionAllow}
	}
	flags := m.proxy.Scan(message)
	if len(flags) == 0 {
		return core.SecurityDecision{Action: core.SecurityActionAllow}
	}
	for _, flag := range flags {
		if isBlockingSecurityFlag(flag.Keyword) {
			return core.SecurityDecision{Action: core.SecurityActionBlock, Reason: "Message blocked because it appears to contain a live credential or exported secret."}
		}
	}
	return core.SecurityDecision{Action: core.SecurityActionConfirm, Reason: "This message may include sensitive information. Approve it before it is sent to the agent."}
}

func isBlockingSecurityFlag(keyword string) bool {
	switch keyword {
	case "Bearer ", "sk-", "ghp_", "ghs_", "AKIA", "export-env-var":
		return true
	default:
		return false
	}
}

func (m *Manager) storePendingConfirmation(senderID, channel string, workspaceID, sessionID int64, message string) (string, error) {
	token, err := newPendingToken()
	if err != nil {
		return "", fmt.Errorf("creating pending confirmation token: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[token] = pendingConfirmation{
		SenderID:    senderID,
		Channel:     channel,
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Token:       token,
		Message:     message,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}
	return token, nil
}

func (m *Manager) takePendingConfirmation(senderID, channel, token string) (pendingConfirmation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending, ok := m.pending[token]
	if !ok {
		return pendingConfirmation{}, false
	}
	if pending.SenderID != senderID || pending.Channel != channel {
		return pendingConfirmation{}, false
	}
	if time.Now().After(pending.ExpiresAt) {
		delete(m.pending, token)
		return pendingConfirmation{}, false
	}
	delete(m.pending, token)
	return pending, true
}

func (m *Manager) cleanupExpiredPendingConfirmations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for token, pending := range m.pending {
		if now.After(pending.ExpiresAt) {
			delete(m.pending, token)
		}
	}
}

// AbortSession cancels in-flight generation for the session identified by the
// agent's own session ID (e.g. opencode serve "ses_xxx"). It looks up the
// session in the store, builds a SessionInfo, and delegates to the streaming
// adapter's AbortStream method.
func (m *Manager) AbortSession(ctx context.Context, agentSessionID string) error {
	sess, err := m.store.GetSessionByAgentSessionID(ctx, agentSessionID)
	if err != nil {
		return fmt.Errorf("looking up session for agent session %q: %w", agentSessionID, err)
	}
	ws, err := m.store.GetWorkspaceByID(ctx, sess.WorkspaceID)
	if err != nil {
		return fmt.Errorf("looking up workspace for session %d: %w", sess.ID, err)
	}
	adapter, err := m.adapters.GetAdapter(sess.Agent)
	if err != nil {
		return fmt.Errorf("getting adapter for agent %q: %w", sess.Agent, err)
	}
	sa, ok := adapter.(executor.StreamingAdapter)
	if !ok {
		return fmt.Errorf("agent %q does not support streaming abort", sess.Agent)
	}
	info := m.buildSessionInfo(ws, sess)
	return sa.AbortStream(ctx, *info)
}

func newPendingToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (m *Manager) loadSessionTarget(ctx context.Context, sessionID int64, requireAlive bool) (*core.Workspace, *core.Session, *core.SessionInfo, error) {
	sess, err := m.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("getting session: %w", err)
	}
	ws, err := m.store.GetWorkspaceByID(ctx, sess.WorkspaceID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("getting workspace: %w", err)
	}
	info := m.buildSessionInfo(ws, sess)
	if requireAlive {
		adapter, err := m.adapters.GetAdapter(sess.Agent)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("getting adapter for agent %q: %w", sess.Agent, err)
		}
		if !adapter.IsAlive(*info) {
			return nil, nil, nil, store.ErrNotFound
		}
	}
	return ws, sess, info, nil
}
