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
	clearCalls  []int64
	switchCalls []switchCall
	sendCalls   []sendCall
	activeSess  *core.Session
}

type createCall struct {
	workspace core.Workspace
	agent     string
}

type switchCall struct {
	workspaceID int64
	sessionID   int64
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
	return m.activeSess, nil
}

func (m *mockSessionManager) GetSessionByName(_ context.Context, _ string) (*core.Session, error) {
	return m.activeSess, nil
}

func (m *mockSessionManager) SetActiveSession(_ context.Context, workspaceID, sessionID int64) error {
	m.switchCalls = append(m.switchCalls, switchCall{workspaceID: workspaceID, sessionID: sessionID})
	return nil
}

func (m *mockSessionManager) CreateSession(_ context.Context, ws core.Workspace, agent string) (*core.Session, error) {
	m.createCalls = append(m.createCalls, createCall{workspace: ws, agent: agent})
	return &core.Session{ID: 1, WorkspaceID: ws.ID, Agent: agent, Slug: "abc1"}, nil
}

func (m *mockSessionManager) ClearSession(_ context.Context, sess core.Session) (*core.Session, error) {
	m.clearCalls = append(m.clearCalls, sess.ID)
	return &core.Session{ID: 2, WorkspaceID: sess.WorkspaceID, Agent: sess.Agent, Slug: "def2"}, nil
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
		{ID: 1, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-proj1-abc1", Status: "active", LastActivity: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: 2, WorkspaceID: 2, Agent: "opencode", Slug: "def2", TmuxSession: "ai-chat-proj2-def2", Status: "idle"},
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
	if entries[0].Workspace != "proj1" {
		t.Errorf("expected workspace 'proj1', got %q", entries[0].Workspace)
	}
	if entries[0].Name != "ai-chat-proj1-abc1" {
		t.Errorf("expected name 'ai-chat-proj1-abc1', got %q", entries[0].Name)
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

func TestSessionListWithWorkspaceFilter(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.workspaces["proj2"] = &core.Workspace{ID: 2, Name: "proj2", Path: "/tmp"}
	ms.sessions = []core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", Status: "active"},
		{ID: 2, WorkspaceID: 2, Agent: "opencode", Slug: "def2", Status: "idle"},
	}

	res, _, err := srv.handleSessionList(context.Background(), &gomcp.CallToolRequest{}, SessionListInput{Workspace: "proj1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var entries []SessionListEntry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Workspace != "proj1" {
		t.Errorf("expected workspace 'proj1', got %q", entries[0].Workspace)
	}
}

func TestSessionSwitchBySlug(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1"}}
	sm.activeSess = &ms.sessions[0]

	_, _, err := srv.handleSessionSwitch(context.Background(), &gomcp.CallToolRequest{}, SessionSwitchInput{
		SessionName: "abc1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.switchCalls) != 1 {
		t.Errorf("expected 1 switch call, got %d", len(sm.switchCalls))
	}
	if sm.switchCalls[0].sessionID != 10 {
		t.Errorf("expected sessionID 10, got %d", sm.switchCalls[0].sessionID)
	}
}

func TestSessionSwitchByFullName(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-test-abc1"}}
	sm.activeSess = &ms.sessions[0]

	_, _, err := srv.handleSessionSwitch(context.Background(), &gomcp.CallToolRequest{}, SessionSwitchInput{
		SessionName: "ai-chat-test-abc1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.switchCalls) != 1 {
		t.Errorf("expected 1 switch call, got %d", len(sm.switchCalls))
	}
}

func TestSessionCreate(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}

	res, _, err := srv.handleSessionCreate(context.Background(), &gomcp.CallToolRequest{}, SessionCreateInput{
		Workspace: "test",
		Agent:     "opencode",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.createCalls) != 1 {
		t.Errorf("expected 1 create call, got %d", len(sm.createCalls))
	}
	if sm.createCalls[0].agent != "opencode" {
		t.Errorf("expected agent 'opencode', got %q", sm.createCalls[0].agent)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	if tc.Text == "" {
		t.Error("expected non-empty result")
	}
}

func TestSessionClear(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", Status: "active"}}

	_, _, err := srv.handleSessionClear(context.Background(), &gomcp.CallToolRequest{}, SessionClearInput{
		Workspace: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.clearCalls) != 1 {
		t.Errorf("expected 1 clear call, got %d", len(sm.clearCalls))
	}
}

func TestSessionKill(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1"}}
	sm.activeSess = &ms.sessions[0]

	_, _, err := srv.handleSessionKill(context.Background(), &gomcp.CallToolRequest{}, SessionKillInput{
		SessionName: "abc1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.killCalls) != 1 {
		t.Errorf("expected 1 kill call, got %d", len(sm.killCalls))
	}
	if sm.killCalls[0] != 10 {
		t.Errorf("expected kill sessionID 10, got %d", sm.killCalls[0])
	}
}

func TestAgentSend(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1"}}
	sm.activeSess = &ms.sessions[0]

	res, _, err := srv.handleAgentSend(context.Background(), &gomcp.CallToolRequest{}, AgentSendInput{
		SessionName: "abc1",
		Message:     "hello agent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.sendCalls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(sm.sendCalls))
	}
	if sm.sendCalls[0].sessionID != 10 || sm.sendCalls[0].message != "hello agent" {
		t.Errorf("unexpected send call: %+v", sm.sendCalls[0])
	}

	tc := res.Content[0].(*gomcp.TextContent)
	if tc.Text != "Message sent to agent" {
		t.Errorf("expected 'Message sent to agent', got %q", tc.Text)
	}
}

func TestSessionNilSessionManager(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleSessionCreate(context.Background(), &gomcp.CallToolRequest{}, SessionCreateInput{
		Workspace: "test",
		Agent:     "opencode",
	})
	if err == nil {
		t.Error("expected error for nil session manager")
	}

	_, _, err = srv.handleSessionClear(context.Background(), &gomcp.CallToolRequest{}, SessionClearInput{
		Workspace: "test",
	})
	if err == nil {
		t.Error("expected error for nil session manager")
	}

	_, _, err = srv.handleSessionKill(context.Background(), &gomcp.CallToolRequest{}, SessionKillInput{
		SessionName: "abc1",
	})
	if err == nil {
		t.Error("expected error for nil session manager")
	}

	_, _, err = srv.handleAgentSend(context.Background(), &gomcp.CallToolRequest{}, AgentSendInput{
		SessionName: "abc1",
		Message:     "hello",
	})
	if err == nil {
		t.Error("expected error for nil session manager")
	}
}

func TestHumanAge(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{"zero", time.Time{}, "unknown"},
		{"just now", time.Now().Add(-30 * time.Second), "just now"},
		{"minutes", time.Now().Add(-5 * time.Minute), "5m ago"},
		{"hours", time.Now().Add(-2 * time.Hour), "2h ago"},
		{"days", time.Now().Add(-48 * time.Hour), "2d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanAge(tt.time)
			if tt.name != "just now" && tt.name != "minutes" && tt.name != "hours" && tt.name != "days" {
				if got != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, got)
				}
			}
		})
	}
}

func TestParseSessionName(t *testing.T) {
	tests := []struct {
		input      string
		workspace  string
		slug       string
		isFullName bool
	}{
		{"ai-chat-test-abc1", "test", "abc1", true},
		{"abc1", "", "abc1", false},
		{"ai-chat-", "", "ai-chat-", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ws, slug, isFullName := parseSessionName(tt.input)
			if ws != tt.workspace {
				t.Errorf("expected workspace %q, got %q", tt.workspace, ws)
			}
			if slug != tt.slug {
				t.Errorf("expected slug %q, got %q", tt.slug, slug)
			}
			if isFullName != tt.isFullName {
				t.Errorf("expected isFullName %v, got %v", tt.isFullName, isFullName)
			}
		})
	}
}
