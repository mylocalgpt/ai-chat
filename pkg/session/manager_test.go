package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

type mockStore struct {
	activeWorkspace     *core.ActiveWorkspace
	activeWorkspaceErr  error
	activeSession       *core.ActiveWorkspaceSession
	activeSessionErr    error
	workspace           *core.Workspace
	workspaceErr        error
	session             *core.Session
	sessionErr          error
	sessions            []core.Session
	sessionsErr         error
	count               int
	countErr            error
	setActiveSessionErr error
	clearActiveSession  error
	updateStatusErr     error
	touchErr            error
	createSessionErr    error
	updateMetadataErr   error
	setActiveSessionIDs []int64
	clearedWorkspaceIDs []int64
}

func (m *mockStore) GetActiveWorkspace(_ context.Context, _, _ string) (*core.ActiveWorkspace, error) {
	return m.activeWorkspace, m.activeWorkspaceErr
}

func (m *mockStore) GetActiveWorkspaceSessionBySessionID(_ context.Context, _ int64) (*core.ActiveWorkspaceSession, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStore) SetActiveWorkspace(_ context.Context, _, _ string, _ int64) error {
	return nil
}

func (m *mockStore) GetActiveSessionForWorkspace(_ context.Context, _, _ string, _ int64) (*core.ActiveWorkspaceSession, error) {
	return m.activeSession, m.activeSessionErr
}

func (m *mockStore) SetActiveSessionForWorkspace(_ context.Context, _, _ string, workspaceID, sessionID int64) error {
	m.setActiveSessionIDs = append(m.setActiveSessionIDs, workspaceID, sessionID)
	return m.setActiveSessionErr
}

func (m *mockStore) ClearActiveSessionForWorkspace(_ context.Context, _, _ string, workspaceID int64) error {
	m.clearedWorkspaceIDs = append(m.clearedWorkspaceIDs, workspaceID)
	return m.clearActiveSession
}

func (m *mockStore) GetWorkspaceByID(_ context.Context, _ int64) (*core.Workspace, error) {
	return m.workspace, m.workspaceErr
}

func (m *mockStore) GetWorkspaceByName(_ context.Context, _ string) (*core.Workspace, error) {
	return m.workspace, m.workspaceErr
}

func (m *mockStore) GetWorkspaceByAlias(_ context.Context, _ string) (*core.Workspace, error) {
	return m.workspace, m.workspaceErr
}

func (m *mockStore) GetSessionByID(_ context.Context, _ int64) (*core.Session, error) {
	return m.session, m.sessionErr
}

func (m *mockStore) GetSessionByTmuxSession(_ context.Context, _ string) (*core.Session, error) {
	return m.session, m.sessionErr
}

func (m *mockStore) GetActiveSession(_ context.Context, _ int64) (*core.Session, error) {
	return m.session, m.sessionErr
}

func (m *mockStore) GetActiveSessionForSender(_ context.Context, _, _ string, _ int64) (*core.Session, error) {
	return m.session, m.sessionErr
}

func (m *mockStore) CreateSession(_ context.Context, _ int64, _, _, _ string) (*core.Session, error) {
	if m.createSessionErr != nil {
		return nil, m.createSessionErr
	}
	return m.session, nil
}

func (m *mockStore) UpdateSessionStatus(_ context.Context, _ int64, _ string) error {
	return m.updateStatusErr
}

func (m *mockStore) TouchSession(_ context.Context, _ int64) error {
	return m.touchErr
}

func (m *mockStore) ListSessions(_ context.Context) ([]core.Session, error) {
	return m.sessions, m.sessionsErr
}

func (m *mockStore) ListActiveSessionsForWorkspace(_ context.Context, _ int64) ([]core.Session, error) {
	return m.sessions, m.sessionsErr
}

func (m *mockStore) CountActiveSessionsForWorkspace(_ context.Context, _ int64) (int, error) {
	return m.count, m.countErr
}

func (m *mockStore) UpdateWorkspaceMetadata(_ context.Context, _ int64, _ json.RawMessage) error {
	return m.updateMetadataErr
}

func (m *mockStore) UpdateAgentSessionID(_ context.Context, _ int64, _ string) error {
	return nil
}

