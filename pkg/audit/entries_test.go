package audit

import (
	"testing"
)

func TestInboundEntry(t *testing.T) {
	e := Inbound("telegram", "user1", "lab", "hello")
	if e.Type != "inbound" {
		t.Errorf("Type = %q, want %q", e.Type, "inbound")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Channel != "telegram" {
		t.Errorf("Channel = %q, want %q", e.Channel, "telegram")
	}
	if e.Sender != "user1" {
		t.Errorf("Sender = %q, want %q", e.Sender, "user1")
	}
	if e.Workspace != "lab" {
		t.Errorf("Workspace = %q, want %q", e.Workspace, "lab")
	}
	if e.Content != "hello" {
		t.Errorf("Content = %q, want %q", e.Content, "hello")
	}
	// Fields not set should be zero.
	if e.Agent != "" {
		t.Errorf("Agent should be empty, got %q", e.Agent)
	}
	if e.Duration != 0 {
		t.Errorf("Duration should be 0, got %d", e.Duration)
	}
}

func TestRouteEntry(t *testing.T) {
	e := Route("lab", "agent_task", "claude", 0.95)
	if e.Type != "route" {
		t.Errorf("Type = %q, want %q", e.Type, "route")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Workspace != "lab" {
		t.Errorf("Workspace = %q", e.Workspace)
	}
	if e.Action != "agent_task" {
		t.Errorf("Action = %q", e.Action)
	}
	if e.Agent != "claude" {
		t.Errorf("Agent = %q", e.Agent)
	}
	if e.Confidence != 0.95 {
		t.Errorf("Confidence = %f", e.Confidence)
	}
}

func TestAgentSendEntry(t *testing.T) {
	e := AgentSend("lab", "claude", "sess1", "do this")
	if e.Type != "agent_send" {
		t.Errorf("Type = %q, want %q", e.Type, "agent_send")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Session != "sess1" {
		t.Errorf("Session = %q", e.Session)
	}
	if e.Message != "do this" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestAgentResponseEntry(t *testing.T) {
	e := AgentResponse("lab", "claude", "sess1", 500, 1200)
	if e.Type != "agent_response" {
		t.Errorf("Type = %q, want %q", e.Type, "agent_response")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Length != 500 {
		t.Errorf("Length = %d", e.Length)
	}
	if e.Duration != 1200 {
		t.Errorf("Duration = %d", e.Duration)
	}
}

func TestOutboundEntry(t *testing.T) {
	e := Outbound("telegram", "user1", "lab", 200)
	if e.Type != "outbound" {
		t.Errorf("Type = %q, want %q", e.Type, "outbound")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Recipient != "user1" {
		t.Errorf("Recipient = %q", e.Recipient)
	}
	if e.Length != 200 {
		t.Errorf("Length = %d", e.Length)
	}
}

func TestErrorEntry(t *testing.T) {
	e := Error("lab", "something broke")
	if e.Type != "error" {
		t.Errorf("Type = %q, want %q", e.Type, "error")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Error != "something broke" {
		t.Errorf("Error = %q", e.Error)
	}
}

func TestHealthEntry(t *testing.T) {
	e := Health("lab", "active")
	if e.Type != "health" {
		t.Errorf("Type = %q, want %q", e.Type, "health")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Status != "active" {
		t.Errorf("Status = %q", e.Status)
	}
}

func TestUnauthorizedEntry(t *testing.T) {
	e := Unauthorized("telegram", "bad_user")
	if e.Type != "unauthorized" {
		t.Errorf("Type = %q, want %q", e.Type, "unauthorized")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Channel != "telegram" {
		t.Errorf("Channel = %q", e.Channel)
	}
	if e.Sender != "bad_user" {
		t.Errorf("Sender = %q", e.Sender)
	}
	// Should have no workspace.
	if e.Workspace != "" {
		t.Errorf("Workspace should be empty, got %q", e.Workspace)
	}
}
