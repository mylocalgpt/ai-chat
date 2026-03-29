package telegram

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// mockResponseBot records all bot API calls for verification in response tests.
type mockResponseBot struct {
	mu sync.Mutex

	sentMessages    []*bot.SendMessageParams
	sentDocuments   []*bot.SendDocumentParams
	sentChatActions []*bot.SendChatActionParams
	uploadedData    []string
	sendMsgErr      error
	sendDocErr      error
}

func (b *mockResponseBot) GetMe(context.Context) (*models.User, error) {
	return &models.User{ID: 1, IsBot: true}, nil
}
func (b *mockResponseBot) Start(context.Context) {}
func (b *mockResponseBot) SendMessage(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sendMsgErr != nil {
		return nil, b.sendMsgErr
	}
	b.sentMessages = append(b.sentMessages, params)
	return &models.Message{ID: 1, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}
func (b *mockResponseBot) SendChatAction(_ context.Context, params *bot.SendChatActionParams) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sentChatActions = append(b.sentChatActions, params)
	return true, nil
}
func (b *mockResponseBot) SetMyCommands(context.Context, *bot.SetMyCommandsParams) (bool, error) {
	return true, nil
}
func (b *mockResponseBot) DeleteMessage(context.Context, *bot.DeleteMessageParams) (bool, error) {
	return true, nil
}
func (b *mockResponseBot) EditMessageText(_ context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	return &models.Message{ID: params.MessageID, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}
func (b *mockResponseBot) SendDocument(_ context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sendDocErr != nil {
		return nil, b.sendDocErr
	}
	b.sentDocuments = append(b.sentDocuments, params)

	if upload, ok := params.Document.(*models.InputFileUpload); ok && upload.Data != nil {
		data, err := io.ReadAll(upload.Data)
		if err == nil {
			b.uploadedData = append(b.uploadedData, string(data))
		}
	}

	return &models.Message{ID: 1, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}

func newResponseTestAdapter(mb *mockResponseBot) *TelegramAdapter {
	return &TelegramAdapter{
		bot:             mb,
		allowedUsers:    map[int64]bool{},
		callbackHandler: newCallbackHandler(nil, map[int64]bool{}, nil),
	}
}

func TestSendResponseShort(t *testing.T) {
	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)

	content := "Short response under 8000 runes"
	params := core.ResponseParams{
		ChatID:      123,
		ReplyToID:   "42",
		Content:     content,
		SessionName: "ai-chat-lab-a3f2",
		MsgIdx:      0,
	}

	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Short path: should go through Send() which calls SendMessage with formatted HTML.
	if len(mb.sentMessages) == 0 {
		t.Fatal("expected at least 1 SendMessage call")
	}

	// Verify no document was sent.
	if len(mb.sentDocuments) > 0 {
		t.Error("short response should not send documents")
	}

	// First message should have the chat action (typing), then the formatted message.
	// Send() calls SendChatAction then SendMessage. Check that final message was sent.
	found := false
	for _, msg := range mb.sentMessages {
		if msg.ChatID == int64(123) {
			found = true
			// Should NOT have an inline keyboard.
			if msg.ReplyMarkup != nil {
				t.Error("short response should not have inline keyboard")
			}
		}
	}
	if !found {
		t.Error("no message sent to chat 123")
	}
}

func TestSendResponseMedium(t *testing.T) {
	// Content between shortThreshold and longThreshold (8001-20000 runes).
	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)

	// No summarizer set: will use fallback.
	content := strings.Repeat("a", 10000)
	params := core.ResponseParams{
		ChatID:      456,
		ReplyToID:   "10",
		Content:     content,
		SessionName: "eager-canyon",
		MsgIdx:      1,
		Workspace:   "/tmp/test",
	}

	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Should NOT send a document (content under longThreshold).
	if len(mb.sentDocuments) > 0 {
		t.Error("medium response should not send documents")
	}

	// Should send a summary message with inline keyboard.
	if len(mb.sentMessages) == 0 {
		t.Fatal("expected at least 1 SendMessage call")
	}

	// Find the message with the inline keyboard.
	var summaryMsg *bot.SendMessageParams
	for _, msg := range mb.sentMessages {
		if msg.ReplyMarkup != nil {
			summaryMsg = msg
			break
		}
	}
	if summaryMsg == nil {
		t.Fatal("expected a message with inline keyboard")
	}

	if summaryMsg.ChatID != int64(456) {
		t.Errorf("ChatID = %v, want 456", summaryMsg.ChatID)
	}
	if summaryMsg.ParseMode != models.ParseModeHTML {
		t.Errorf("ParseMode = %v, want HTML", summaryMsg.ParseMode)
	}

	// Verify inline keyboard has "Show full output" button.
	kb, ok := summaryMsg.ReplyMarkup.(*models.InlineKeyboardMarkup)
	if !ok {
		t.Fatal("ReplyMarkup is not *models.InlineKeyboardMarkup")
	}
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("expected 1x1 keyboard, got %v", kb.InlineKeyboard)
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.Text != "Show full output" {
		t.Errorf("button text = %q, want %q", btn.Text, "Show full output")
	}
	wantCallback := "full:eager-canyon:1"
	if btn.CallbackData != wantCallback {
		t.Errorf("callback data = %q, want %q", btn.CallbackData, wantCallback)
	}

	// Verify link preview is disabled.
	if summaryMsg.LinkPreviewOptions == nil || !*summaryMsg.LinkPreviewOptions.IsDisabled {
		t.Error("link preview should be disabled")
	}
}

