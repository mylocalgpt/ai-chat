package telegram

import (
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/router"
)

func TestWorkspacePickerKeyboard(t *testing.T) {
	kb := WorkspacePickerKeyboard(&router.WorkspacePickerData{
		Workspaces:        []router.WorkspaceOption{{ID: 1, Name: "lab"}, {ID: 2, Name: "docs"}},
		ActiveWorkspaceID: 2,
	})
	if kb == nil || len(kb.InlineKeyboard) != 2 {
		t.Fatalf("unexpected keyboard: %+v", kb)
	}
	if kb.InlineKeyboard[0][0].CallbackData != "ws:1" {
		t.Fatalf("unexpected callback: %q", kb.InlineKeyboard[0][0].CallbackData)
	}
	if kb.InlineKeyboard[1][0].Text != "-> docs" {
		t.Fatalf("unexpected active label: %q", kb.InlineKeyboard[1][0].Text)
	}
}

func TestSessionPickerKeyboard(t *testing.T) {
	kb := SessionPickerKeyboard(&router.SessionPickerData{
		WorkspaceID:     7,
		ActiveSessionID: 22,
		Sessions:        []router.SessionOption{{ID: 11, Slug: "a1b2", Agent: "opencode", Status: "active"}, {ID: 22, Slug: "c3d4", Agent: "copilot", Status: "idle"}},
	})
	if kb == nil || len(kb.InlineKeyboard) != 2 {
		t.Fatalf("unexpected keyboard: %+v", kb)
	}
	if kb.InlineKeyboard[0][0].CallbackData != "sess:7:11" {
		t.Fatalf("unexpected callback: %q", kb.InlineKeyboard[0][0].CallbackData)
	}
	if kb.InlineKeyboard[1][0].Text[:2] != "->" {
		t.Fatalf("expected active session label, got %q", kb.InlineKeyboard[1][0].Text)
	}
}

func TestSecurityWarningKeyboard(t *testing.T) {
	msgRef := "test-uuid-123"

	kb := SecurityWarningKeyboard(msgRef)
	if kb == nil {
		t.Fatal("keyboard is nil")
	}

	if len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row, got %d", len(kb.InlineKeyboard))
	}

	row := kb.InlineKeyboard[0]
	if len(row) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(row))
	}

	if row[0].Text != "Yes, send" {
		t.Errorf("first button text: got %q, want %q", row[0].Text, "Yes, send")
	}
	if row[0].CallbackData != "sec:approve:test-uuid-123" {
		t.Errorf("first button data: got %q, want %q", row[0].CallbackData, "sec:approve:test-uuid-123")
	}

	if row[1].Text != "Cancel" {
		t.Errorf("second button text: got %q, want %q", row[1].Text, "Cancel")
	}
	if row[1].CallbackData != "sec:reject:test-uuid-123" {
		t.Errorf("second button data: got %q, want %q", row[1].CallbackData, "sec:reject:test-uuid-123")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is longer than ten", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d): got %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name    string
		timeAgo time.Duration
	}{
		{"just now", 30 * time.Second},
		{"minutes", 5 * time.Minute},
		{"hours", 2 * time.Hour},
		{"days", 48 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pastTime := time.Now().Add(-tt.timeAgo)
			result := formatAge(pastTime)
			if result == "" {
				t.Error("expected non-empty age string")
			}
		})
	}
}
