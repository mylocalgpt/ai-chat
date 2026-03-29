package telegram

import (
	"errors"
	"testing"
)

func TestHandleBotError_conflict_triggers_shutdown(t *testing.T) {
	var shutdownCalled bool
	adapter := &TelegramAdapter{
		shutdownFunc: func() { shutdownCalled = true },
	}

	adapter.handleBotError(errors.New("conflict: terminated by other getUpdates request; make sure that only one bot instance is running"))

	if !shutdownCalled {
		t.Fatal("expected shutdownFunc to be called on polling conflict error")
	}
}

func TestHandleBotError_unrelated_error_does_not_trigger_shutdown(t *testing.T) {
	var shutdownCalled bool
	adapter := &TelegramAdapter{
		shutdownFunc: func() { shutdownCalled = true },
	}

	adapter.handleBotError(errors.New("network timeout"))

	if shutdownCalled {
		t.Fatal("shutdownFunc should not be called for unrelated errors")
	}
}