type mockAdapter struct {
	name       string
	isAlive    bool
	spawnErr   error
	sendErr    error
	stopErr    error
	stopCalled bool
	sent       []string
}

func (m *mockAdapter) Name() string {
	return m.name
}

func (m *mockAdapter) Spawn(_ context.Context, _ core.SessionInfo) error {
	return m.spawnErr
}

func (m *mockAdapter) Send(_ context.Context, _ core.SessionInfo, message string) error {
	m.sent = append(m.sent, message)
	return m.sendErr
}

func (m *mockAdapter) IsAlive(_ core.SessionInfo) bool {
	return m.isAlive
}

func (m *mockAdapter) Stop(_ context.Context, _ core.SessionInfo) error {
	m.stopCalled = true
	return m.stopErr
}

type mockRegistry struct {
	adapters map[string]executor.AgentAdapter
	agents   []string
}

func (m *mockRegistry) GetAdapter(agent string) (executor.AgentAdapter, error) {
	a, ok := m.adapters[agent]
	if !ok {
		return nil, errors.New("adapter not found")
	}
	return a, nil
}

func (m *mockRegistry) KnownAgents() []string {
	return m.agents
}

func TestNewManager_AppliesDefaults(t *testing.T) {
	store := &mockStore{}
	registry := &mockRegistry{adapters: make(map[string]executor.AgentAdapter), agents: []string{"opencode"}}
	m := NewManager(store, registry, nil, ManagerConfig{})

	if m.cfg.SoftIdleTimeout != 30*time.Minute {
		t.Errorf("expected SoftIdleTimeout 30m, got %v", m.cfg.SoftIdleTimeout)
	}
	if m.cfg.HardIdleTimeout != 2*time.Hour {
		t.Errorf("expected HardIdleTimeout 2h, got %v", m.cfg.HardIdleTimeout)
	}
	if m.cfg.ReaperInterval != 5*time.Minute {
		t.Errorf("expected ReaperInterval 5m, got %v", m.cfg.ReaperInterval)
	}
}

