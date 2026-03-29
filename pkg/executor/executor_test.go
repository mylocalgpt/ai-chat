package executor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type mockTmux struct {
	sessions map[string]bool
	captured string
}

func newMockTmux() *mockTmux {
	return &mockTmux{sessions: make(map[string]bool)}
}

func (m *mockTmux) NewSession(name, workDir string) error {
	m.sessions[name] = true
	return nil
}

func (m *mockTmux) HasSession(name string) bool {
	return m.sessions[name]
}

func (m *mockTmux) KillSession(name string) error {
	delete(m.sessions, name)
	return nil
}

func (m *mockTmux) SendKeys(session, text string) error {
	return nil
}

func (m *mockTmux) CapturePaneRaw(session string, lines int) (string, error) {
	return m.captured, nil
}

func (m *mockTmux) ListSessions() ([]string, error) {
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	return names, nil
}

type mockStore struct {
	sessions []*core.Session
	nextID   int64
}

func newMockStore() *mockStore {
	return &mockStore{nextID: 1}
}

func (m *mockStore) CreateSession(_ context.Context, workspaceID int64, agent, slug, tmuxSession string) (*core.Session, error) {
	sess := &core.Session{
		ID:          m.nextID,
		WorkspaceID: workspaceID,
		Agent:       agent,
		Slug:        slug,
		TmuxSession: tmuxSession,
		Status:      string(core.SessionActive),
	}
	m.nextID++
	m.sessions = append(m.sessions, sess)
	return sess, nil
}

func (m *mockStore) GetActiveSession(_ context.Context, workspaceID int64) (*core.Session, error) {
	for _, sess := range m.sessions {
		if sess.WorkspaceID == workspaceID && sess.Status == string(core.SessionActive) {
			return sess, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) UpdateSessionStatus(_ context.Context, id int64, status string) error {
	for _, sess := range m.sessions {
		if sess.ID == id {
			sess.Status = status
			return nil
		}
	}
	return nil
}

func (m *mockStore) TouchSession(_ context.Context, id int64) error {
	return nil
}

func (m *mockStore) ListSessions(_ context.Context) ([]core.Session, error) {
	result := make([]core.Session, len(m.sessions))
	for i, sess := range m.sessions {
		result[i] = *sess
	}
	return result, nil
}

func (m *mockStore) GetWorkspaceByID(_ context.Context, id int64) (*core.Workspace, error) {
	return &core.Workspace{ID: id, Name: "mock-workspace", Path: "/tmp/mock"}, nil
}

func TestExecuteRemoteWorkspaceReturnsError(t *testing.T) {
	exec := NewExecutor(newMockStore(), newMockTmux(), NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy()), "")

	ws := core.Workspace{ID: 1, Name: "remote-proj", Path: "/tmp", Host: "mac"}
	_, err := exec.Execute(context.Background(), ws, "opencode", "hello")
	if !errors.Is(err, ErrRemoteNotSupported) {
		t.Errorf("expected ErrRemoteNotSupported, got %v", err)
	}
}

func TestSessionInfoEnrichment(t *testing.T) {
	tmx := newMockTmux()
	tmx.sessions["ai-chat-proj-a1b2"] = true

	st := newMockStore()
	st.sessions = []*core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "opencode-tmux", Slug: "a1b2", TmuxSession: "ai-chat-proj-a1b2", Status: string(core.SessionActive)},
		{ID: 2, WorkspaceID: 2, Agent: "opencode-tmux", Slug: "c3d4", TmuxSession: "ai-chat-old-c3d4", Status: string(core.SessionCrashed)},
	}

	exec := NewExecutor(st, tmx, NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy()), "")
	infos, err := exec.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}

	if !infos[0].IsAlive {
		t.Error("session 1 should be alive")
	}
	if infos[1].IsAlive {
		t.Error("session 2 should not be alive")
	}
}

func TestKillSessionNoActiveSession(t *testing.T) {
	exec := NewExecutor(newMockStore(), newMockTmux(), NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy()), "")

	err := exec.KillSession(context.Background(), 999, "opencode")
	if err != nil {
		t.Errorf("KillSession with no active session: %v", err)
	}
}

type mockAdapter struct {
	name     string
	alive    bool
	sendErr  error
	spawnErr error
	stopErr  error
	lastSend string
	lastSess core.SessionInfo
	spawned  bool
	stopped  bool
}

func (m *mockAdapter) Name() string {
	return m.name
}

