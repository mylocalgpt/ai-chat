package executor

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestParseSSEEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		data      string
		want      core.AgentEvent
		wantOK    bool
	}{
		{
			name:      "text delta",
			eventType: "message.part.delta",
			data:      `{"field":"text","delta":"Hi"}`,
			want:      core.AgentEvent{Type: core.EventTextDelta, Text: "Hi"},
			wantOK:    true,
		},
		{
			name:      "non-text delta ignored",
			eventType: "message.part.delta",
			data:      `{"field":"tool","delta":"something"}`,
			want:      core.AgentEvent{},
			wantOK:    false,
		},
		{
			name:      "text part updated",
			eventType: "message.part.updated",
			data:      `{"type":"text","text":"Complete response"}`,
			want:      core.AgentEvent{Type: core.EventText, Text: "Complete response"},
			wantOK:    true,
		},
		{
			name:      "tool-use part updated",
			eventType: "message.part.updated",
			data:      `{"type":"tool-use","toolName":"Read","input":{"path":"/tmp/foo"}}`,
			want:      core.AgentEvent{Type: core.EventToolUse, ToolName: "Read", ToolInput: `{"path":"/tmp/foo"}`},
			wantOK:    true,
		},
		{
			name:      "tool-result part updated",
			eventType: "message.part.updated",
			data:      `{"type":"tool-result","toolName":"Read","output":"file contents here"}`,
			want:      core.AgentEvent{Type: core.EventToolResult, ToolName: "Read", ToolOutput: "file contents here"},
			wantOK:    true,
		},
		{
			name:      "step-start part updated",
			eventType: "message.part.updated",
			data:      `{"type":"step-start","stepId":"abc123"}`,
			want:      core.AgentEvent{Type: core.EventStepStart},
			wantOK:    true,
		},
		{
			name:      "step-finish part updated",
			eventType: "message.part.updated",
			data:      `{"type":"step-finish","reason":"stop","tokens":{"input":1234,"output":567},"cost":0.02}`,
			want: core.AgentEvent{
				Type:   core.EventStepFinish,
				Tokens: &core.TokenUsage{Input: 1234, Output: 567},
				Cost:   0.02,
				Reason: "stop",
			},
			wantOK: true,
		},
		{
			name:      "session status busy",
			eventType: "session.status",
			data:      `{"type":"busy"}`,
			want:      core.AgentEvent{Type: core.EventBusy},
			wantOK:    true,
		},
		{
			name:      "session status idle",
			eventType: "session.status",
			data:      `{"type":"idle"}`,
			want:      core.AgentEvent{Type: core.EventIdle},
			wantOK:    true,
		},
		{
			name:      "server heartbeat ignored",
			eventType: "server.heartbeat",
			data:      `{}`,
			want:      core.AgentEvent{},
			wantOK:    false,
		},
		{
			name:      "server connected ignored",
			eventType: "server.connected",
			data:      `{}`,
			want:      core.AgentEvent{},
			wantOK:    false,
		},
		{
			name:      "unknown event type ignored",
			eventType: "some.future.event",
			data:      `{"foo":"bar"}`,
			want:      core.AgentEvent{},
			wantOK:    false,
		},
		{
			name:      "malformed JSON ignored",
			eventType: "message.part.delta",
			data:      `{not valid json`,
			want:      core.AgentEvent{},
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSSEEvent(tt.eventType, []byte(tt.data))
			if ok != tt.wantOK {
				t.Fatalf("parseSSEEvent() ok = %v, wantOK %v", ok, tt.wantOK)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Text != tt.want.Text {
				t.Errorf("Text = %q, want %q", got.Text, tt.want.Text)
			}
			if got.ToolName != tt.want.ToolName {
				t.Errorf("ToolName = %q, want %q", got.ToolName, tt.want.ToolName)
			}
			if got.ToolInput != tt.want.ToolInput {
				t.Errorf("ToolInput = %q, want %q", got.ToolInput, tt.want.ToolInput)
			}
			if got.ToolOutput != tt.want.ToolOutput {
				t.Errorf("ToolOutput = %q, want %q", got.ToolOutput, tt.want.ToolOutput)
			}
			if got.Cost != tt.want.Cost {
				t.Errorf("Cost = %v, want %v", got.Cost, tt.want.Cost)
			}
			if got.Reason != tt.want.Reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.want.Reason)
			}
			if tt.want.Tokens == nil && got.Tokens != nil {
				t.Errorf("Tokens = %v, want nil", got.Tokens)
			}
			if tt.want.Tokens != nil {
				if got.Tokens == nil {
					t.Fatalf("Tokens = nil, want %v", tt.want.Tokens)
				}
				if *got.Tokens != *tt.want.Tokens {
					t.Errorf("Tokens = %v, want %v", *got.Tokens, *tt.want.Tokens)
				}
			}
		})
	}
}