func TestGetOrCreateActiveSession_CreatesNewWhenNone(t *testing.T) {
	sessionID := int64(42)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
		session: &core.Session{
			ID:          sessionID,
			WorkspaceID: 1,
			Agent:       "opencode",
			TmuxSession: "ai-chat-lab-a3f2",
			Slug:        "a3f2",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	sess, info, err := m.GetOrCreateActiveSession(context.Background(), "user1", "telegram", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("expected session, got nil")
	}
	if info == nil {
		t.Fatal("expected session info, got nil")
	}
	if info.Workspace != "lab" {
		t.Errorf("expected workspace 'lab', got %q", info.Workspace)
	}
}

func TestGetOrCreateActiveSession_ReusesExisting(t *testing.T) {
	sessionID := int64(42)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
			SessionID:   sessionID,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
		session: &core.Session{
			ID:          sessionID,
			WorkspaceID: 1,
			Agent:       "opencode",
			TmuxSession: "ai-chat-lab-a3f2",
			Slug:        "a3f2",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	sess, _, err := m.GetOrCreateActiveSession(context.Background(), "user1", "telegram", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != sessionID {
		t.Errorf("expected session ID %d, got %d", sessionID, sess.ID)
	}
}

func TestGetOrCreateActiveSession_CreatesNewWhenDead(t *testing.T) {
	oldSessionID := int64(42)
	newSessionID := int64(99)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
			SessionID:   oldSessionID,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
		session: &core.Session{
			ID:          newSessionID,
			WorkspaceID: 1,
			Agent:       "opencode",
			TmuxSession: "ai-chat-lab-b4c5",
			Slug:        "b4c5",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: false}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	sess, _, err := m.GetOrCreateActiveSession(context.Background(), "user1", "telegram", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != newSessionID {
		t.Errorf("expected new session ID %d, got %d", newSessionID, sess.ID)
	}
}

func TestClearSession_AdapterAware(t *testing.T) {
	sessionID := int64(42)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
			SessionID:   sessionID,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
		session: &core.Session{
			ID:          sessionID,
			WorkspaceID: 1,
			Agent:       "opencode",
			TmuxSession: "ai-chat-lab-a3f2",
			Slug:        "a3f2",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	info, err := m.ClearSession(context.Background(), "user1", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected session info, got nil")
	}
}

func TestClearSession_CopilotStateless(t *testing.T) {
	sessionID := int64(42)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
			SessionID:   sessionID,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
		session: &core.Session{
			ID:          sessionID,
			WorkspaceID: 1,
			Agent:       "copilot",
			TmuxSession: "ai-chat-lab-a3f2",
			Slug:        "a3f2",
		},
	}

	adapter := &mockAdapter{name: "copilot", isAlive: true}
	opencodeAdapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{
			"copilot":  adapter,
			"opencode": opencodeAdapter,
		},
		agents: []string{"copilot", "opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	info, err := m.ClearSession(context.Background(), "user1", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected session info, got nil")
	}
	if info.Agent != "copilot" {
		t.Fatalf("expected clear to preserve agent, got %q", info.Agent)
	}
}

func TestKillSession(t *testing.T) {
	sessionID := int64(42)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
			SessionID:   sessionID,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
		session: &core.Session{
			ID:          sessionID,
			WorkspaceID: 1,
			Agent:       "opencode",
			TmuxSession: "ai-chat-lab-a3f2",
			Slug:        "a3f2",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	err := m.KillSession(context.Background(), "user1", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetAgent_UnknownAgent(t *testing.T) {
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	_, err := m.SetAgent(context.Background(), "user1", "telegram", "unknown")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestSetAgent_KillsDifferentAgentSession(t *testing.T) {
	sessionID := int64(42)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
			SessionID:   sessionID,
		},
		workspace: &core.Workspace{
			ID:       1,
			Name:     "lab",
			Path:     "/path/to/lab",
			Metadata: json.RawMessage(`{}`),
		},
		session: &core.Session{
			ID:          sessionID,
			WorkspaceID: 1,
			Agent:       "opencode",
			TmuxSession: "ai-chat-lab-a3f2",
			Slug:        "a3f2",
		},
	}

	opencodeAdapter := &mockAdapter{name: "opencode", isAlive: true}
	copilotAdapter := &mockAdapter{name: "copilot", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{
			"opencode": opencodeAdapter,
			"copilot":  copilotAdapter,
		},
		agents: []string{"copilot", "opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	_, err := m.SetAgent(context.Background(), "user1", "telegram", "copilot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetStatus(t *testing.T) {
	sessionID := int64(42)
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID:    "user1",
			Channel:     "telegram",
			WorkspaceID: 1,
			SessionID:   sessionID,
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
		session: &core.Session{
			ID:          sessionID,
			WorkspaceID: 1,
			Agent:       "opencode",
			TmuxSession: "ai-chat-lab-a3f2",
			Slug:        "a3f2",
		},
		count: 3,
	}

	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	info, err := m.GetStatus(context.Background(), "user1", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Workspace.Name != "lab" {
		t.Errorf("expected workspace 'lab', got %q", info.Workspace.Name)
	}
	if info.SessionCount != 3 {
		t.Errorf("expected session count 3, got %d", info.SessionCount)
	}
}

func TestCreateSessionForSenderActivatesNewSession(t *testing.T) {
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       &core.Workspace{ID: 1, Name: "lab", Path: "/path/to/lab"},
		session:         &core.Session{ID: 77, WorkspaceID: 1, Agent: "opencode", TmuxSession: "ai-chat-lab-z9y8", Slug: "z9y8"},
	}
	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{adapters: map[string]executor.AgentAdapter{"opencode": adapter}, agents: []string{"opencode"}}
	m := NewManager(store, registry, nil, ManagerConfig{})

	info, err := m.CreateSessionForSender(context.Background(), "user1", "telegram", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil || info.Name == "" {
		t.Fatal("expected session info")
	}
	if len(store.setActiveSessionIDs) != 2 {
		t.Fatalf("expected active session mapping update, got %v", store.setActiveSessionIDs)
	}
	if store.setActiveSessionIDs[0] != 1 || store.setActiveSessionIDs[1] != 77 {
		t.Fatalf("unexpected mapping args: %v", store.setActiveSessionIDs)
	}
}

func TestSwitchActiveSessionSetsRequestedMapping(t *testing.T) {
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       &core.Workspace{ID: 1, Name: "lab", Path: "/path/to/lab"},
		session:         &core.Session{ID: 88, WorkspaceID: 1, Agent: "opencode", TmuxSession: "ai-chat-lab-q1w2", Slug: "q1w2"},
	}
	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{adapters: map[string]executor.AgentAdapter{"opencode": adapter}, agents: []string{"opencode"}}
	m := NewManager(store, registry, nil, ManagerConfig{})

	if err := m.SwitchActiveSession(context.Background(), "user1", "telegram", 1, 88); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.setActiveSessionIDs) != 2 {
		t.Fatalf("expected mapping update, got %v", store.setActiveSessionIDs)
	}
	if store.setActiveSessionIDs[0] != 1 || store.setActiveSessionIDs[1] != 88 {
		t.Fatalf("unexpected mapping args: %v", store.setActiveSessionIDs)
	}
}

func TestGetStatusClearsStaleMapping(t *testing.T) {
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		activeSession:   &core.ActiveWorkspaceSession{SenderID: "user1", Channel: "telegram", WorkspaceID: 1, SessionID: 42},
		workspace:       &core.Workspace{ID: 1, Name: "lab", Path: "/path/to/lab"},
		sessionErr:      core.ErrNotFound,
		count:           1,
	}
	registry := &mockRegistry{adapters: map[string]executor.AgentAdapter{}, agents: []string{"opencode"}}
	m := NewManager(store, registry, nil, ManagerConfig{})

	info, err := m.GetStatus(context.Background(), "user1", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ActiveSession != nil {
		t.Fatal("expected stale active session to be cleared")
	}
	if len(store.clearedWorkspaceIDs) == 0 || store.clearedWorkspaceIDs[0] != 1 {
		t.Fatalf("expected stale mapping clear, got %v", store.clearedWorkspaceIDs)
	}
}

func TestResponseCh(t *testing.T) {
	store := &mockStore{}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	ch := m.ResponseCh()
	if ch == nil {
		t.Fatal("expected response channel, got nil")
	}
}

func TestSendReturnsSecurityConfirmation(t *testing.T) {
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()},
		session:         &core.Session{ID: 42, WorkspaceID: 1, Agent: "opencode", TmuxSession: "ai-chat-lab-a3f2", Slug: "a3f2"},
	}
	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{adapters: map[string]executor.AgentAdapter{"opencode": adapter}, agents: []string{"opencode"}}
	m := NewManager(store, registry, executor.NewSecurityProxy(), ManagerConfig{ResponsesDir: filepath.Join(t.TempDir(), "responses")})

	err := m.Send(context.Background(), "user1", "telegram", "my password is hunter2")
	var decisionErr *core.SecurityDecisionError
	if !errors.As(err, &decisionErr) {
		t.Fatalf("expected security decision error, got %v", err)
	}
	if decisionErr.Decision.Action != core.SecurityActionConfirm {
		t.Fatalf("unexpected action: %s", decisionErr.Decision.Action)
	}
	if decisionErr.Decision.PendingID == "" {
		t.Fatal("expected pending token")
	}
}

func TestHandleSecurityDecisionApprovedSendsMessage(t *testing.T) {
	responsesDir := filepath.Join(t.TempDir(), "responses")
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()},
		session:         &core.Session{ID: 42, WorkspaceID: 1, Agent: "opencode", TmuxSession: "ai-chat-lab-a3f2", Slug: "a3f2"},
	}
	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{adapters: map[string]executor.AgentAdapter{"opencode": adapter}, agents: []string{"opencode"}}
	m := NewManager(store, registry, executor.NewSecurityProxy(), ManagerConfig{ResponsesDir: responsesDir})
	if _, err := executor.NewResponseFile(responsesDir, *m.buildSessionInfo(store.workspace, store.session)); err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err := m.Send(context.Background(), "user1", "telegram", "my password is hunter2")
	var decisionErr *core.SecurityDecisionError
	if !errors.As(err, &decisionErr) {
		t.Fatalf("expected security decision error, got %v", err)
	}

	text, err := m.HandleSecurityDecision(context.Background(), "user1", "telegram", decisionErr.Decision.PendingID, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Message approved and sent." {
		t.Fatalf("unexpected text: %q", text)
	}
	rf, err := executor.ReadResponseFile(filepath.Join(responsesDir, "ai-chat-lab-a3f2.json"))
	if err != nil {
		t.Fatalf("reading response file: %v", err)
	}
	if len(rf.Messages) != 1 || rf.Messages[0].Role != "user" {
		t.Fatalf("unexpected messages: %+v", rf.Messages)
	}
}

func TestHandleSecurityDecisionUnauthorizedAttemptDoesNotConsumeToken(t *testing.T) {
	responsesDir := filepath.Join(t.TempDir(), "responses")
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()},
		session:         &core.Session{ID: 42, WorkspaceID: 1, Agent: "opencode", TmuxSession: "ai-chat-lab-a3f2", Slug: "a3f2"},
	}
	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{adapters: map[string]executor.AgentAdapter{"opencode": adapter}, agents: []string{"opencode"}}
	m := NewManager(store, registry, executor.NewSecurityProxy(), ManagerConfig{ResponsesDir: responsesDir})
	if _, err := executor.NewResponseFile(responsesDir, *m.buildSessionInfo(store.workspace, store.session)); err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err := m.Send(context.Background(), "user1", "telegram", "my password is hunter2")
	var decisionErr *core.SecurityDecisionError
	if !errors.As(err, &decisionErr) {
		t.Fatalf("expected security decision error, got %v", err)
	}

	text, err := m.HandleSecurityDecision(context.Background(), "user2", "telegram", decisionErr.Decision.PendingID, true)
	if err != nil {
		t.Fatalf("unexpected error from unauthorized decision: %v", err)
	}
	if text != "That confirmation has expired or was already used." {
		t.Fatalf("unexpected unauthorized text: %q", text)
	}

	text, err = m.HandleSecurityDecision(context.Background(), "user1", "telegram", decisionErr.Decision.PendingID, true)
	if err != nil {
		t.Fatalf("unexpected error approving after unauthorized attempt: %v", err)
	}
	if text != "Message approved and sent." {
		t.Fatalf("unexpected approval text after unauthorized attempt: %q", text)
	}

	rf, err := executor.ReadResponseFile(filepath.Join(responsesDir, "ai-chat-lab-a3f2.json"))
	if err != nil {
		t.Fatalf("reading response file: %v", err)
	}
	if len(rf.Messages) != 1 || rf.Messages[0].Role != "user" {
		t.Fatalf("unexpected messages after authorized approval: %+v", rf.Messages)
	}
}

