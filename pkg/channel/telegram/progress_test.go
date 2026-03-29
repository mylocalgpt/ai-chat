package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// mockProgressBot records all calls made by ProgressReporter.
type mockProgressBot struct {
	sentMessages    []*bot.SendMessageParams
	editedMessages  []*bot.EditMessageTextParams
	deletedMessages []*bot.DeleteMessageParams
	nextMessageID   int
	editErr         error // if set, EditMessageText returns this
}

func newMockProgressBot() *mockProgressBot {
	return &mockProgressBot{nextMessageID: 42}
}

func (b *mockProgressBot) GetMe(context.Context) (*models.User, error) {
	return &models.User{ID: 1, IsBot: true}, nil
}
func (b *mockProgressBot) Start(context.Context) {}
func (b *mockProgressBot) SendMessage(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	b.sentMessages = append(b.sentMessages, params)
	msg := &models.Message{ID: b.nextMessageID, Chat: models.Chat{ID: params.ChatID.(int64)}, Text: params.Text}
	b.nextMessageID++
	return msg, nil
}
func (b *mockProgressBot) SendChatAction(context.Context, *bot.SendChatActionParams) (bool, error) {
	return true, nil
}
func (b *mockProgressBot) SetMyCommands(context.Context, *bot.SetMyCommandsParams) (bool, error) {
	return true, nil
}
func (b *mockProgressBot) DeleteMessage(_ context.Context, params *bot.DeleteMessageParams) (bool, error) {
	b.deletedMessages = append(b.deletedMessages, params)
	return true, nil
}
func (b *mockProgressBot) EditMessageText(_ context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	if b.editErr != nil {
		return nil, b.editErr
	}
	b.editedMessages = append(b.editedMessages, params)
	return &models.Message{ID: params.MessageID, Chat: models.Chat{ID: params.ChatID.(int64)}, Text: params.Text}, nil
}
func (b *mockProgressBot) SendDocument(_ context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	msg := &models.Message{ID: b.nextMessageID, Chat: models.Chat{ID: params.ChatID.(int64)}}
	b.nextMessageID++
	return msg, nil
}

func TestProgressReporter_Start(t *testing.T) {
	mb := newMockProgressBot()
	pr := NewProgressReporter(mb, 123, 7, "sess-abc")

	if err := pr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if len(mb.sentMessages) != 1 {
		t.Fatalf("expected 1 SendMessage call, got %d", len(mb.sentMessages))
	}
	params := mb.sentMessages[0]
	if params.Text != "Thinking..." {
		t.Errorf("text = %q, want %q", params.Text, "Thinking...")
	}
	if params.ParseMode != models.ParseModeHTML {
		t.Errorf("parse mode = %v, want HTML", params.ParseMode)
	}
	if params.ReplyParameters == nil || params.ReplyParameters.MessageID != 7 {
		t.Errorf("reply parameters missing or wrong message ID")
	}
	kb, ok := params.ReplyMarkup.(*models.InlineKeyboardMarkup)
	if !ok || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatal("expected inline keyboard with 1 button")
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.Text != "Stop" {
		t.Errorf("button text = %q, want %q", btn.Text, "Stop")
	}
	if btn.CallbackData != "stop:sess-abc" {
		t.Errorf("callback data = %q, want %q", btn.CallbackData, "stop:sess-abc")
	}
}

func TestProgressReporter_Update_TextDelta(t *testing.T) {
	mb := newMockProgressBot()
	pr := NewProgressReporter(mb, 123, 7, "sess-abc")
	_ = pr.Start(context.Background())

	// Force throttle to pass by backdating lastEdit
	pr.lastEdit = time.Now().Add(-2 * time.Second)

	pr.Update(context.Background(), core.AgentEvent{
		Type: core.EventTextDelta,
		Text: "hello",
	})

	if pr.textLen != 5 {
		t.Errorf("textLen = %d, want 5", pr.textLen)
	}
	if len(mb.editedMessages) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(mb.editedMessages))
	}
	if mb.editedMessages[0].MessageID != 42 {
		t.Errorf("edited message ID = %d, want 42", mb.editedMessages[0].MessageID)
	}
}

func TestProgressReporter_Update_ToolUse(t *testing.T) {
	mb := newMockProgressBot()
	pr := NewProgressReporter(mb, 123, 7, "sess-abc")
	_ = pr.Start(context.Background())

	pr.lastEdit = time.Now().Add(-2 * time.Second)

	pr.Update(context.Background(), core.AgentEvent{
		Type:     core.EventToolUse,
		ToolName: "read_file",
	})

	if len(pr.tools) != 1 || pr.tools[0] != "read_file" {
		t.Errorf("tools = %v, want [read_file]", pr.tools)
	}
}

