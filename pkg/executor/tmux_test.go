package executor

import (
	"os/exec"
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

func TestSessionName(t *testing.T) {
	tests := []struct {
		name      string
		workspace string
		agent     string
		want      string
	}{
		{"normal", "myproject", "claude", "ai-chat-myproject-claude"},
		{"spaces", "my project", "open code", "ai-chat-my-project-open-code"},
		{"dots", "my.project", "claude", "ai-chat-my-project-claude"},
		{"special chars", "my@project!", "claude#2", "ai-chat-my-project-claude-2"},
		{"uppercase", "MyProject", "Claude", "ai-chat-myproject-claude"},
		{"hyphens preserved", "my-project", "claude-oneshot", "ai-chat-my-project-claude-oneshot"},
		{"leading trailing special", ".project.", ".agent.", "ai-chat-project-agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionName(tt.workspace, tt.agent)
			if got != tt.want {
				t.Errorf("SessionName(%q, %q) = %q, want %q", tt.workspace, tt.agent, got, tt.want)
			}
		})
	}
}

func TestParseSessionName(t *testing.T) {
	agents := []string{"claude", "opencode", "copilot", "claude-oneshot"}

	tests := []struct {
		name          string
		input         string
		wantWorkspace string
		wantAgent     string
		wantOK        bool
	}{
		{"simple", "ai-chat-myproject-claude", "myproject", "claude", true},
		{"hyphenated workspace", "ai-chat-my-project-claude", "my-project", "claude", true},
		{"oneshot agent", "ai-chat-lab-claude-oneshot", "lab", "claude-oneshot", true},
		{"hyphenated workspace oneshot", "ai-chat-my-project-claude-oneshot", "my-project", "claude-oneshot", true},
		{"opencode agent", "ai-chat-work-opencode", "work", "opencode", true},
		{"copilot agent", "ai-chat-demo-copilot", "demo", "copilot", true},
		{"wrong prefix", "other-myproject-claude", "", "", false},
		{"no prefix", "myproject-claude", "", "", false},
		{"empty after prefix", "ai-chat-", "", "", false},
		{"no matching agent", "ai-chat-myproject-unknown", "", "", false},
		{"empty workspace", "ai-chat--claude", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, ag, ok := ParseSessionName(tt.input, agents)
			if ok != tt.wantOK {
				t.Errorf("ParseSessionName(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				return
			}
			if !tt.wantOK {
				return
			}
			if ws != tt.wantWorkspace {
				t.Errorf("ParseSessionName(%q) workspace = %q, want %q", tt.input, ws, tt.wantWorkspace)
			}
			if ag != tt.wantAgent {
				t.Errorf("ParseSessionName(%q) agent = %q, want %q", tt.input, ag, tt.wantAgent)
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
	defer tmx.KillSession(session)

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
	found := false
	for _, s := range sessions {
		if s == session {
			found = true
			break
		}
	}
	if !found {
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