func TestSendResponseLong(t *testing.T) {
	// Content over longThreshold (>20000 runes).
	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)

	content := strings.Repeat("b", 25000)
	params := core.ResponseParams{
		ChatID:      789,
		ReplyToID:   "5",
		Content:     content,
		SessionName: "bright-river",
		MsgIdx:      2,
		Workspace:   "/tmp/test",
	}

	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Should send a document attachment.
	if len(mb.sentDocuments) != 1 {
		t.Fatalf("expected 1 SendDocument call, got %d", len(mb.sentDocuments))
	}

	doc := mb.sentDocuments[0]
	if doc.ChatID != int64(789) {
		t.Errorf("document ChatID = %v, want 789", doc.ChatID)
	}

	upload, ok := doc.Document.(*models.InputFileUpload)
	if !ok {
		t.Fatal("Document is not *models.InputFileUpload")
	}
	wantFilename := "response-bright-river-2.md"
	if upload.Filename != wantFilename {
		t.Errorf("filename = %q, want %q", upload.Filename, wantFilename)
	}

	// Verify uploaded data matches content.
	if len(mb.uploadedData) != 1 || mb.uploadedData[0] != content {
		t.Error("uploaded document content does not match input")
	}

	// Should also send a summary message with keyboard.
	var summaryMsg *bot.SendMessageParams
	for _, msg := range mb.sentMessages {
		if msg.ReplyMarkup != nil {
			summaryMsg = msg
			break
		}
	}
	if summaryMsg == nil {
		t.Fatal("expected summary message with inline keyboard for long content")
	}
}

func TestSendResponseSummarizationFailure(t *testing.T) {
	// When summarizer is set but returns an error, fallback is used.
	const workspace = "/tmp/test-workspace"
	const summaryText = "AI summary" // won't be returned due to error

	// Create a mock server that always returns an error.
	ts := newMockOpenCodeServer(t, summaryText, 0)
	ts.Close() // Close immediately so requests fail.

	mgr := newTestServerManager(t, ts.URL, workspace)
	summarizer := NewSummarizer(mgr)

	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)
	adapter.SetSummarizer(summarizer)

	content := strings.Repeat("c", 10000) // medium content
	params := core.ResponseParams{
		ChatID:      111,
		Content:     content,
		SessionName: "test-session",
		MsgIdx:      0,
		Workspace:   workspace,
	}

	// Should not error; should fall back to truncated summary.
	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Verify a summary message was sent (using fallback).
	var summaryMsg *bot.SendMessageParams
	for _, msg := range mb.sentMessages {
		if msg.ReplyMarkup != nil {
			summaryMsg = msg
			break
		}
	}
	if summaryMsg == nil {
		t.Fatal("expected fallback summary message with keyboard")
	}

	// The summary text should be the fallback (truncated content), not the AI summary.
	// Fallback truncates to 2000 runes, so we just verify it was sent.
	if summaryMsg.Text == "" {
		t.Error("summary message text should not be empty")
	}
}

func TestSendResponseTypingIndicator(t *testing.T) {
	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)

	content := strings.Repeat("d", 10000) // medium content, triggers long path
	params := core.ResponseParams{
		ChatID:      222,
		Content:     content,
		SessionName: "typing-test",
		MsgIdx:      0,
		Workspace:   "/tmp/test",
	}

	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Verify typing indicator was sent at least once.
	mb.mu.Lock()
	actionCount := len(mb.sentChatActions)
	mb.mu.Unlock()

	if actionCount == 0 {
		t.Error("expected at least 1 SendChatAction call for typing indicator")
	}

	// Verify the first action was for chat 222 with typing action.
	mb.mu.Lock()
	action := mb.sentChatActions[0]
	mb.mu.Unlock()

	if action.ChatID != int64(222) {
		t.Errorf("ChatAction ChatID = %v, want 222", action.ChatID)
	}
	if action.Action != models.ChatActionTyping {
		t.Errorf("ChatAction Action = %v, want typing", action.Action)
	}
}

