package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mockStore struct {
	workspaces map[string]*core.Workspace
	sessions   []core.Session
	nextID     int64
	pingErr    error
}

func newMockStore() *mockStore {
	return &mockStore{
		workspaces: make(map[string]*core.Workspace),
		nextID:     1,
	}
}

func (m *mockStore) Ping(_ context.Context) error {
	if m.pingErr != nil {
		return m.pingErr
	}
	return nil
}

func (m *mockStore) CreateWorkspace(_ context.Context, name, path, host string) (*core.Workspace, error) {
	ws := &core.Workspace{ID: m.nextID, Name: name, Path: path, Host: host}
	m.nextID++
	m.workspaces[name] = ws
	return ws, nil
}

func (m *mockStore) GetWorkspace(_ context.Context, name string) (*core.Workspace, error) {
	ws, ok := m.workspaces[name]
	if !ok {
		return nil, store.ErrNotFound
	}
	return ws, nil
}

func (m *mockStore) GetWorkspaceByID(_ context.Context, id int64) (*core.Workspace, error) {
	for _, ws := range m.workspaces {
		if ws.ID == id {
			return ws, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) ListWorkspaces(_ context.Context) ([]core.Workspace, error) {
	var out []core.Workspace
	for _, ws := range m.workspaces {
		out = append(out, *ws)
	}
	return out, nil
}

func (m *mockStore) UpdateWorkspaceMetadata(_ context.Context, id int64, metadata json.RawMessage) error {
	for _, ws := range m.workspaces {
		if ws.ID == id {
			ws.Metadata = metadata
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *mockStore) DeleteWorkspace(_ context.Context, id int64) error {
	for name, ws := range m.workspaces {
		if ws.ID == id {
			delete(m.workspaces, name)
			return nil
		}
	}
	return nil
}

func (m *mockStore) RenameWorkspace(_ context.Context, id int64, newName string) error {
	for name, ws := range m.workspaces {
		if ws.ID == id {
			delete(m.workspaces, name)
			ws.Name = newName
			m.workspaces[newName] = ws
			return nil
		}
	}
	return nil
}

func (m *mockStore) GetActiveSession(_ context.Context, workspaceID int64) (*core.Session, error) {
	for _, sess := range m.sessions {
		if sess.WorkspaceID == workspaceID && sess.Status == "active" {
			return &sess, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) ListSessions(_ context.Context) ([]core.Session, error) {
	if m.sessions != nil {
		return m.sessions, nil
	}
	return []core.Session{}, nil
}

func (m *mockStore) ListSessionsForWorkspace(_ context.Context, workspaceID int64) ([]core.Session, error) {
	var result []core.Session
	for _, sess := range m.sessions {
		if sess.WorkspaceID == workspaceID {
			result = append(result, sess)
		}
	}
	return result, nil
}

func (m *mockStore) GetSessionByName(_ context.Context, name string) (*core.Session, error) {
	for _, sess := range m.sessions {
		if sess.TmuxSession == name {
			return &sess, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) GetSessionByReference(ctx context.Context, reference string) (*core.Session, error) {
	if wsName, slug, isFull := store.ParseSessionReference(reference); isFull {
		ws, err := m.GetWorkspace(ctx, wsName)
		if err != nil {
			return nil, err
		}
		return m.GetSessionByReferenceInWorkspace(ctx, ws.ID, slug)
	}
	var matches []core.Session
	for _, sess := range m.sessions {
		if sess.Slug == reference {
			matches = append(matches, sess)
		}
	}
	if len(matches) == 0 {
		return nil, store.ErrNotFound
	}
	if len(matches) > 1 {
		return nil, context.DeadlineExceeded
	}
	return &matches[0], nil
}

func (m *mockStore) GetSessionByReferenceInWorkspace(_ context.Context, workspaceID int64, reference string) (*core.Session, error) {
	for _, sess := range m.sessions {
		if sess.WorkspaceID == workspaceID && (sess.Slug == reference || sess.TmuxSession == reference) {
			return &sess, nil
		}
	}
	return nil, store.ErrNotFound
}

type mockNotifier struct {
	called int
}

func (n *mockNotifier) OnWorkspacesChanged() { n.called++ }

func newTestServer(st MCPStore, notifier WorkspaceChangeNotifier) *Server {
	opts := []Option{}
	if notifier != nil {
		opts = append(opts, WithNotifier(notifier))
	}
	return NewServer(st, &MCPConfig{}, opts...)
}

func TestWorkspaceRegister(t *testing.T) {
	ms := newMockStore()
	notif := &mockNotifier{}
	srv := newTestServer(ms, notif)

	dir := t.TempDir()

	res, _, err := srv.handleWorkspaceRegister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRegisterInput{
		Name: "test",
		Path: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected result content")
	}

	if _, ok := ms.workspaces["test"]; !ok {
		t.Error("workspace not created in store")
	}
	if notif.called != 1 {
		t.Errorf("expected notifier called once, got %d", notif.called)
	}
}

func TestWorkspaceRegisterDuplicate(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	dir := t.TempDir()
	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: dir}

	_, _, err := srv.handleWorkspaceRegister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRegisterInput{
		Name: "test",
		Path: dir,
	})
	if err == nil {
		t.Error("expected error for duplicate workspace")
	}
}

func TestWorkspaceRegisterBadPath(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleWorkspaceRegister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRegisterInput{
		Name: "test",
		Path: "/nonexistent/path/xyz123",
	})
	if err == nil {
		t.Error("expected error for non-existent path")
	}
}

func TestWorkspaceRegisterRemoteSkipsPathCheck(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleWorkspaceRegister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRegisterInput{
		Name: "remote-ws",
		Path: "/remote/path",
		Host: "mac",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := ms.workspaces["remote-ws"]; !ok {
		t.Error("remote workspace not created")
	}
}

func TestWorkspaceUnregister(t *testing.T) {
	ms := newMockStore()
	notif := &mockNotifier{}
	srv := newTestServer(ms, notif)

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}

	_, _, err := srv.handleWorkspaceUnregister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceUnregisterInput{Name: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := ms.workspaces["test"]; ok {
		t.Error("workspace should have been deleted")
	}
	if notif.called != 1 {
		t.Errorf("expected notifier called once, got %d", notif.called)
	}
}

func TestWorkspaceUnregisterNotFound(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleWorkspaceUnregister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceUnregisterInput{Name: "nope"})
	if err == nil {
		t.Error("expected error for missing workspace")
	}
}

func TestWorkspaceRename(t *testing.T) {
	ms := newMockStore()
	notif := &mockNotifier{}
	srv := newTestServer(ms, notif)

	ms.workspaces["old"] = &core.Workspace{ID: 1, Name: "old", Path: "/tmp"}

	_, _, err := srv.handleWorkspaceRename(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRenameInput{
		OldName: "old",
		NewName: "new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := ms.workspaces["new"]; !ok {
		t.Error("workspace not renamed in store")
	}
	if _, ok := ms.workspaces["old"]; ok {
		t.Error("old name should be gone")
	}
	if notif.called != 1 {
		t.Errorf("expected notifier called once, got %d", notif.called)
	}
}

func TestWorkspaceRenameToExisting(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	ms.workspaces["a"] = &core.Workspace{ID: 1, Name: "a", Path: "/tmp"}
	ms.workspaces["b"] = &core.Workspace{ID: 2, Name: "b", Path: "/tmp"}

	_, _, err := srv.handleWorkspaceRename(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRenameInput{
		OldName: "a",
		NewName: "b",
	})
	if err == nil {
		t.Error("expected error renaming to existing name")
	}
}

func TestWorkspaceList(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	meta, _ := json.Marshal(map[string]any{"description": "test desc", "aliases": []string{"t"}})
	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp", Metadata: meta}

	res, _, err := srv.handleWorkspaceList(context.Background(), &gomcp.CallToolRequest{}, WorkspaceListInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected result content")
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var infos []workspaceInfo
	if err := json.Unmarshal([]byte(tc.Text), &infos); err != nil {
		t.Fatalf("failed to parse list result: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(infos))
	}
	if infos[0].Description != "test desc" {
		t.Errorf("expected description 'test desc', got %q", infos[0].Description)
	}
}

func TestNotifierNilSafety(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	dir := t.TempDir()
	_, _, err := srv.handleWorkspaceRegister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRegisterInput{
		Name: "test",
		Path: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error with nil notifier: %v", err)
	}
}

func TestWorkspaceRegisterWithMetadata(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	dir := t.TempDir()
	_, _, err := srv.handleWorkspaceRegister(context.Background(), &gomcp.CallToolRequest{}, WorkspaceRegisterInput{
		Name:         "proj",
		Path:         dir,
		Description:  "my project",
		DefaultAgent: "opencode",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ws := ms.workspaces["proj"]
	var m map[string]any
	_ = json.Unmarshal(ws.Metadata, &m)
	if m["description"] != "my project" {
		t.Errorf("expected description 'my project', got %v", m["description"])
	}
	if m["default_agent"] != "opencode" {
		t.Errorf("expected default_agent 'opencode', got %v", m["default_agent"])
	}
}