func TestSendDoesNotPersistUndeliveredPrompt(t *testing.T) {
	responsesDir := filepath.Join(t.TempDir(), "responses")
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()},
		session:         &core.Session{ID: 42, WorkspaceID: 1, Agent: "opencode", TmuxSession: "ai-chat-lab-a3f2", Slug: "a3f2"},
	}
	adapter := &mockAdapter{name: "opencode", isAlive: true, sendErr: errors.New("send failed")}
	registry := &mockRegistry{adapters: map[string]executor.AgentAdapter{"opencode": adapter}, agents: []string{"opencode"}}
	m := NewManager(store, registry, executor.NewSecurityProxy(), ManagerConfig{ResponsesDir: responsesDir})
	if _, err := executor.NewResponseFile(responsesDir, *m.buildSessionInfo(store.workspace, store.session)); err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err := m.Send(context.Background(), "user1", "telegram", "hello")
	if err == nil {
		t.Fatal("expected send error")
	}
	if len(adapter.sent) != 1 || adapter.sent[0] != "hello" {
		t.Fatalf("adapter sent = %v", adapter.sent)
	}

	rf, err := executor.ReadResponseFile(filepath.Join(responsesDir, "ai-chat-lab-a3f2.json"))
	if err != nil {
		t.Fatalf("reading response file: %v", err)
	}
	if len(rf.Messages) != 0 {
		t.Fatalf("expected no persisted messages after failed send, got %+v", rf.Messages)
	}
}

