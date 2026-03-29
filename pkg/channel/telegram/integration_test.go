package telegram

import (
	"fmt"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestFormatHTMLIntegration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:  "full agent response",
			input: "## Summary\n\nI made the following changes:\n\n- **Fixed** the bug in `processData()`\n- Added error handling\n\n```go\nfunc processData(input string) error {\n    if input == \"\" {\n        return errors.New(\"empty\")\n    }\n    return nil\n}\n```\n\nSee [docs](https://example.com) for more.",
			contains: []string{
				"<b>Summary</b>",
				"<b>Fixed</b>",
				"<code>processData()</code>",
				"<pre><code>",
				"<a href=\"https://example.com\">docs</a>",
			},
		},
		{
			name:  "nested formatting",
			input: "**Bold with *italic* and `code` inside**",
			contains: []string{
				"<b>Bold with <i>italic</i> and <code>code</code> inside</b>",
			},
		},
		{
			name:  "multiple code blocks",
			input: "First:\n```go\ncode1\n```\n\nSecond:\n```python\ncode2\n```",
			contains: []string{
				"<pre><code>code1\n</code></pre>",
				"<pre><code>code2\n</code></pre>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatHTML(tt.input)
			for _, want := range tt.contains {
				if !contains(result, want) {
					t.Errorf("result missing %q", want)
				}
			}
		})
	}
}

func TestSplitMessageIntegration(t *testing.T) {
	codeContent := repeatChar('x', 5000)
	codeBlock := "<pre><code>" + codeContent + "</code></pre>"
	input := "Before code:\n\n" + codeBlock + "\n\nAfter code."

	chunks := SplitMessage(input, FormattedMaxLen)

	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if len(chunk) > FormattedMaxLen {
			t.Errorf("chunk %d exceeds max: %d > %d", i, len(chunk), FormattedMaxLen)
		}
	}

	combined := ""
	for _, chunk := range chunks {
		combined += chunk
	}
	combined = replaceAll(combined, "</code></pre><pre><code>", "")

	if !contains(combined, repeatChar('x', 100)) {
		t.Error("code content was lost in splitting")
	}
}

func TestWorkspaceKeyboardIntegration(t *testing.T) {
	workspaces := []core.Workspace{
		{ID: 1, Name: "lab", Path: "/path/to/lab"},
		{ID: 2, Name: "ai-chat", Path: "/path/to/ai-chat"},
		{ID: 3, Name: "docs", Path: "/path/to/docs"},
	}

	kb := WorkspaceKeyboard(workspaces, 0)

	if len(kb.InlineKeyboard) != 3 {
		t.Errorf("expected 3 rows, got %d", len(kb.InlineKeyboard))
	}

	for i, ws := range workspaces {
		row := kb.InlineKeyboard[i]
		if len(row) != 1 {
			t.Errorf("row %d: expected 1 button, got %d", i, len(row))
			continue
		}
		if row[0].Text != ws.Name {
			t.Errorf("row %d: text = %q, want %q", i, row[0].Text, ws.Name)
		}
		expectedData := fmt.Sprintf("ws:%d", ws.ID)
		if row[0].CallbackData != expectedData {
			t.Errorf("row %d: callback = %q, want %q", i, row[0].CallbackData, expectedData)
		}
	}
}

func TestSessionKeyboardIntegration(t *testing.T) {
	sessions := []SessionPreview{
		{Name: "ai-chat-lab-a3f2", FirstUserMsg: "fix the bug", LastAgentMsg: "done", Status: "active", Age: "2h"},
		{Name: "ai-chat-lab-k7x1", FirstUserMsg: "add feature", LastAgentMsg: "implemented", Status: "idle", Age: "1d"},
	}

	kb := SessionKeyboard(sessions)

	if len(kb.InlineKeyboard) != 2 {
		t.Errorf("expected 2 rows, got %d", len(kb.InlineKeyboard))
	}

	for i, sess := range sessions {
		row := kb.InlineKeyboard[i]
		if len(row) != 1 {
			continue
		}
		expectedData := sess.Name
		if row[0].CallbackData != expectedData {
			t.Errorf("session %d: callback = %q, want %q", i, row[0].CallbackData, expectedData)
		}
	}
}

func TestSecurityWarningKeyboardIntegration(t *testing.T) {
	msgRef := "abc123-def456-ghi789"

	kb := SecurityWarningKeyboard(msgRef)

	if len(kb.InlineKeyboard) != 1 {
		t.Errorf("expected 1 row, got %d", len(kb.InlineKeyboard))
	}

	row := kb.InlineKeyboard[0]
	if len(row) != 2 {
		t.Errorf("expected 2 buttons, got %d", len(row))
	}

	if row[0].Text != "Yes, send" {
		t.Errorf("first button text = %q, want 'Yes, send'", row[0].Text)
	}
	if row[0].CallbackData != "sec:approve:"+msgRef {
		t.Errorf("first button callback = %q, want 'sec:approve:%s'", row[0].CallbackData, msgRef)
	}

	if row[1].Text != "Cancel" {
		t.Errorf("second button text = %q, want 'Cancel'", row[1].Text)
	}
	if row[1].CallbackData != "sec:reject:"+msgRef {
		t.Errorf("second button callback = %q, want 'sec:reject:%s'", row[1].CallbackData, msgRef)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func repeatChar(c byte, n int) string {
	result := make([]byte, n)
	for i := range result {
		result[i] = c
	}
	return string(result)
}

func replaceAll(s, old, new string) string {
	result := ""
	for {
		idx := indexOf(s, old)
		if idx == -1 {
			result += s
			break
		}
		result += s[:idx] + new
		s = s[idx+len(old):]
	}
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
