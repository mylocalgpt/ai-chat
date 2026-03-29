package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// mockCallbackBot records calls made by the callback handler.
type mockCallbackBot struct {
	editedMessages  []*bot.EditMessageTextParams
	answeredQueries []*bot.AnswerCallbackQueryParams
	editErr         error // if set, EditMessageText returns this
}

func (b *mockCallbackBot) AnswerCallbackQuery(_ context.Context, params *bot.AnswerCallbackQueryParams) (bool, error) {
	b.answeredQueries = append(b.answeredQueries, params)
	return true, nil
}

func (b *mockCallbackBot) EditMessageText(_ context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	if b.editErr != nil {
		return nil, b.editErr
	}
	b.editedMessages = append(b.editedMessages, params)
	return &models.Message{ID: params.MessageID, Chat: models.Chat{ID: params.ChatID.(int64)}, Text: params.Text}, nil
}

func TestHandleStopCallback_Success(t *testing.T) {
	mb := &mockCallbackBot{}
	var calledWith string
	abortFn := func(_ context.Context, agentSessionID string) error {
		calledWith = agentSessionID
		return nil
	}

	h := newCallbackHandler(nil, map[int64]bool{}, abortFn, nil, "")
	h.handleStopCallback(context.Background(), mb, 123, 42, "ses_abc123")

	if calledWith != "ses_abc123" {
		t.Errorf("abortFunc called with %q, want %q", calledWith, "ses_abc123")
	}

	if len(mb.editedMessages) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(mb.editedMessages))
	}
	edit := mb.editedMessages[0]
	if edit.ChatID != int64(123) {
		t.Errorf("edit chat ID = %v, want 123", edit.ChatID)
	}
	if edit.MessageID != 42 {
		t.Errorf("edit message ID = %d, want 42", edit.MessageID)
	}
	if edit.Text != "Stopping..." {
		t.Errorf("edit text = %q, want %q", edit.Text, "Stopping...")
	}
}

func TestHandleStopCallback_NilAbortFunc(t *testing.T) {
	mb := &mockCallbackBot{}

	h := newCallbackHandler(nil, map[int64]bool{}, nil, nil, "")
	h.handleStopCallback(context.Background(), mb, 123, 42, "ses_abc123")

	if len(mb.editedMessages) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(mb.editedMessages))
	}
	if mb.editedMessages[0].Text != "Stop not available." {
		t.Errorf("edit text = %q, want %q", mb.editedMessages[0].Text, "Stop not available.")
	}
}

func TestHandleStopCallback_AbortError(t *testing.T) {
	mb := &mockCallbackBot{}
	abortFn := func(_ context.Context, _ string) error {
		return errors.New("connection refused")
	}

	h := newCallbackHandler(nil, map[int64]bool{}, abortFn, nil, "")
	h.handleStopCallback(context.Background(), mb, 123, 42, "ses_abc123")

	if len(mb.editedMessages) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(mb.editedMessages))
	}
	if mb.editedMessages[0].Text != "Failed to stop." {
		t.Errorf("edit text = %q, want %q", mb.editedMessages[0].Text, "Failed to stop.")
	}
}

func TestHandleCallback_StopRoutesCorrectly(t *testing.T) {
	mb := &mockCallbackBot{}
	var calledWith string
	abortFn := func(_ context.Context, agentSessionID string) error {
		calledWith = agentSessionID
		return nil
	}

	h := newCallbackHandler(nil, map[int64]bool{42: true}, abortFn, nil, "")

	update := &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "query123",
			Data: "stop:ses_xyz789",
			From: models.User{ID: 42},
			Message: models.MaybeInaccessibleMessage{
				Type: models.MaybeInaccessibleMessageTypeMessage,
				Message: &models.Message{
					ID:   10,
					Chat: models.Chat{ID: 555},
				},
			},
		},
	}

	h.handleCallback(context.Background(), mb, update)

	if calledWith != "ses_xyz789" {
		t.Errorf("abortFunc called with %q, want %q", calledWith, "ses_xyz789")
	}

	// Should have: 1 edit (Stopping...) + 1 answer callback query
	if len(mb.editedMessages) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(mb.editedMessages))
	}
	if mb.editedMessages[0].Text != "Stopping..." {
		t.Errorf("edit text = %q, want %q", mb.editedMessages[0].Text, "Stopping...")
	}
	if mb.editedMessages[0].ChatID != int64(555) {
		t.Errorf("edit chat ID = %v, want 555", mb.editedMessages[0].ChatID)
	}

	// AnswerCallbackQuery should be called (deferred in handleCallback)
	if len(mb.answeredQueries) != 1 {
		t.Fatalf("expected 1 AnswerCallbackQuery, got %d", len(mb.answeredQueries))
	}
}

func TestHandleCallback_StopUnauthorized(t *testing.T) {
	mb := &mockCallbackBot{}
	abortCalled := false
	abortFn := func(_ context.Context, _ string) error {
		abortCalled = true
		return nil
	}

	// User 99 is NOT in allowed list
	h := newCallbackHandler(nil, map[int64]bool{42: true}, abortFn, nil, "")

	update := &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "query123",
			Data: "stop:ses_xyz789",
			From: models.User{ID: 99},
			Message: models.MaybeInaccessibleMessage{
				Type: models.MaybeInaccessibleMessageTypeMessage,
				Message: &models.Message{
					ID:   10,
					Chat: models.Chat{ID: 555},
				},
			},
		},
	}

	h.handleCallback(context.Background(), mb, update)

	if abortCalled {
		t.Error("abortFunc should not be called for unauthorized users")
	}

	// Should get "Not authorized." answer callback
	if len(mb.answeredQueries) != 1 {
		t.Fatalf("expected 1 AnswerCallbackQuery, got %d", len(mb.answeredQueries))
	}
	if mb.answeredQueries[0].Text != "Not authorized." {
		t.Errorf("answer text = %q, want %q", mb.answeredQueries[0].Text, "Not authorized.")
	}

	// No edits should have happened
	if len(mb.editedMessages) != 0 {
		t.Errorf("expected 0 edits for unauthorized user, got %d", len(mb.editedMessages))
	}
}

func TestSetAbortFunc(t *testing.T) {
	adapter := &TelegramAdapter{
		callbackHandler: newCallbackHandler(nil, map[int64]bool{}, nil, nil, ""),
	}

	if adapter.callbackHandler.abortFunc != nil {
		t.Fatal("abortFunc should be nil initially")
	}

	called := false
	adapter.SetAbortFunc(func(_ context.Context, _ string) error {
		called = true
		return nil
	})

	if adapter.callbackHandler.abortFunc == nil {
		t.Fatal("abortFunc should be set after SetAbortFunc")
	}

	_ = adapter.callbackHandler.abortFunc(context.Background(), "test")
	if !called {
		t.Error("abortFunc was not called")
	}
}
