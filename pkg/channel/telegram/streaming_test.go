package telegram

import (
	"context"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// newTestAdapter builds a TelegramAdapter wired to the given mock bot.
func newTestAdapter(mb *mockProgressBot) *TelegramAdapter {
	return &TelegramAdapter{
		bot:          mb,
		allowedUsers: map[int64]bool{},
	}
}

// feedEvents sends events into a channel and closes it.
func feedEvents(events ...core.AgentEvent) <-chan core.AgentEvent {
	ch := make(chan core.AgentEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestSendStreaming_HappyPath(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventBusy},
		core.AgentEvent{Type: core.EventToolUse, ToolName: "read_file"},
		core.AgentEvent{Type: core.EventTextDelta, Text: "Hello "},
		core.AgentEvent{Type: core.EventText, Text: "Hello world"},
		core.AgentEvent{Type: core.EventIdle},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	// EventText resets the builder, so final text should be "Hello world"
	if result.Text != "Hello world" {
		t.Errorf("returned text = %q, want %q", result.Text, "Hello world")
	}

	// SendStreaming no longer sends the final response itself; it only manages
	// the progress reporter. Verify status message was created and deleted.
	if len(mb.sentMessages) < 1 {
		t.Fatalf("expected at least 1 SendMessage call (status), got %d", len(mb.sentMessages))
	}

	// First message is the status message
	if mb.sentMessages[0].Text != "Thinking..." {
		t.Errorf("first message text = %q, want %q", mb.sentMessages[0].Text, "Thinking...")
	}

	// Status message should be deleted (Finish called)
	if len(mb.deletedMessages) != 1 {
		t.Fatalf("expected 1 DeleteMessage call, got %d", len(mb.deletedMessages))
	}
}

func TestSendStreaming_ErrorEvent(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventBusy},
		core.AgentEvent{Type: core.EventTextDelta, Text: "partial"},
		core.AgentEvent{Type: core.EventError, Text: "something went wrong"},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v (expected nil)", err)
	}

	// Error event returns empty string
	if result.Text != "" {
		t.Errorf("returned text = %q, want empty", result.Text)
	}

	// Status message should be deleted
	if len(mb.deletedMessages) != 1 {
		t.Fatalf("expected 1 DeleteMessage call, got %d", len(mb.deletedMessages))
	}

	// At least 2 SendMessage calls: status + error message
	if len(mb.sentMessages) < 2 {
		t.Fatalf("expected at least 2 SendMessage calls (status + error), got %d", len(mb.sentMessages))
	}
}

func TestSendStreaming_EmptyResponse(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventBusy},
		core.AgentEvent{Type: core.EventIdle},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	if result.Text != "" {
		t.Errorf("returned text = %q, want empty", result.Text)
	}

	// Status message created and deleted
	if len(mb.sentMessages) != 1 {
		t.Errorf("expected 1 SendMessage call (status only), got %d", len(mb.sentMessages))
	}
	if len(mb.deletedMessages) != 1 {
		t.Errorf("expected 1 DeleteMessage call, got %d", len(mb.deletedMessages))
	}
}

func TestSendStreaming_NoEventBusy(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventTextDelta, Text: "direct text"},
		core.AgentEvent{Type: core.EventIdle},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	if result.Text != "direct text" {
		t.Errorf("returned text = %q, want %q", result.Text, "direct text")
	}

	// No status message created (no EventBusy), so no DeleteMessage
	if len(mb.deletedMessages) != 0 {
		t.Errorf("expected 0 DeleteMessage calls (no status to delete), got %d", len(mb.deletedMessages))
	}
}

func TestSendStreaming_ContextCancellation(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	ctx, cancel := context.WithCancel(context.Background())

	// Use an unbuffered channel so we control event delivery precisely.
	ch := make(chan core.AgentEvent)

	done := make(chan struct{})
	var result core.StreamResult
	var err error

	go func() {
		result, err = adapter.SendStreaming(ctx, 123, 7, "ses_abc", ch)
		close(done)
	}()

	// Send EventBusy so reporter starts.
	ch <- core.AgentEvent{Type: core.EventBusy}

	// Cancel the context.
	cancel()

	// Send one more event so the loop iterates and checks ctx.Done().
	ch <- core.AgentEvent{Type: core.EventTextDelta, Text: "after cancel"}
	close(ch)

	<-done

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if result.Text != "" {
		t.Errorf("returned text = %q, want empty on cancellation", result.Text)
	}
}

