package telegram

import (
	"testing"
	"time"

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

	if len(kb.InlineKeyboard) != 4 {
		t.Errorf("expected 4 rows (3 workspaces + search), got %d", len(kb.InlineKeyboard))
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
		expectedData := "ws:" + ws.Name
		if row[0].CallbackData != expectedData {
			t.Errorf("row %d: callback = %q, want %q", i, row[0].CallbackData, expectedData)
		}
	}

	lastRow := kb.InlineKeyboard[3]
	if lastRow[0].CallbackData != "ws:search" {
		t.Errorf("last row callback = %q, want ws:search", lastRow[0].CallbackData)
	}
}

func TestSessionKeyboardIntegration(t *testing.T) {
	sessions := []SessionPreview{
		{Name: "ai-chat-lab-a3f2", FirstUserMsg: "fix the bug", LastAgentMsg: "done", Status: "active", Age: "2h"},
		{Name: "ai-chat-lab-k7x1", FirstUserMsg: "add feature", LastAgentMsg: "implemented", Status: "idle", Age: "1d"},
	}

	kb := SessionKeyboard(sessions)

	if len(kb.InlineKeyboard) != 4 {
		t.Errorf("expected 4 rows (new + 2 sessions + back), got %d", len(kb.InlineKeyboard))
	}

	if kb.InlineKeyboard[0][0].CallbackData != "sess:new" {
		t.Errorf("first row should be 'new session'")
	}

	if kb.InlineKeyboard[3][0].CallbackData != "sess:back" {
		t.Errorf("last row should be 'back'")
	}

	for i, sess := range sessions {
		row := kb.InlineKeyboard[i+1]
		if len(row) != 1 {
			continue
		}
		expectedData := "sess:" + sess.Name
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
	if row[0].CallbackData != "sec:yes:"+msgRef {
		t.Errorf("first button callback = %q, want 'sec:yes:%s'", row[0].CallbackData, msgRef)
	}

	if row[1].Text != "Cancel" {
		t.Errorf("second button text = %q, want 'Cancel'", row[1].Text)
	}
	if row[1].CallbackData != "sec:no:"+msgRef {
		t.Errorf("second button callback = %q, want 'sec:no:%s'", row[1].CallbackData, msgRef)
	}
}

func TestCallbackHandlerPendingMessages(t *testing.T) {
	h := newCallbackHandler(nil)

	msgRef := "test-ref-123"
	msg := core.InboundMessage{
		ID:      "1",
		Channel: "telegram",
		Content: "test message with password",
	}

	h.addPendingMessage(msgRef, msg, nil)

	pending, ok := h.getPendingMessage(msgRef)
	if !ok {
		t.Fatal("pending message not found")
	}
	if pending.Msg.Content != msg.Content {
		t.Errorf("pending message content = %q, want %q", pending.Msg.Content, msg.Content)
	}

	h.removePendingMessage(msgRef)

	_, ok = h.getPendingMessage(msgRef)
	if ok {
		t.Error("pending message should be removed")
	}
}

func TestCallbackHandlerExpiry(t *testing.T) {
	h := newCallbackHandler(nil)

	msgRef := "test-expired"
	msg := core.InboundMessage{Content: "test"}

	h.addPendingMessage(msgRef, msg, nil)

	h.pendingMutex.Lock()
	if p, ok := h.pendingMessages[msgRef]; ok {
		p.ExpiresAt = p.ExpiresAt.Add(-10 * time.Minute)
		h.pendingMessages[msgRef] = p
	}
	h.pendingMutex.Unlock()

	_, ok := h.getPendingMessage(msgRef)
	if ok {
		t.Error("expired message should not be returned")
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