// mockStreamingAdapter implements both AgentAdapter and StreamingAdapter.
type mockStreamingAdapter struct {
	mockAdapter
	agentSessionID string
	events         []core.AgentEvent
	sendStreamErr  error
	abortCalled    bool
}

func (m *mockStreamingAdapter) SendStream(_ context.Context, _ core.SessionInfo, _ string) (<-chan core.AgentEvent, error) {
	if m.sendStreamErr != nil {
		return nil, m.sendStreamErr
	}
	ch := make(chan core.AgentEvent, len(m.events))
	for _, evt := range m.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (m *mockStreamingAdapter) AbortStream(_ context.Context, _ core.SessionInfo) error {
	m.abortCalled = true
	return nil
}

func (m *mockStreamingAdapter) GetAgentSessionID(_ core.SessionInfo) string {
	id := m.agentSessionID
	m.agentSessionID = "" // read-once semantics
	return id
}

func TestDispatchStreaming_WritesResponseFile(t *testing.T) {
	responsesDir := filepath.Join(t.TempDir(), "responses")
	ws := &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()}
	sess := &core.Session{
		ID:          42,
		WorkspaceID: 1,
		Agent:       "opencode",
		TmuxSession: "ai-chat-lab-s1t2",
		Slug:        "s1t2",
	}
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       ws,
		session:         sess,
	}

	events := []core.AgentEvent{
		{Type: core.EventBusy},
		{Type: core.EventStepStart},
		{Type: core.EventTextDelta, Text: "Hello "},
		{Type: core.EventTextDelta, Text: "world"},
		{Type: core.EventText, Text: "Hello world"},
		{Type: core.EventStepFinish, Tokens: &core.TokenUsage{Input: 100, Output: 50}, Cost: 0.01, Reason: "stop"},
		{Type: core.EventIdle},
	}
	adapter := &mockStreamingAdapter{
		mockAdapter: mockAdapter{name: "opencode", isAlive: true},
		events:      events,
	}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{ResponsesDir: responsesDir})
	info := m.buildSessionInfo(ws, sess)
	if _, err := executor.NewResponseFile(responsesDir, *info); err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err := m.dispatchMessage(context.Background(), sess, info, "test message")
	if err != nil {
		t.Fatalf("dispatchMessage error: %v", err)
	}

	rf, err := executor.ReadResponseFile(info.ResponseFile)
	if err != nil {
		t.Fatalf("reading response file: %v", err)
	}
	if len(rf.Messages) != 2 {
		t.Fatalf("expected 2 messages (user + agent), got %d: %+v", len(rf.Messages), rf.Messages)
	}
	if rf.Messages[0].Role != "user" || rf.Messages[0].Content != "test message" {
		t.Errorf("unexpected user message: %+v", rf.Messages[0])
	}
	if rf.Messages[1].Role != "agent" || rf.Messages[1].Content != "Hello world" {
		t.Errorf("unexpected agent message: %+v", rf.Messages[1])
	}
}