func (m *mockAdapter) Spawn(_ context.Context, session core.SessionInfo) error {
	m.spawned = true
	m.lastSess = session
	_, err := NewResponseFile(filepath.Dir(session.ResponseFile), session)
	if err != nil {
		return err
	}
	return m.spawnErr
}

func (m *mockAdapter) Send(_ context.Context, session core.SessionInfo, message string) error {
	m.lastSend = message
	m.lastSess = session
	if m.sendErr != nil {
		return m.sendErr
	}
	_ = AppendMessage(session.ResponseFile, ResponseMessage{
		Role:      "agent",
		Content:   "mock response: " + message,
		Timestamp: time.Now().UTC(),
	})
	return nil
}

func (m *mockAdapter) IsAlive(_ core.SessionInfo) bool {
	return m.alive
}

func (m *mockAdapter) Stop(_ context.Context, _ core.SessionInfo) error {
	m.stopped = true
	return m.stopErr
}

func TestExecuteAdapterPath(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	adapter := &mockAdapter{name: "test-adapter", alive: true}
	reg.RegisterAdapter("test-adapter", adapter)

	tmpDir := t.TempDir()
	exec := NewExecutor(st, tmx, reg, tmpDir)

	ws := core.Workspace{ID: 1, Name: "proj", Path: "/tmp"}
	resp, err := exec.Execute(context.Background(), ws, "test-adapter", "hello")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp != "mock response: hello" {
		t.Errorf("got %q, want %q", resp, "mock response: hello")
	}

	if len(st.sessions) != 1 {
		t.Errorf("adapter path should create session, got %d", len(st.sessions))
	}

	if !adapter.spawned {
		t.Error("adapter should have been spawned")
	}

	sess := st.sessions[0]
	responsePath := ResponseFilePath(tmpDir, sess.TmuxSession)
	if _, err := ReadResponseFile(responsePath); err != nil {
		t.Errorf("response file should exist: %v", err)
	}
}

func TestExecuteAdapterReusesSession(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	adapter := &mockAdapter{name: "test-adapter", alive: true}
	reg.RegisterAdapter("test-adapter", adapter)

	tmpDir := t.TempDir()
	exec := NewExecutor(st, tmx, reg, tmpDir)

	ws := core.Workspace{ID: 1, Name: "proj", Path: "/tmp"}

	_, err := exec.Execute(context.Background(), ws, "test-adapter", "first")
	if err != nil {
		t.Fatalf("Execute 1: %v", err)
	}

	adapter.spawned = false

	_, err = exec.Execute(context.Background(), ws, "test-adapter", "second")
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}

	if adapter.spawned {
		t.Error("adapter should not have been spawned again")
	}

	if len(st.sessions) != 1 {
		t.Errorf("should have 1 session, got %d", len(st.sessions))
	}
}

func TestExecuteAdapterRespawnsDeadSession(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	adapter := &mockAdapter{name: "test-adapter", alive: true}
	reg.RegisterAdapter("test-adapter", adapter)

	tmpDir := t.TempDir()
	exec := NewExecutor(st, tmx, reg, tmpDir)

	ws := core.Workspace{ID: 1, Name: "proj", Path: "/tmp"}

	_, err := exec.Execute(context.Background(), ws, "test-adapter", "first")
	if err != nil {
		t.Fatalf("Execute 1: %v", err)
	}

	adapter.alive = false
	adapter.spawned = false

	_, err = exec.Execute(context.Background(), ws, "test-adapter", "second")
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}

	if !adapter.spawned {
		t.Error("adapter should have been spawned again")
	}
}

func TestExecuteDoesNotPersistUndeliveredPrompt(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	adapter := &mockAdapter{name: "test-adapter", alive: true, sendErr: errors.New("send failed")}
	reg.RegisterAdapter("test-adapter", adapter)

	tmpDir := t.TempDir()
	exec := NewExecutor(st, tmx, reg, tmpDir)

	ws := core.Workspace{ID: 1, Name: "proj", Path: "/tmp"}
	_, err := exec.Execute(context.Background(), ws, "test-adapter", "hello")
	if err == nil {
		t.Fatal("expected send error")
	}

	if len(st.sessions) != 1 {
		t.Fatalf("expected created session, got %d", len(st.sessions))
	}
	responsePath := ResponseFilePath(tmpDir, st.sessions[0].TmuxSession)
	rf, err := ReadResponseFile(responsePath)
	if err != nil {
		t.Fatalf("ReadResponseFile: %v", err)
	}
	if len(rf.Messages) != 0 {
		t.Fatalf("expected no persisted messages after failed send, got %+v", rf.Messages)
	}
}
