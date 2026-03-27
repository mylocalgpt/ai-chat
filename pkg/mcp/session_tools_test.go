package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockExecutor implements the Executor interface for testing.
type mockExecutor struct {
	killCalls  []executorCall
	spawnCalls []executorCall
}

type executorCall struct {
	workspaceID int64
	agent       string
}

func (m *mockExecutor) KillSession(_ context.Context, workspaceID int64, agent string) error {
	m.killCalls = append(m.killCalls, executorCall{workspaceID, agent})
	return nil
}

func (m *mockExecutor) SpawnSession(_ context.Context, ws core.Workspace, agent string) error {
	m.spawnCalls = append(m.spawnCalls, executorCall{ws.ID, agent})
	return nil
}

func (m *mockExecutor) Execute(_ context.Context, ws core.Workspace, agent, message string) (string, error) {
	return "ok", nil
}

func TestSessionList(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	// Add workspaces and sessions.
	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.workspaces["proj2"] = &core.Workspace{ID: 2, Name: "proj2", Path: "/tmp"}
	ms.sessions = []core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "claude", TmuxSession: "proj1-claude", Status: "active", LastActivity: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: 2, WorkspaceID: 2, Agent: "opencode", TmuxSession: "proj2-opencode", Status: "idle"},
	}

	res, _, err := srv.handleSessionList(context.Background(), &gomcp.CallToolRequest{}, SessionListInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var entries []SessionListEntry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].WorkspaceName != "proj1" {
		t.Errorf("expected workspace name 'proj1', got %q", entries[0].WorkspaceName)
	}
}

func TestSessionListEmpty(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	res, _, err := srv.handleSessionList(context.Background(), &gomcp.CallToolRequest{}, SessionListInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var entries []SessionListEntry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}
}

func TestSessionRestartNilExecutor(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil) // no executor

	_, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "test",
		Agent:         "claude",
	})
	if err == nil {
		t.Error("expected error for nil executor")
	}
}

func TestSessionRestart(t *testing.T) {
	ms := newMockStore()
	exec := &mockExecutor{}
	srv := NewServer(ms, &ServerConfig{}, WithExecutor(exec))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}

	res, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "test",
		Agent:         "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(exec.killCalls) != 1 || exec.killCalls[0].workspaceID != 1 || exec.killCalls[0].agent != "claude" {
		t.Errorf("expected KillSession(1, claude), got %v", exec.killCalls)
	}
	if len(exec.spawnCalls) != 1 || exec.spawnCalls[0].workspaceID != 1 || exec.spawnCalls[0].agent != "claude" {
		t.Errorf("expected SpawnSession(1, claude), got %v", exec.spawnCalls)
	}
	if res == nil {
		t.Error("expected result")
	}
}

func TestSessionRestartMissingWorkspace(t *testing.T) {
	ms := newMockStore()
	exec := &mockExecutor{}
	srv := NewServer(ms, &ServerConfig{}, WithExecutor(exec))

	_, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "nope",
		Agent:         "claude",
	})
	if err == nil {
		t.Error("expected error for missing workspace")
	}
}

func TestSessionRestartNoAgent(t *testing.T) {
	ms := newMockStore()
	exec := &mockExecutor{}
	srv := NewServer(ms, &ServerConfig{}, WithExecutor(exec))

	// No default_agent in metadata.
	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}

	_, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "test",
	})
	if err == nil {
		t.Error("expected error when no agent specified")
	}
}

func TestSessionRestartDefaultAgent(t *testing.T) {
	ms := newMockStore()
	exec := &mockExecutor{}
	srv := NewServer(ms, &ServerConfig{}, WithExecutor(exec))

	meta, _ := json.Marshal(map[string]any{"default_agent": "opencode"})
	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp", Metadata: meta}

	_, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.spawnCalls) != 1 || exec.spawnCalls[0].agent != "opencode" {
		t.Errorf("expected SpawnSession with opencode, got %v", exec.spawnCalls)
	}
}

func TestSessionKill(t *testing.T) {
	ms := newMockStore()
	exec := &mockExecutor{}
	srv := NewServer(ms, &ServerConfig{}, WithExecutor(exec))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}

	res, _, err := srv.handleSessionKill(context.Background(), &gomcp.CallToolRequest{}, SessionKillInput{
		WorkspaceName: "test",
		Agent:         "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.killCalls) != 1 {
		t.Errorf("expected 1 kill call, got %d", len(exec.killCalls))
	}
	if res == nil {
		t.Error("expected result")
	}
}

func TestSessionKillNilExecutor(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleSessionKill(context.Background(), &gomcp.CallToolRequest{}, SessionKillInput{
		WorkspaceName: "test",
		Agent:         "claude",
	})
	if err == nil {
		t.Error("expected error for nil executor")
	}
}
