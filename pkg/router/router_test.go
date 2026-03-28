package router

import (
	"context"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestParse_ValidCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantArgs []string
	}{
		{
			name:     "simple command",
			input:    "/workspaces",
			wantName: "workspaces",
			wantArgs: nil,
		},
		{
			name:     "command with args",
			input:    "/switch my-workspace",
			wantName: "switch",
			wantArgs: []string{"my-workspace"},
		},
		{
			name:     "command with multiple args",
			input:    "/agent opencode copilot",
			wantName: "agent",
			wantArgs: []string{"opencode", "copilot"},
		},
		{
			name:     "command with bot mention",
			input:    "/workspaces@mybot",
			wantName: "workspaces",
			wantArgs: nil,
		},
		{
			name:     "command with args and bot mention",
			input:    "/switch ws@mybot",
			wantName: "switch",
			wantArgs: []string{"ws@mybot"},
		},
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
			for i := range cmd.Args {
				if cmd.Args[i] != tt.wantArgs[i] {
					t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestParse_InvalidCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"bare slash", "/"},
		{"no slash", "workspaces"},
		{"plain text", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := Parse(tt.input)
			if ok {
				t.Fatalf("Parse(%q) returned true, want false", tt.input)
			}
		})
	}
}

type mockSessionManager struct {
	sendCalled          bool
	sendMessage         string
	createSessionCalled bool
	clearSessionCalled  bool
	killSessionCalled   bool
	getStatusCalled     bool
	setAgentCalled      bool
	setAgentName        string
}

func (m *mockSessionManager) Send(ctx context.Context, senderID, channel, message string) error {
	m.sendCalled = true
	m.sendMessage = message
	return nil
}

func (m *mockSessionManager) CreateSession(ctx context.Context, workspaceID int64, agent string) (*core.SessionInfo, error) {
	m.createSessionCalled = true
	return &core.SessionInfo{Name: "test-session", Workspace: "test-ws"}, nil
}

func (m *mockSessionManager) ClearSession(ctx context.Context, senderID, channel string) (*core.SessionInfo, error) {
	m.clearSessionCalled = true
	return &core.SessionInfo{Name: "cleared-session"}, nil
}

func (m *mockSessionManager) KillSession(ctx context.Context, senderID, channel string) error {
	m.killSessionCalled = true
	return nil
}

func (m *mockSessionManager) GetStatus(ctx context.Context, senderID, channel string) (*StatusInfo, error) {
	m.getStatusCalled = true
	return &StatusInfo{Agent: "opencode", SessionCount: 1}, nil
}

func (m *mockSessionManager) SetAgent(ctx context.Context, senderID, channel, agent string) error {
	m.setAgentCalled = true
	m.setAgentName = agent
	return nil
}

func TestRoute_NonCommand(t *testing.T) {
	mockSM := &mockSessionManager{}
	r := &Router{sessionMgr: mockSM}

	_, err := r.Route(context.Background(), core.InboundMessage{
		Content:  "hello world",
		SenderID: "user1",
		Channel:  "telegram",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mockSM.sendCalled {
		t.Error("expected Send to be called for non-command message")
	}
}

func TestRoute_NonCommand_NoSessionManager(t *testing.T) {
	r := &Router{sessionMgr: nil}

	_, err := r.Route(context.Background(), core.InboundMessage{
		Content:  "hello world",
		SenderID: "user1",
		Channel:  "telegram",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoute_UnknownCommand(t *testing.T) {
	r := &Router{}

	resp, err := r.Route(context.Background(), core.InboundMessage{
		Content:  "/unknown",
		SenderID: "user1",
		Channel:  "telegram",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp == "" {
		t.Fatal("expected response for unknown command")
	}
}

func TestRoute_AgentCommand(t *testing.T) {
	mockSM := &mockSessionManager{}
	r := &Router{sessionMgr: mockSM}

	resp, err := r.Route(context.Background(), core.InboundMessage{
		Content:  "/agent opencode",
		SenderID: "user1",
		Channel:  "telegram",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mockSM.setAgentCalled {
		t.Error("expected SetAgent to be called")
	}
	if mockSM.setAgentName != "opencode" {
		t.Errorf("agent name = %q, want %q", mockSM.setAgentName, "opencode")
	}
	if resp == "" {
		t.Fatal("expected response for agent command")
	}
}

func TestRoute_AgentCommand_NoArgs(t *testing.T) {
	r := &Router{}

	resp, err := r.Route(context.Background(), core.InboundMessage{
		Content:  "/agent",
		SenderID: "user1",
		Channel:  "telegram",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp != "Usage: /agent <name>" {
		t.Errorf("response = %q, want usage message", resp)
	}
}

func TestRoute_NewCommand_NoSessionManager(t *testing.T) {
	r := &Router{sessionMgr: nil}

	resp, err := r.Route(context.Background(), core.InboundMessage{
		Content:  "/new",
		SenderID: "user1",
		Channel:  "telegram",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp != "Session manager not configured." {
		t.Errorf("response = %q, want not configured message", resp)
	}
}

func TestRoute_SwitchCommand_NoArgs(t *testing.T) {
	r := &Router{}

	resp, err := r.Route(context.Background(), core.InboundMessage{
		Content:  "/switch",
		SenderID: "user1",
		Channel:  "telegram",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp != "Usage: /switch <workspace>" {
		t.Errorf("response = %q, want usage message", resp)
	}
}