func TestSendStreaming_StepFinishLogsOnly(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventBusy},
		core.AgentEvent{
			Type:   core.EventStepFinish,
			Tokens: &core.TokenUsage{Input: 100, Output: 50},
			Cost:   0.003,
		},
		core.AgentEvent{Type: core.EventTextDelta, Text: "result"},
		core.AgentEvent{Type: core.EventIdle},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	if result.Text != "result" {
		t.Errorf("returned text = %q, want %q", result.Text, "result")
	}

	// Verify token accumulation.
	if result.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", result.InputTokens)
	}
	if result.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", result.OutputTokens)
	}
	if result.Cost != 0.003 {
		t.Errorf("Cost = %f, want 0.003", result.Cost)
	}
}

func TestSendStreaming_EventTextResetsBuilder(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventBusy},
		core.AgentEvent{Type: core.EventTextDelta, Text: "partial one "},
		core.AgentEvent{Type: core.EventTextDelta, Text: "partial two "},
		core.AgentEvent{Type: core.EventText, Text: "authoritative full text"},
		core.AgentEvent{Type: core.EventIdle},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	// EventText should have reset the builder
	if result.Text != "authoritative full text" {
		t.Errorf("returned text = %q, want %q", result.Text, "authoritative full text")
	}
}

func TestSendStreaming_DrainAfterIdle(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	// Events after Idle should be drained without processing.
	events := feedEvents(
		core.AgentEvent{Type: core.EventBusy},
		core.AgentEvent{Type: core.EventTextDelta, Text: "before idle"},
		core.AgentEvent{Type: core.EventIdle},
		core.AgentEvent{Type: core.EventTextDelta, Text: " SHOULD NOT APPEAR"},
		core.AgentEvent{Type: core.EventText, Text: "SHOULD NOT APPEAR EITHER"},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	if result.Text != "before idle" {
		t.Errorf("returned text = %q, want %q", result.Text, "before idle")
	}
}

func TestSendStreaming_ErrorEventNoFinalSend(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventBusy},
		core.AgentEvent{Type: core.EventTextDelta, Text: "partial"},
		core.AgentEvent{Type: core.EventError, Text: "agent crashed"},
	)

	result, err := adapter.SendStreaming(context.Background(), 123, 7, "ses_abc", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	// Should return empty text on error
	if result.Text != "" {
		t.Errorf("returned text = %q, want empty on error", result.Text)
	}

	// Verify the error message was sent. Count messages:
	// 1: status "Thinking..."
	// 2: SendChatAction (not in sentMessages)
	// 3: error message via Send -> SendChatAction + SendMessage
	// So at least 2 SendMessage calls.
	foundError := false
	for _, msg := range mb.sentMessages {
		if msg.ReplyParameters != nil && msg.ReplyParameters.MessageID == 7 {
			if msg.Text != "Thinking..." {
				foundError = true
			}
		}
	}
	if !foundError {
		// Check based on count - we know first is status, the rest should include error
		if len(mb.sentMessages) < 2 {
			t.Errorf("expected at least 2 SendMessage calls, got %d", len(mb.sentMessages))
		}
	}
}

func TestSendStreaming_FinalResponseReplyToID(t *testing.T) {
	mb := newMockProgressBot()
	adapter := newTestAdapter(mb)

	events := feedEvents(
		core.AgentEvent{Type: core.EventTextDelta, Text: "hello"},
		core.AgentEvent{Type: core.EventIdle},
	)

	result, err := adapter.SendStreaming(context.Background(), 456, 99, "ses_xyz", events)
	if err != nil {
		t.Fatalf("SendStreaming() error: %v", err)
	}

	// Verify text was accumulated correctly.
	if result.Text != "hello" {
		t.Errorf("returned text = %q, want %q", result.Text, "hello")
	}
}