func TestDispatchStreaming_DeltaFallback(t *testing.T) {
	responsesDir := filepath.Join(t.TempDir(), "responses")
	ws := &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()}
	sess := &core.Session{
		ID:          43,
		WorkspaceID: 1,
		Agent:       "opencode",
		TmuxSession: "ai-chat-lab-d3e4",
		Slug:        "d3e4",
	}
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       ws,
		session:         sess,
	}

	// No EventText, only deltas.
	events := []core.AgentEvent{
		{Type: core.EventTextDelta, Text: "Only "},
		{Type: core.EventTextDelta, Text: "deltas"},
		{Type: core.EventIdle},
	}
	adapter := &mockStreamingAdapter{
		mockAdapter: mockAdapter{name: "opencode", isAlive: true},
		events:      events,
	}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{ResponsesDir: responsesDir})
	info := m.buildSessionInfo(ws, sess)
	if _, err := executor.NewResponseFile(responsesDir, *info); err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err := m.dispatchMessage(context.Background(), sess, info, "ping")
	if err != nil {
		t.Fatalf("dispatchMessage error: %v", err)
	}

	rf, err := executor.ReadResponseFile(info.ResponseFile)
	if err != nil {
		t.Fatalf("reading response file: %v", err)
	}
	if len(rf.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(rf.Messages))
	}
	if rf.Messages[1].Content != "Only deltas" {
		t.Errorf("expected delta fallback text %q, got %q", "Only deltas", rf.Messages[1].Content)
	}
}