// newSSEStream creates an SSEStream from a string for testing.
func newSSEStream(s string) *SSEStream {
	r := io.NopCloser(strings.NewReader(s))
	return &SSEStream{
		reader: bufio.NewReader(r),
		body:   r,
	}
}

func TestSSEStreamNext(t *testing.T) {
	// Two events back-to-back.
	input := "event: message.part.delta\ndata: {\"field\":\"text\",\"delta\":\"Hi\"}\n\nevent: session.status\ndata: {\"type\":\"idle\"}\n\n"
	stream := newSSEStream(input)
	defer func() { _ = stream.Close() }()

	// First event.
	eventType, data, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error on first event: %v", err)
	}
	if eventType != "message.part.delta" {
		t.Errorf("first event type = %q, want %q", eventType, "message.part.delta")
	}
	if string(data) != `{"field":"text","delta":"Hi"}` {
		t.Errorf("first event data = %q, want %q", string(data), `{"field":"text","delta":"Hi"}`)
	}

	// Second event.
	eventType, data, err = stream.Next()
	if err != nil {
		t.Fatalf("unexpected error on second event: %v", err)
	}
	if eventType != "session.status" {
		t.Errorf("second event type = %q, want %q", eventType, "session.status")
	}
	if string(data) != `{"type":"idle"}` {
		t.Errorf("second event data = %q, want %q", string(data), `{"type":"idle"}`)
	}

	// EOF.
	_, _, err = stream.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestSSEStreamNext_HeartbeatThenEvent(t *testing.T) {
	// Heartbeat followed by a real event.
	input := "event: server.heartbeat\ndata: {}\n\nevent: session.status\ndata: {\"type\":\"busy\"}\n\n"
	stream := newSSEStream(input)
	defer func() { _ = stream.Close() }()

	// First event is the heartbeat.
	eventType, data, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventType != "server.heartbeat" {
		t.Errorf("event type = %q, want %q", eventType, "server.heartbeat")
	}
	if string(data) != "{}" {
		t.Errorf("data = %q, want %q", string(data), "{}")
	}

	// Second event is the real one.
	eventType, data, err = stream.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventType != "session.status" {
		t.Errorf("event type = %q, want %q", eventType, "session.status")
	}
	if string(data) != `{"type":"busy"}` {
		t.Errorf("data = %q, want %q", string(data), `{"type":"busy"}`)
	}
}

func TestSSEStreamNext_MultiLineData(t *testing.T) {
	// Event with multiple data: lines.
	input := "event: message.part.updated\ndata: {\"type\":\ndata: \"text\",\"text\":\"hello\"}\n\n"
	stream := newSSEStream(input)
	defer func() { _ = stream.Close() }()

	eventType, data, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventType != "message.part.updated" {
		t.Errorf("event type = %q, want %q", eventType, "message.part.updated")
	}
	// Multiple data lines should be joined with \n.
	expected := "{\"type\":\n\"text\",\"text\":\"hello\"}"
	if string(data) != expected {
		t.Errorf("data = %q, want %q", string(data), expected)
	}
}

func TestSSEStreamNext_Comments(t *testing.T) {
	// SSE comment lines (starting with ':') should be ignored.
	input := ": this is a comment\n: another comment\nevent: session.status\ndata: {\"type\":\"idle\"}\n\n"
	stream := newSSEStream(input)
	defer func() { _ = stream.Close() }()

	eventType, data, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventType != "session.status" {
		t.Errorf("event type = %q, want %q", eventType, "session.status")
	}
	if string(data) != `{"type":"idle"}` {
		t.Errorf("data = %q, want %q", string(data), `{"type":"idle"}`)
	}
}
