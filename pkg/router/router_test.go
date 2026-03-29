package router

import (
	"context"
	"errors"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	st := store.New(db)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func seedWorkspace(t *testing.T, st *store.Store, name string) *core.Workspace {
	t.Helper()
	ws, err := st.CreateWorkspace(context.Background(), name, "/tmp/"+name, "")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return ws
}

func TestParse_ValidCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantArgs []string
	}{
		{name: "simple command", input: "/workspaces", wantName: "workspaces"},
		{name: "command with args", input: "/switch my-workspace", wantName: "switch", wantArgs: []string{"my-workspace"}},
		{name: "command with multiple args", input: "/agent opencode copilot", wantName: "agent", wantArgs: []string{"opencode", "copilot"}},
		{name: "command with bot mention", input: "/workspaces@mybot", wantName: "workspaces"},
		{name: "command with args and bot mention", input: "/switch ws@mybot", wantName: "switch", wantArgs: []string{"ws@mybot"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, ok := Parse(tt.input)
			if !ok {
				t.Fatalf("Parse(%q) returned false, want true", tt.input)
			}
			if cmd.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", cmd.Name, tt.wantName)
			}
			if len(cmd.Args) != len(tt.wantArgs) {
				t.Errorf("Args = %v, want %v", cmd.Args, tt.wantArgs)
			}
		})
	}
}

func TestParse_InvalidCommand(t *testing.T) {
	tests := []string{"", "/", "workspaces", "hello world"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, ok := Parse(input)
			if ok {
				t.Fatalf("Parse(%q) returned true, want false", input)
			}
		})
	}
}

type mockSessionManager struct {
	sendCalled            bool
	sendErr               error
	handleSecurityText    string
	handleSecurityErr     error
	createSessionCalled   bool
	createForSenderCalled bool
	switchSessionCalled   bool
	clearSessionCalled    bool
	clearSessionErr       error
	killSessionCalled     bool
	killSessionErr        error
	getStatusCalled       bool
	getStatusErr          error
	setAgentCalled        bool
	setAgentErr           error
}

func (m *mockSessionManager) Send(context.Context, string, string, string, string) error {
	m.sendCalled = true
	return m.sendErr
}
func (m *mockSessionManager) HandleSecurityDecision(context.Context, string, string, string, bool) (string, error) {
	if m.handleSecurityText != "" {
		return m.handleSecurityText, m.handleSecurityErr
	}
	return "Cancelled.", m.handleSecurityErr
}
func (m *mockSessionManager) CreateSession(context.Context, int64, string) (*core.SessionInfo, error) {
	m.createSessionCalled = true
	return &core.SessionInfo{Name: "test-session", Workspace: "test-ws"}, nil
}
func (m *mockSessionManager) CreateSessionForSender(context.Context, string, string, string) (*core.SessionInfo, error) {
	m.createForSenderCalled = true
	return &core.SessionInfo{Name: "test-session", Workspace: "test-ws"}, nil
}
func (m *mockSessionManager) SwitchActiveSession(context.Context, string, string, int64, int64) error {
	m.switchSessionCalled = true
	return nil
}
func (m *mockSessionManager) ClearSession(context.Context, string, string) (*core.SessionInfo, error) {
	m.clearSessionCalled = true
	if m.clearSessionErr != nil {
		return nil, m.clearSessionErr
	}
	return &core.SessionInfo{Name: "cleared-session"}, nil
}
func (m *mockSessionManager) KillSession(context.Context, string, string) error {
	m.killSessionCalled = true
	return m.killSessionErr
}
func (m *mockSessionManager) GetStatus(context.Context, string, string) (*StatusInfo, error) {
	m.getStatusCalled = true
	if m.getStatusErr != nil {
		return nil, m.getStatusErr
	}
	return &StatusInfo{Agent: "opencode", SessionCount: 1}, nil
}
func (m *mockSessionManager) SetAgent(context.Context, string, string, string) (*core.SessionInfo, error) {
	m.setAgentCalled = true
	return nil, m.setAgentErr
}

