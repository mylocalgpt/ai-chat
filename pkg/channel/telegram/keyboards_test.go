package telegram

import (
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestWorkspaceKeyboard(t *testing.T) {
	tests := []struct {
		name         string
		workspaces   []core.Workspace
		limit        int
		wantButtons  int
		wantLastData string
	}{
		{
			name:         "empty workspaces",
			workspaces:   nil,
			limit:        5,
			wantButtons:  1,
			wantLastData: "ws:none",
		},
		{
			name: "single workspace",
			workspaces: []core.Workspace{
				{Name: "lab"},
			},
			limit:        5,
			wantButtons:  2,
			wantLastData: "ws:search",
		},
		{
			name: "multiple workspaces",
			workspaces: []core.Workspace{
				{Name: "lab"},
				{Name: "ai-chat"},
				{Name: "docs"},
			},
			limit:        5,
			wantButtons:  4,
			wantLastData: "ws:search",
		},
		{
			name: "limit applied",
			workspaces: []core.Workspace{
				{Name: "ws1"},
				{Name: "ws2"},
				{Name: "ws3"},
				{Name: "ws4"},
				{Name: "ws5"},
				{Name: "ws6"},
			},
			limit:        3,
			wantButtons:  4,
			wantLastData: "ws:search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kb := WorkspaceKeyboard(tt.workspaces, tt.limit)
			if kb == nil {
				t.Fatal("keyboard is nil")
			}

			if len(kb.InlineKeyboard) != tt.wantButtons {
				t.Errorf("got %d buttons, want %d", len(kb.InlineKeyboard), tt.wantButtons)
			}

			if len(kb.InlineKeyboard) > 0 {
				lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
				if len(lastRow) > 0 && lastRow[0].CallbackData != tt.wantLastData {
					t.Errorf("last button data: got %q, want %q", lastRow[0].CallbackData, tt.wantLastData)
				}
			}
		})
	}
}

func TestWorkspaceKeyboardCallbackData(t *testing.T) {
	workspaces := []core.Workspace{
		{Name: "lab"},
		{Name: "my workspace"},
		{Name: "test/slash"},
	}

	kb := WorkspaceKeyboard(workspaces, 5)

	if kb.InlineKeyboard[0][0].CallbackData != "ws:lab" {
		t.Errorf("first workspace callback: got %q, want %q", kb.InlineKeyboard[0][0].CallbackData, "ws:lab")
	}

	if kb.InlineKeyboard[1][0].CallbackData != "ws:my+workspace" {
		t.Errorf("space in name: got %q, want %q", kb.InlineKeyboard[1][0].CallbackData, "ws:my+workspace")
	}

	if kb.InlineKeyboard[2][0].CallbackData != "ws:test%2Fslash" {
		t.Errorf("slash in name: got %q, want %q", kb.InlineKeyboard[2][0].CallbackData, "ws:test%2Fslash")
	}
}

func TestSessionKeyboard(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []SessionPreview
		wantButtons int
	}{
		{
			name:        "empty sessions",
			sessions:    nil,
			wantButtons: 2,
		},
		{
			name: "single session",
			sessions: []SessionPreview{
				{Name: "ai-chat-lab-a3f2", FirstUserMsg: "hello", LastAgentMsg: "hi"},
			},
			wantButtons: 3,
		},
		{
			name: "multiple sessions",
			sessions: []SessionPreview{
				{Name: "ai-chat-lab-a3f2", FirstUserMsg: "hello", LastAgentMsg: "hi"},
				{Name: "ai-chat-lab-k7x1", FirstUserMsg: "test", LastAgentMsg: "ok"},
			},
			wantButtons: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kb := SessionKeyboard(tt.sessions)
			if kb == nil {
				t.Fatal("keyboard is nil")
			}

			if len(kb.InlineKeyboard) != tt.wantButtons {
				t.Errorf("got %d buttons, want %d", len(kb.InlineKeyboard), tt.wantButtons)
			}

			if len(kb.InlineKeyboard) > 0 {
				firstRow := kb.InlineKeyboard[0]
				if len(firstRow) > 0 && firstRow[0].CallbackData != "sess:new" {
					t.Errorf("first button should be 'new session', got %q", firstRow[0].CallbackData)
				}

				lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
				if len(lastRow) > 0 && lastRow[0].CallbackData != "sess:back" {
					t.Errorf("last button should be 'back', got %q", lastRow[0].CallbackData)
				}
			}
		})
	}
}

func TestSessionKeyboardPreviewTruncation(t *testing.T) {
	sessions := []SessionPreview{
		{
			Name:         "ai-chat-lab-a3f2",
			FirstUserMsg: "this is a very long user message that should be truncated",
			LastAgentMsg: "this is a very long agent response that should also be truncated",
			Status:       "active",
			Age:          "2h",
		},
	}

	kb := SessionKeyboard(sessions)

	buttonText := kb.InlineKeyboard[1][0].Text
	if len(buttonText) > 64 {
		t.Errorf("button text too long: %d chars, max 64", len(buttonText))
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
	if row[0].CallbackData != "sec:yes:test-uuid-123" {
		t.Errorf("first button data: got %q, want %q", row[0].CallbackData, "sec:yes:test-uuid-123")
	}

	if row[1].Text != "Cancel" {
		t.Errorf("second button text: got %q, want %q", row[1].Text, "Cancel")
	}
	if row[1].CallbackData != "sec:no:test-uuid-123" {
		t.Errorf("second button data: got %q, want %q", row[1].CallbackData, "sec:no:test-uuid-123")
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