func TestProgressReporter_Update_Throttle_SuppressesRapidEdits(t *testing.T) {
	mb := newMockProgressBot()
	pr := NewProgressReporter(mb, 123, 7, "sess-abc")
	_ = pr.Start(context.Background())

	// lastEdit is set to now by Start, so both updates should be throttled
	pr.Update(context.Background(), core.AgentEvent{Type: core.EventTextDelta, Text: "a"})
	pr.Update(context.Background(), core.AgentEvent{Type: core.EventTextDelta, Text: "b"})

	if len(mb.editedMessages) != 0 {
		t.Errorf("expected 0 edits due to throttle, got %d", len(mb.editedMessages))
	}
	// Counters should still have been updated
	if pr.textLen != 2 {
		t.Errorf("textLen = %d, want 2", pr.textLen)
	}
}

func TestProgressReporter_Update_Throttle_AllowsAfterDelay(t *testing.T) {
	mb := newMockProgressBot()
	pr := NewProgressReporter(mb, 123, 7, "sess-abc")
	_ = pr.Start(context.Background())

	// Simulate enough time passing
	pr.lastEdit = time.Now().Add(-2 * time.Second)

	pr.Update(context.Background(), core.AgentEvent{Type: core.EventTextDelta, Text: "hello world"})

	if len(mb.editedMessages) != 1 {
		t.Errorf("expected 1 edit after throttle window, got %d", len(mb.editedMessages))
	}
}

func TestProgressReporter_Finish(t *testing.T) {
	mb := newMockProgressBot()
	pr := NewProgressReporter(mb, 123, 7, "sess-abc")
	_ = pr.Start(context.Background())

	pr.Finish(context.Background())

	if len(mb.deletedMessages) != 1 {
		t.Fatalf("expected 1 DeleteMessage call, got %d", len(mb.deletedMessages))
	}
	if mb.deletedMessages[0].MessageID != 42 {
		t.Errorf("deleted message ID = %d, want 42", mb.deletedMessages[0].MessageID)
	}
	if pr.statusMsg != nil {
		t.Error("statusMsg should be nil after Finish")
	}
}

func TestProgressReporter_Finish_NilStatusMsg(t *testing.T) {
	mb := newMockProgressBot()
	pr := NewProgressReporter(mb, 123, 7, "sess-abc")

	// Finish without Start - should not panic or call DeleteMessage
	pr.Finish(context.Background())

	if len(mb.deletedMessages) != 0 {
		t.Errorf("expected 0 DeleteMessage calls, got %d", len(mb.deletedMessages))
	}
}

func TestBuildStatusText_NoToolsNoText(t *testing.T) {
	pr := &ProgressReporter{}

	text := pr.buildStatusText()
	if text != "Working" {
		t.Errorf("buildStatusText() = %q, want %q", text, "Working")
	}
}

func TestBuildStatusText_FiveToolsShowsLastThree(t *testing.T) {
	pr := &ProgressReporter{
		tools: []string{"tool1", "tool2", "tool3", "tool4", "tool5"},
	}

	text := pr.buildStatusText()
	want := "Working\n\nTools: tool3 -> tool4 -> tool5"
	if text != want {
		t.Errorf("buildStatusText() = %q, want %q", text, want)
	}
}

func TestBuildStatusText_WithText(t *testing.T) {
	pr := &ProgressReporter{
		textLen: 1234,
	}

	text := pr.buildStatusText()
	want := "Working\n\nResponse: 1234 chars so far"
	if text != want {
		t.Errorf("buildStatusText() = %q, want %q", text, want)
	}
}

func TestBuildStatusText_WithToolsAndText(t *testing.T) {
	pr := &ProgressReporter{
		tools:   []string{"read", "write"},
		textLen: 500,
	}

	text := pr.buildStatusText()
	want := "Working\n\nTools: read -> write\n\nResponse: 500 chars so far"
	if text != want {
		t.Errorf("buildStatusText() = %q, want %q", text, want)
	}
}

func TestEditStatus_SwallowsNotModifiedError(t *testing.T) {
	mb := newMockProgressBot()
	mb.editErr = errors.New("Bad Request: message is not modified")

	pr := NewProgressReporter(mb, 123, 7, "sess-abc")
	_ = pr.Start(context.Background())

	// Should not panic or log an error for "not modified"
	pr.editStatus(context.Background(), "Working")

	// EditMessageText was called but returned error, so editedMessages is empty
	if len(mb.editedMessages) != 0 {
		t.Errorf("expected 0 recorded edits when error is returned, got %d", len(mb.editedMessages))
	}
}

func TestStopKeyboard(t *testing.T) {
	kb := stopKeyboard("my-session-123")

	if len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row, got %d", len(kb.InlineKeyboard))
	}
	if len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("expected 1 button, got %d", len(kb.InlineKeyboard[0]))
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.Text != "Stop" {
		t.Errorf("button text = %q, want %q", btn.Text, "Stop")
	}
	if btn.CallbackData != "stop:my-session-123" {
		t.Errorf("callback data = %q, want %q", btn.CallbackData, "stop:my-session-123")
	}
}
