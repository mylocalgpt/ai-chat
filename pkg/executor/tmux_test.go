package executor

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "\x1b[1mhello\x1b[0m", "hello"},
		{"color", "\x1b[31mred\x1b[0m", "red"},
		{"reset", "\x1b[0m", ""},
		{"cursor movement", "\x1b[2Jcleared", "cleared"},
		{"osc title", "\x1b]0;my title\x07visible", "visible"},
		{"mixed content", "\x1b[1m\x1b[31mbold red\x1b[0m plain", "bold red plain"},
		{"already clean", "just text", "just text"},
		{"empty string", "", ""},
		{"multi-param csi", "\x1b[38;5;196mcolor256\x1b[0m", "color256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.in)
			if got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizePart(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"normal", "myproject", "myproject"},
		{"spaces", "my project", "my-project"},
		{"dots", "my.project", "my-project"},
		{"special chars", "my@project!", "my-project"},
		{"uppercase", "MyProject", "myproject"},
		{"hyphens preserved", "my-project", "my-project"},
		{"leading trailing special", ".project.", "project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizePart(tt.in)
			if got != tt.want {
				t.Errorf("sanitizePart(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTmuxIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Check that tmux is available.
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found in PATH")
	}

	tmx := NewTmux()
	session := "ai-chat-test-integration"

	// Clean up any leftover session from a previous failed run.
	_ = tmx.KillSession(session)

	// Create session.
	if err := tmx.NewSession(session, "/tmp"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tmx.KillSession(session) }()

	if !tmx.HasSession(session) {
		t.Fatal("HasSession returned false after NewSession")
	}

	// Send a command and capture output.
	if err := tmx.SendKeys(session, "echo hello-tmux-test"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}

	// Give the shell time to execute.
	time.Sleep(500 * time.Millisecond)

	out, err := tmx.CapturePaneRaw(session, 200)
	if err != nil {
		t.Fatalf("CapturePaneRaw: %v", err)
	}

	if !strings.Contains(out, "hello-tmux-test") {
		t.Errorf("captured output does not contain expected text:\n%s", out)
	}

	// ListSessions should include our session.
	sessions, err := tmx.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if !slices.Contains(sessions, session) {
		t.Errorf("ListSessions did not include %q: %v", session, sessions)
	}

	// Kill session.
	if err := tmx.KillSession(session); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if tmx.HasSession(session) {
		t.Error("HasSession returned true after KillSession")
	}
}
