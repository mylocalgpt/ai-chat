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

// mockTmux implements tmuxRunner for testing.
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

// mockStore implements sessionStore for testing.
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

// mockCLIHarness implements CLIHarness for testing.
type mockCLIHarness struct {
	response string
	err      error
}

func (m *mockCLIHarness) Execute(_ context.Context, workDir, message string) (string, error) {
	return m.response, m.err
}

func TestExecuteRemoteWorkspaceReturnsError(t *testing.T) {
	exec := NewExecutor(newMockStore(), newMockTmux(), NewHarnessRegistry(NewTmux()), "")

	ws := core.Workspace{ID: 1, Name: "remote-proj", Path: "/tmp", Host: "mac"}
	_, err := exec.Execute(context.Background(), ws, "opencode", "hello")
	if !errors.Is(err, ErrRemoteNotSupported) {
		t.Errorf("expected ErrRemoteNotSupported, got %v", err)
	}
}

func TestExecuteCLIPath(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux())
	reg.RegisterCLI("test-cli", &mockCLIHarness{response: "test response"})

	exec := NewExecutor(st, tmx, reg, "")

	ws := core.Workspace{ID: 1, Name: "proj", Path: "/tmp"}
	resp, err := exec.Execute(context.Background(), ws, "test-cli", "hello")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp != "test response" {
		t.Errorf("got %q, want %q", resp, "test response")
	}

	// No sessions should have been created.
	if len(st.sessions) != 0 {
		t.Errorf("CLI path should not create sessions, got %d", len(st.sessions))
	}
}

func TestSessionInfoEnrichment(t *testing.T) {
	tmx := newMockTmux()
	tmx.sessions["ai-chat-proj-a1b2"] = true

	st := newMockStore()
	st.sessions = []*core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "opencode", Slug: "a1b2", TmuxSession: "ai-chat-proj-a1b2", Status: string(core.SessionActive)},
		{ID: 2, WorkspaceID: 2, Agent: "opencode", Slug: "c3d4", TmuxSession: "ai-chat-old-c3d4", Status: string(core.SessionCrashed)},
	}

	exec := NewExecutor(st, tmx, NewHarnessRegistry(NewTmux()), "")
	infos, err := exec.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}

	// First session has a live tmux session.
	if !infos[0].IsAlive {
		t.Error("session 1 should be alive")
	}
	// Second session's tmux session does not exist.
	if infos[1].IsAlive {
		t.Error("session 2 should not be alive")
	}
}

func TestKillSessionNoActiveSession(t *testing.T) {
	exec := NewExecutor(newMockStore(), newMockTmux(), NewHarnessRegistry(NewTmux()), "")

	// Killing a session when none exists should not error.
	err := exec.KillSession(context.Background(), 999, "opencode")
	if err != nil {
		t.Errorf("KillSession with no active session: %v", err)
	}
}

// mockAdapter implements AgentAdapter for testing.
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
	// Create the response file as a real adapter would.
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
	// Write a response to the file.
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
	reg := NewHarnessRegistry(NewTmux())
	adapter := &mockAdapter{name: "test-adapter", alive: true}
	reg.RegisterAdapter("test-adapter", adapter)

	// Use a temp directory for responses.
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

	// Session should have been created.
	if len(st.sessions) != 1 {
		t.Errorf("adapter path should create session, got %d", len(st.sessions))
	}

	// Adapter should have been spawned.
	if !adapter.spawned {
		t.Error("adapter should have been spawned")
	}

	// Response file should exist.
	sess := st.sessions[0]
	responsePath := ResponseFilePath(tmpDir, sess.TmuxSession)
	if _, err := ReadResponseFile(responsePath); err != nil {
		t.Errorf("response file should exist: %v", err)
	}
}

func TestExecuteAdapterReusesSession(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux())
	adapter := &mockAdapter{name: "test-adapter", alive: true}
	reg.RegisterAdapter("test-adapter", adapter)

	tmpDir := t.TempDir()
	exec := NewExecutor(st, tmx, reg, tmpDir)

	ws := core.Workspace{ID: 1, Name: "proj", Path: "/tmp"}

	// First call creates session.
	_, err := exec.Execute(context.Background(), ws, "test-adapter", "first")
	if err != nil {
		t.Fatalf("Execute 1: %v", err)
	}

	// Reset spawn tracking.
	adapter.spawned = false

	// Second call should reuse session.
	_, err = exec.Execute(context.Background(), ws, "test-adapter", "second")
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}

	// Should not have spawned again.
	if adapter.spawned {
		t.Error("adapter should not have been spawned again")
	}

	// Only one session should exist.
	if len(st.sessions) != 1 {
		t.Errorf("should have 1 session, got %d", len(st.sessions))
	}
}

func TestExecuteAdapterRespawnsDeadSession(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux())
	adapter := &mockAdapter{name: "test-adapter", alive: true} // alive initially
	reg.RegisterAdapter("test-adapter", adapter)

	tmpDir := t.TempDir()
	exec := NewExecutor(st, tmx, reg, tmpDir)

	ws := core.Workspace{ID: 1, Name: "proj", Path: "/tmp"}

	// First call creates session.
	_, err := exec.Execute(context.Background(), ws, "test-adapter", "first")
	if err != nil {
		t.Fatalf("Execute 1: %v", err)
	}

	// Mark as not alive for second call (session is now "dead").
	adapter.alive = false
	adapter.spawned = false

	// Second call should respawn because session is not alive.
	_, err = exec.Execute(context.Background(), ws, "test-adapter", "second")
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}

	// Should have spawned again.
	if !adapter.spawned {
		t.Error("adapter should have been spawned again")
	}
}