func routeMessage(t *testing.T, r *Router, content string) Result {
	t.Helper()
	result, err := r.Route(context.Background(), Request{Message: &core.InboundMessage{Content: content, SenderID: "user1", Channel: "telegram"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

func TestRouteNonCommandNoReplyOnSuccess(t *testing.T) {
	result := routeMessage(t, &Router{sessionMgr: &mockSessionManager{}}, "hello world")
	if result.Kind != ResultNoReply {
		t.Fatalf("kind = %q, want %q", result.Kind, ResultNoReply)
	}
}

func TestRouteNonCommandReturnsWorkspacePickerOnMissingWorkspace(t *testing.T) {
	st := newTestStore(t)
	seedWorkspace(t, st, "test-ws")
	result := routeMessage(t, &Router{sessionMgr: &mockSessionManager{sendErr: store.ErrNotFound}, store: st}, "hello world")
	if result.Kind != ResultWorkspacePicker {
		t.Fatalf("kind = %q, want %q", result.Kind, ResultWorkspacePicker)
	}
}

func TestRouteUnknownCommandReturnsText(t *testing.T) {
	result := routeMessage(t, &Router{}, "/unknown")
	if result.Kind != ResultText || result.Text == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRouteAgentCommand(t *testing.T) {
	sm := &mockSessionManager{}
	result := routeMessage(t, &Router{sessionMgr: sm}, "/agent opencode")
	if !sm.setAgentCalled {
		t.Fatal("expected SetAgent to be called")
	}
	if result.Kind != ResultText {
		t.Fatalf("kind = %q, want %q", result.Kind, ResultText)
	}
}

func TestRouteStatusMissingWorkspace(t *testing.T) {
	st := newTestStore(t)
	seedWorkspace(t, st, "test-ws")
	sm := &mockSessionManager{getStatusErr: store.ErrNotFound}
	result := routeMessage(t, &Router{sessionMgr: sm, store: st}, "/status")
	if result.Kind != ResultWorkspacePicker {
		t.Fatalf("kind = %q, want %q", result.Kind, ResultWorkspacePicker)
	}
}

func TestRouteClearMissingWorkspace(t *testing.T) {
	st := newTestStore(t)
	seedWorkspace(t, st, "test-ws")
	sm := &mockSessionManager{clearSessionErr: store.ErrNotFound}
	result := routeMessage(t, &Router{sessionMgr: sm, store: st}, "/clear")
	if result.Kind != ResultWorkspacePicker {
		t.Fatalf("kind = %q, want %q", result.Kind, ResultWorkspacePicker)
	}
}

func TestRouteKillMissingWorkspace(t *testing.T) {
	st := newTestStore(t)
	seedWorkspace(t, st, "test-ws")
	sm := &mockSessionManager{killSessionErr: store.ErrNotFound}
	result := routeMessage(t, &Router{sessionMgr: sm, store: st}, "/kill")
	if result.Kind != ResultWorkspacePicker {
		t.Fatalf("kind = %q, want %q", result.Kind, ResultWorkspacePicker)
	}
}

func TestRouteNewNoSessionManager(t *testing.T) {
	result := routeMessage(t, &Router{}, "/new")
	if result.Text != "Session manager not configured." {
		t.Fatalf("text = %q", result.Text)
	}
}

func TestRouteSwitchNoArgs(t *testing.T) {
	result := routeMessage(t, &Router{}, "/switch")
	if result.Text != "Usage: /switch <workspace>" {
		t.Fatalf("text = %q", result.Text)
	}
}

func TestRouteUnexpectedError(t *testing.T) {
	_, err := (&Router{sessionMgr: &mockSessionManager{sendErr: errors.New("boom")}}).Route(context.Background(), Request{Message: &core.InboundMessage{Content: "hello world", SenderID: "user1", Channel: "telegram"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRouteReturnsSecurityConfirmationResult(t *testing.T) {
	result := routeMessage(t, &Router{sessionMgr: &mockSessionManager{sendErr: &core.SecurityDecisionError{Decision: core.SecurityDecision{Action: core.SecurityActionConfirm, PendingID: "token-123", Reason: "confirm it"}}}}, "my password is hunter2")
	if result.Kind != ResultSecurityConfirmation {
		t.Fatalf("kind = %q, want %q", result.Kind, ResultSecurityConfirmation)
	}
	if result.SecurityConfirmation == nil || result.SecurityConfirmation.Token != "token-123" {
		t.Fatalf("unexpected security confirmation: %+v", result.SecurityConfirmation)
	}
}

func TestHandleSecurityDecisionDelegatesToSessionManager(t *testing.T) {
	r := &Router{sessionMgr: &mockSessionManager{handleSecurityText: "Message approved and sent."}}
	result, err := r.HandleSecurityDecision(context.Background(), "user1", "telegram", "token-123", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Kind != ResultText || result.Text != "Message approved and sent." {
		t.Fatalf("unexpected result: %+v", result)
	}
}
