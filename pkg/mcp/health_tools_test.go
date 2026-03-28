package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockChannel implements ChannelAdapter for testing.
type mockChannel struct {
	connected bool
	sentMsgs  []core.OutboundMessage
	sendErr   error
}

func (c *mockChannel) Send(_ context.Context, msg core.OutboundMessage) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sentMsgs = append(c.sentMsgs, msg)
	return nil
}

func (c *mockChannel) IsConnected() bool {
	return c.connected
}

func TestHealthCheckHealthy(t *testing.T) {
	ms := newMockStore()
	ch := &mockChannel{connected: true}
	srv := NewServer(ms, &ServerConfig{AllowedUsers: []int64{123}}, WithChannelAdapter(ch))

	ms.workspaces["ws1"] = &core.Workspace{ID: 1, Name: "ws1", Path: "/tmp"}
	ms.sessions = []core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "opencode", Status: "active"},
	}

	res, _, err := srv.handleHealthCheck(context.Background(), &gomcp.CallToolRequest{}, HealthCheckInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output HealthCheckOutput
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if output.Status != "healthy" {
		t.Errorf("expected status healthy, got %q", output.Status)
	}
	if output.ActiveSessions != 1 {
		t.Errorf("expected 1 active session, got %d", output.ActiveSessions)
	}
	if output.TotalWorkspaces != 1 {
		t.Errorf("expected 1 workspace, got %d", output.TotalWorkspaces)
	}
}

func TestHealthCheckDBDown(t *testing.T) {
	ms := newMockStore()
	ms.pingErr = fmt.Errorf("connection refused")
	srv := newTestServer(ms, nil)

	res, _, err := srv.handleHealthCheck(context.Background(), &gomcp.CallToolRequest{}, HealthCheckInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output HealthCheckOutput
	_ = json.Unmarshal([]byte(tc.Text), &output)

	if output.Status != "unhealthy" {
		t.Errorf("expected unhealthy, got %q", output.Status)
	}

	// Verify no raw error details leak.
	if strings.Contains(tc.Text, "connection refused") {
		t.Error("raw error message should not appear in output")
	}
}

func TestHealthCheckNoChannel(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil) // no channel adapter

	res, _, err := srv.handleHealthCheck(context.Background(), &gomcp.CallToolRequest{}, HealthCheckInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output HealthCheckOutput
	_ = json.Unmarshal([]byte(tc.Text), &output)

	if output.Status != "degraded" {
		t.Errorf("expected degraded, got %q", output.Status)
	}
}

func TestHealthCheckChannelDisconnected(t *testing.T) {
	ms := newMockStore()
	ch := &mockChannel{connected: false}
	srv := NewServer(ms, &ServerConfig{}, WithChannelAdapter(ch))

	res, _, err := srv.handleHealthCheck(context.Background(), &gomcp.CallToolRequest{}, HealthCheckInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output HealthCheckOutput
	_ = json.Unmarshal([]byte(tc.Text), &output)

	if output.Status != "degraded" {
		t.Errorf("expected degraded, got %q", output.Status)
	}
}

func TestHealthCheckNoSecrets(t *testing.T) {
	ms := newMockStore()
	ch := &mockChannel{connected: true}
	srv := NewServer(ms, &ServerConfig{AllowedUsers: []int64{123}}, WithChannelAdapter(ch))

	res, _, err := srv.handleHealthCheck(context.Background(), &gomcp.CallToolRequest{}, HealthCheckInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	// Verify no tokens or keys appear in output.
	for _, pattern := range []string{"token", "api_key", "secret", "password"} {
		if strings.Contains(strings.ToLower(tc.Text), pattern) {
			t.Errorf("health output should not contain %q", pattern)
		}
	}
}

func TestSendTestMessageSuccess(t *testing.T) {
	ms := newMockStore()
	ch := &mockChannel{connected: true}
	srv := NewServer(ms, &ServerConfig{AllowedUsers: []int64{42}}, WithChannelAdapter(ch))

	res, _, err := srv.handleSendTestMessage(context.Background(), &gomcp.CallToolRequest{}, SendTestMessageInput{
		Platform: "telegram",
		Message:  "hello test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected result")
	}
	if len(ch.sentMsgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(ch.sentMsgs))
	}
	if ch.sentMsgs[0].RecipientID != "42" {
		t.Errorf("expected recipient 42, got %q", ch.sentMsgs[0].RecipientID)
	}
	if ch.sentMsgs[0].Content != "hello test" {
		t.Errorf("expected message 'hello test', got %q", ch.sentMsgs[0].Content)
	}
}

func TestSendTestMessageNilChannel(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleSendTestMessage(context.Background(), &gomcp.CallToolRequest{}, SendTestMessageInput{
		Platform: "telegram",
		Message:  "test",
	})
	if err == nil {
		t.Error("expected error for nil channel")
	}
}

func TestSendTestMessageNoAllowedUsers(t *testing.T) {
	ms := newMockStore()
	ch := &mockChannel{connected: true}
	srv := NewServer(ms, &ServerConfig{AllowedUsers: []int64{}}, WithChannelAdapter(ch))

	_, _, err := srv.handleSendTestMessage(context.Background(), &gomcp.CallToolRequest{}, SendTestMessageInput{
		Platform: "telegram",
		Message:  "test",
	})
	if err == nil {
		t.Error("expected error for empty allowed users")
	}
}

func TestSendTestMessageUnknownPlatform(t *testing.T) {
	ms := newMockStore()
	ch := &mockChannel{connected: true}
	srv := NewServer(ms, &ServerConfig{AllowedUsers: []int64{1}}, WithChannelAdapter(ch))

	_, _, err := srv.handleSendTestMessage(context.Background(), &gomcp.CallToolRequest{}, SendTestMessageInput{
		Platform: "slack",
		Message:  "test",
	})
	if err == nil {
		t.Error("expected error for unknown platform")
	}
}