func TestDispatchStreaming_ErrorEvent(t *testing.T) {
	responsesDir := filepath.Join(t.TempDir(), "responses")
	ws := &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()}
	sess := &core.Session{
		ID:          44,
		WorkspaceID: 1,
		Agent:       "opencode",
		TmuxSession: "ai-chat-lab-e5f6",
		Slug:        "e5f6",
	}
	store := &mockStore{
		activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
		workspace:       ws,
		session:         sess,
	}

	events := []core.AgentEvent{
		{Type: core.EventTextDelta, Text: "partial"},
		{Type: core.EventError, Text: "something went wrong"},
	}
	adapter := &mockStreamingAdapter{
		mockAdapter: mockAdapter{name: "opencode", isAlive: true},
		events:      events,
	}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{ResponsesDir: responsesDir})
	info := m.buildSessionInfo(ws, sess)
	if _, err := executor.NewResponseFile(responsesDir, *info); err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err := m.dispatchMessage(context.Background(), sess, info, "trigger error")
	if err == nil {
		t.Fatal("expected error from EventError")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("expected error to contain agent error text, got: %v", err)
	}
}

// mockStoreTracking extends mockStore to track UpdateAgentSessionID calls.
type mockStoreTracking struct {
	mockStore
	updatedAgentSessionIDs map[int64]string
}

func (m *mockStoreTracking) UpdateAgentSessionID(_ context.Context, id int64, agentSessionID string) error {
	if m.updatedAgentSessionIDs == nil {
		m.updatedAgentSessionIDs = make(map[int64]string)
	}
	m.updatedAgentSessionIDs[id] = agentSessionID
	return nil
}

func TestCreateSessionLocked_PersistsAgentSessionID(t *testing.T) {
	store := &mockStoreTracking{
		mockStore: mockStore{
			activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
			workspace:       &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()},
			session:         &core.Session{ID: 55, WorkspaceID: 1, Agent: "opencode", TmuxSession: "ai-chat-lab-g7h8", Slug: "g7h8"},
		},
	}

	adapter := &mockStreamingAdapter{
		mockAdapter:    mockAdapter{name: "opencode", isAlive: true},
		agentSessionID: "ses_test123",
	}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	sess, info, err := m.createSessionLocked(context.Background(), 1, store.workspace, "opencode")
	if err != nil {
		t.Fatalf("createSessionLocked error: %v", err)
	}
	if sess == nil || info == nil {
		t.Fatal("expected session and info")
	}

	// Verify UpdateAgentSessionID was called.
	storedID, ok := store.updatedAgentSessionIDs[55]
	if !ok {
		t.Fatal("expected UpdateAgentSessionID to be called")
	}
	if storedID != "ses_test123" {
		t.Errorf("expected agent session ID %q, got %q", "ses_test123", storedID)
	}

	// Verify in-memory session has the ID.
	if sess.AgentSessionID != "ses_test123" {
		t.Errorf("expected in-memory AgentSessionID %q, got %q", "ses_test123", sess.AgentSessionID)
	}

	// Verify info also has it.
	if info.AgentSessionID != "ses_test123" {
		t.Errorf("expected info AgentSessionID %q, got %q", "ses_test123", info.AgentSessionID)
	}
}

func TestCreateSessionLocked_NonStreamingAdapter_SkipsAgentSessionID(t *testing.T) {
	store := &mockStoreTracking{
		mockStore: mockStore{
			activeWorkspace: &core.ActiveWorkspace{SenderID: "user1", Channel: "telegram", WorkspaceID: 1},
			workspace:       &core.Workspace{ID: 1, Name: "lab", Path: t.TempDir()},
			session:         &core.Session{ID: 66, WorkspaceID: 1, Agent: "copilot", TmuxSession: "ai-chat-lab-j9k0", Slug: "j9k0"},
		},
	}

	adapter := &mockAdapter{name: "copilot", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"copilot": adapter},
		agents:   []string{"copilot"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	sess, _, err := m.createSessionLocked(context.Background(), 1, store.workspace, "copilot")
	if err != nil {
		t.Fatalf("createSessionLocked error: %v", err)
	}

	// Non-streaming adapter should NOT trigger UpdateAgentSessionID.
	if len(store.updatedAgentSessionIDs) != 0 {
		t.Errorf("expected no UpdateAgentSessionID calls, got %v", store.updatedAgentSessionIDs)
	}
	if sess.AgentSessionID != "" {
		t.Errorf("expected empty AgentSessionID, got %q", sess.AgentSessionID)
	}
}
