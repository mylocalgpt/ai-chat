package executor

import (
	"context"
	"errors"
	"testing"

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
	exec := NewExecutor(newMockStore(), newMockTmux(), NewHarnessRegistry(NewTmux()))

	ws := core.Workspace{ID: 1, Name: "remote-proj", Path: "/tmp", Host: "mac"}
	_, err := exec.Execute(context.Background(), ws, "claude", "hello")
	if !errors.Is(err, ErrRemoteNotSupported) {
		t.Errorf("expected ErrRemoteNotSupported, got %v", err)
	}
}

func TestExecuteCLIPath(t *testing.T) {
	tmx := newMockTmux()
	st := newMockStore()
	reg := NewHarnessRegistry(NewTmux())
	reg.RegisterCLI("test-cli", &mockCLIHarness{response: "test response"})

	exec := NewExecutor(st, tmx, reg)

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
		{ID: 1, WorkspaceID: 1, Agent: "claude", Slug: "a1b2", TmuxSession: "ai-chat-proj-a1b2", Status: string(core.SessionActive)},
		{ID: 2, WorkspaceID: 2, Agent: "claude", Slug: "c3d4", TmuxSession: "ai-chat-old-c3d4", Status: string(core.SessionCrashed)},
	}

	exec := NewExecutor(st, tmx, NewHarnessRegistry(NewTmux()))
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
	exec := NewExecutor(newMockStore(), newMockTmux(), NewHarnessRegistry(NewTmux()))

	// Killing a session when none exists should not error.
	err := exec.KillSession(context.Background(), 999, "claude")
	if err != nil {
		t.Errorf("KillSession with no active session: %v", err)
	}
}
