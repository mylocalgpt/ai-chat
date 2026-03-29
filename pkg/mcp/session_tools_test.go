package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mockSessionManager struct {
	killCalls   []int64
	createCalls []createCall
	clearCalls  []int64
	sendCalls   []sendCall
	switchCalls []struct{ workspaceID, sessionID int64 }
	sendErr     error
	approveText string
	approveErr  error
	approveArgs []approveCall
}

type approveCall struct {
	pendingID string
	approved  bool
}

type createCall struct {
	workspace core.Workspace
	agent     string
}

type sendCall struct {
	sessionID int64
	message   string
}

func (m *mockSessionManager) CreateSession(_ context.Context, ws core.Workspace, agent string) (*core.Session, error) {
	m.createCalls = append(m.createCalls, createCall{workspace: ws, agent: agent})
	return &core.Session{ID: 1, WorkspaceID: ws.ID, Agent: agent, Slug: "abc1", TmuxSession: "ai-chat-" + ws.Name + "-abc1"}, nil
}

func (m *mockSessionManager) ClearSession(_ context.Context, sessionID int64) (*core.Session, error) {
	m.clearCalls = append(m.clearCalls, sessionID)
	return &core.Session{ID: 2, WorkspaceID: 1, Agent: "opencode", Slug: "def2", TmuxSession: "ai-chat-test-def2"}, nil
}

func (m *mockSessionManager) KillSession(_ context.Context, sessionID int64) error {
	m.killCalls = append(m.killCalls, sessionID)
	return nil
}

func (m *mockSessionManager) Send(_ context.Context, sessionID int64, message string) error {
	m.sendCalls = append(m.sendCalls, sendCall{sessionID: sessionID, message: message})
	return m.sendErr
}

func (m *mockSessionManager) ApproveSend(_ context.Context, pendingID string, approved bool) (string, error) {
	m.approveArgs = append(m.approveArgs, approveCall{pendingID: pendingID, approved: approved})
	if m.approveErr != nil {
		return "", m.approveErr
	}
	if m.approveText == "" {
		return "Message approved and sent.", nil
	}
	return m.approveText, nil
}

func (m *mockSessionManager) SwitchSession(_ context.Context, workspaceID, sessionID int64) (*core.Session, error) {
	m.switchCalls = append(m.switchCalls, struct{ workspaceID, sessionID int64 }{workspaceID: workspaceID, sessionID: sessionID})
	return &core.Session{ID: sessionID, WorkspaceID: workspaceID, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-proj1-abc1"}, nil
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
	ms.activeByChannel["mcp:system"] = 1
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-test-abc1", Status: "active"}}

	_, _, err := srv.handleSessionClear(context.Background(), &gomcp.CallToolRequest{}, SessionClearInput{
		Workspace:   "test",
		SessionName: "abc1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.clearCalls) != 1 {
		t.Errorf("expected 1 clear call, got %d", len(sm.clearCalls))
	}
}

func TestSessionClearUsesActiveWorkspaceSessionByDefault(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.activeByChannel["mcp:system"] = 1
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-test-abc1", Status: "active"}}

	_, _, err := srv.handleSessionClear(context.Background(), &gomcp.CallToolRequest{}, SessionClearInput{Workspace: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sm.clearCalls) != 1 {
		t.Fatalf("expected 1 clear call, got %d", len(sm.clearCalls))
	}
	if sm.clearCalls[0] != 10 {
		t.Fatalf("expected active session ID 10, got %d", sm.clearCalls[0])
	}
}

func TestSessionClearRequiresMatchingActiveWorkspaceForFallback(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.workspaces["other"] = &core.Workspace{ID: 2, Name: "other", Path: "/tmp"}
	ms.activeByChannel["mcp:system"] = 1
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-test-abc1", Status: "active"}}

	_, _, err := srv.handleSessionClear(context.Background(), &gomcp.CallToolRequest{}, SessionClearInput{Workspace: "other"})
	if err == nil {
		t.Fatal("expected error when workspace is not active and no session_name is provided")
	}
}

func TestSessionKill(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["test"] = &core.Workspace{ID: 1, Name: "test", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-test-abc1"}}

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
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-proj1-abc1"}}

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

func TestAgentSendReturnsStructuredConfirmation(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{sendErr: &core.SecurityDecisionError{Decision: core.SecurityDecision{Action: core.SecurityActionConfirm, PendingID: "pending-123", Reason: "confirm it"}}}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-proj1-abc1"}}

	res, _, err := srv.handleAgentSend(context.Background(), &gomcp.CallToolRequest{}, AgentSendInput{SessionName: "abc1", Message: "my password is hunter2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected MCP error result for confirmation")
	}
	payload, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured payload, got %T", res.StructuredContent)
	}
	if payload["status"] != "confirmation_required" || payload["pending_id"] != "pending-123" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestAgentSendApproval(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{approveText: "Message approved and sent."}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	res, _, err := srv.handleAgentSendApproval(context.Background(), &gomcp.CallToolRequest{}, AgentSendApprovalInput{PendingID: "pending-123", Approve: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sm.approveArgs) != 1 || sm.approveArgs[0].pendingID != "pending-123" || !sm.approveArgs[0].approved {
		t.Fatalf("unexpected approve args: %+v", sm.approveArgs)
	}
	if res.IsError {
		t.Fatal("did not expect MCP error result")
	}
	tc := res.Content[0].(*gomcp.TextContent)
	if tc.Text != "Message approved and sent." {
		t.Fatalf("unexpected text: %q", tc.Text)
	}
}

func TestSessionSwitch(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-proj1-abc1"}}

	res, _, err := srv.handleSessionSwitch(context.Background(), &gomcp.CallToolRequest{}, SessionSwitchInput{Workspace: "proj1", Session: "abc1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sm.switchCalls) != 1 || sm.switchCalls[0].workspaceID != 1 || sm.switchCalls[0].sessionID != 10 {
		t.Fatalf("unexpected switch calls: %+v", sm.switchCalls)
	}
	tc := res.Content[0].(*gomcp.TextContent)
	var payload map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &payload); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if payload["workspace"] != "proj1" {
		t.Fatalf("unexpected payload: %v", payload)
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

	_, _, err = srv.handleAgentSendApproval(context.Background(), &gomcp.CallToolRequest{}, AgentSendApprovalInput{PendingID: "pending-123", Approve: true})
	if err == nil {
		t.Error("expected error for nil session manager")
	}
}

func TestAgentSendPropagatesUnexpectedError(t *testing.T) {
	ms := newMockStore()
	sm := &mockSessionManager{sendErr: errors.New("boom")}
	srv := NewServer(ms, &MCPConfig{}, WithSessionManager(sm))

	ms.workspaces["proj1"] = &core.Workspace{ID: 1, Name: "proj1", Path: "/tmp"}
	ms.sessions = []core.Session{{ID: 10, WorkspaceID: 1, Agent: "opencode", Slug: "abc1", TmuxSession: "ai-chat-proj1-abc1"}}

	_, _, err := srv.handleAgentSend(context.Background(), &gomcp.CallToolRequest{}, AgentSendInput{SessionName: "abc1", Message: "hello"})
	if err == nil {
		t.Fatal("expected error")
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
