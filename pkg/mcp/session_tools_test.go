package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mockSessionManager struct {
	killCalls   []int64
	createCalls []createCall
	sendCalls   []sendCall
}

type createCall struct {
	workspace core.Workspace
	agent     string
}

type sendCall struct {
	sessionID int64
	message   string
}

func (m *mockSessionManager) ListSessions(_ context.Context) ([]core.Session, error) {
	return []core.Session{}, nil
}

func (m *mockSessionManager) ListSessionsForWorkspace(_ context.Context, _ int64) ([]core.Session, error) {
	return []core.Session{}, nil
}

func (m *mockSessionManager) GetActiveSession(_ context.Context, _ int64) (*core.Session, error) {
	return nil, nil
}

func (m *mockSessionManager) GetSessionByName(_ context.Context, _ string) (*core.Session, error) {
	return nil, nil
}

func (m *mockSessionManager) SetActiveSession(_ context.Context, _, _ int64) error {
	return nil
}

func (m *mockSessionManager) CreateSession(_ context.Context, ws core.Workspace, agent string) (*core.Session, error) {
	m.createCalls = append(m.createCalls, createCall{workspace: ws, agent: agent})
	return &core.Session{ID: 1, WorkspaceID: ws.ID, Agent: agent}, nil
}

func (m *mockSessionManager) ClearSession(_ context.Context, _ core.Session) (*core.Session, error) {
	return &core.Session{}, nil
}

func (m *mockSessionManager) KillSession(_ context.Context, sessionID int64) error {
	m.killCalls = append(m.killCalls, sessionID)
	return nil
}

func (m *mockSessionManager) Send(_ context.Context, sessionID int64, message string) error {
	m.sendCalls = append(m.sendCalls, sendCall{sessionID: sessionID, message: message})
	return nil
}

func TestSessionList(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.workspaces["proj2"] = &core.Workspace{ID: 2, Name: "proj2", Path: "/tmp"}
	ms.sessions = []core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "opencode", TmuxSession: "proj1-opencode", Status: "active", LastActivity: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
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

func TestSessionRestartNilSessionManager(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "test",
		Agent:         "opencode",
	})
	if err == nil {
		t.Error("expected error for nil session manager")
	}
}

func TestSessionRestart(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode"}}

	res, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "test",
		Agent:         "opencode",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.killCalls) != 1 || sm.killCalls[0] != 10 {
		t.Errorf("expected KillSession(10), got %v", sm.killCalls)
	}
	if len(sm.createCalls) != 1 || sm.createCalls[0].agent != "opencode" {
		t.Errorf("expected CreateSession with opencode, got %v", sm.createCalls)
	}
	if res == nil {
		t.Error("expected result")
	}
}

func TestSessionRestartMissingWorkspace(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	_, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "nope",
		Agent:         "opencode",
	})
	if err == nil {
		t.Error("expected error for missing workspace")
	}
}

func TestSessionRestartNoAgent(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

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
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	meta, _ := json.Marshal(map[string]any{"default_agent": "opencode"})
	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp", Metadata: meta}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode"}}

	_, _, err := srv.handleSessionRestart(context.Background(), &gomcp.CallToolRequest{}, SessionRestartInput{
		WorkspaceName: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sm.createCalls) != 1 || sm.createCalls[0].agent != "opencode" {
		t.Errorf("expected CreateSession with opencode, got %v", sm.createCalls)
	}
}

func TestSessionKill(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode"}}

	res, _, err := srv.handleSessionKill(context.Background(), &gomcp.CallToolRequest{}, SessionKillInput{
		WorkspaceName: "test",
		Agent:         "opencode",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sm.killCalls) != 1 {
		t.Errorf("expected 1 kill call, got %d", len(sm.killCalls))
	}
	if res == nil {
		t.Error("expected result")
	}
}

func TestAgentSend(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode"}}

	res, _, err := srv.handleAgentSend(context.Background(), &gomcp.CallToolRequest{}, AgentSendInput{
		WorkspaceName: "proj1",
		Agent:         "opencode",
		Message:       "hello agent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.sendCalls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(sm.sendCalls))
	}
	call := sm.sendCalls[0]
	if call.sessionID != 10 || call.message != "hello agent" {
		t.Errorf("unexpected send call: %+v", call)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	if tc.Text != "Message sent to agent" {
		t.Errorf("expected response %q, got %q", "Message sent to agent", tc.Text)
	}
}

func TestAgentSendFallbackAgent(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode"}}

	_, _, err := srv.handleAgentSend(context.Background(), &gomcp.CallToolRequest{}, AgentSendInput{
		WorkspaceName: "proj1",
		Message:       "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.sendCalls) != 1 || sm.sendCalls[0].sessionID != 10 {
		t.Errorf("expected send to session 10, got %+v", sm.sendCalls)
	}
}

func TestAgentSendNilSessionManager(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleAgentSend(context.Background(), &gomcp.CallToolRequest{}, AgentSendInput{
		WorkspaceName: "test",
		Message:       "hello",
	})
	if err == nil {
		t.Error("expected error for nil session manager")
	}
}

func TestSessionKillNilSessionManager(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleSessionKill(context.Background(), &gomcp.CallToolRequest{}, SessionKillInput{
		WorkspaceName: "test",
		Agent:         "opencode",
	})
	if err == nil {
		t.Error("expected error for nil session manager")
	}
}