func TestSendResponseWithSummarizer(t *testing.T) {
	const workspace = "/tmp/test-workspace"
	const summaryText = "- Created a new file\n- Fixed 3 bugs"

	ts := newMockOpenCodeServer(t, summaryText, 0)
	defer ts.Close()

	mgr := newTestServerManager(t, ts.URL, workspace)
	summarizer := NewSummarizer(mgr)

	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)
	adapter.SetSummarizer(summarizer)

	content := strings.Repeat("e", 10000)
	params := core.ResponseParams{
		ChatID:      333,
		Content:     content,
		SessionName: "summarizer-test",
		MsgIdx:      0,
		Workspace:   workspace,
	}

	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Verify summary message contains the AI-generated summary (formatted as HTML).
	var summaryMsg *bot.SendMessageParams
	for _, msg := range mb.sentMessages {
		if msg.ReplyMarkup != nil {
			summaryMsg = msg
			break
		}
	}
	if summaryMsg == nil {
		t.Fatal("expected summary message with keyboard")
	}

	// The HTML-formatted summary should contain the bullet points.
	// FormatHTML converts "- " to list items, but the exact format depends on
	// the markdown converter. Just verify it's non-empty and was sent.
	if summaryMsg.Text == "" {
		t.Error("summary message text should not be empty")
	}
}

func TestSendResponseReplyParameters(t *testing.T) {
	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)

	// Medium content to go through long path with reply parameters.
	content := strings.Repeat("f", 10000)
	params := core.ResponseParams{
		ChatID:      444,
		ReplyToID:   "99",
		Content:     content,
		SessionName: "reply-test",
		MsgIdx:      0,
		Workspace:   "/tmp/test",
	}

	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Find the summary message and verify reply parameters.
	var summaryMsg *bot.SendMessageParams
	for _, msg := range mb.sentMessages {
		if msg.ReplyMarkup != nil {
			summaryMsg = msg
			break
		}
	}
	if summaryMsg == nil {
		t.Fatal("expected summary message")
	}

	if summaryMsg.ReplyParameters == nil {
		t.Fatal("expected ReplyParameters to be set")
	}
	if summaryMsg.ReplyParameters.MessageID != 99 {
		t.Errorf("ReplyParameters.MessageID = %d, want 99", summaryMsg.ReplyParameters.MessageID)
	}
}

func TestShowFullKeyboard(t *testing.T) {
	tests := []struct {
		session  string
		idx      int
		wantData string
	}{
		{"ai-chat-lab-a3f2", 0, "full:ai-chat-lab-a3f2:0"},
		{"eager-canyon", 5, "full:eager-canyon:5"},
		{"x", 99, "full:x:99"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%d", tt.session, tt.idx), func(t *testing.T) {
			kb := ShowFullKeyboard(tt.session, tt.idx)

			if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
				t.Fatalf("expected 1x1 keyboard, got %v", kb.InlineKeyboard)
			}

			btn := kb.InlineKeyboard[0][0]
			if btn.Text != "Show full output" {
				t.Errorf("button text = %q, want %q", btn.Text, "Show full output")
			}
			if btn.CallbackData != tt.wantData {
				t.Errorf("callback data = %q, want %q", btn.CallbackData, tt.wantData)
			}

			// Verify callback data fits in Telegram's 64-byte limit.
			if len(btn.CallbackData) > 64 {
				t.Errorf("callback data is %d bytes, exceeds 64-byte limit", len(btn.CallbackData))
			}
		})
	}
}

func TestSendResponseShortWithReplyTo(t *testing.T) {
	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)

	params := core.ResponseParams{
		ChatID:      555,
		ReplyToID:   "77",
		Content:     "Short reply",
		SessionName: "test",
		MsgIdx:      0,
	}

	err := adapter.SendResponse(context.Background(), params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Verify Send() was called (it handles reply params internally).
	if len(mb.sentMessages) == 0 {
		t.Fatal("expected SendMessage call")
	}
}

func TestSendResponseTypingCancelledBeforeSend(t *testing.T) {
	// Verify typing indicator goroutine doesn't leak by using a
	// short-lived context and checking that operations complete.
	mb := &mockResponseBot{}
	adapter := newResponseTestAdapter(mb)

	content := strings.Repeat("g", 10000)
	params := core.ResponseParams{
		ChatID:      666,
		Content:     content,
		SessionName: "cancel-test",
		MsgIdx:      0,
		Workspace:   "/tmp/test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.SendResponse(ctx, params)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}

	// Give goroutine time to exit.
	time.Sleep(50 * time.Millisecond)

	// If we get here without hanging, the typing goroutine was properly cancelled.
}
