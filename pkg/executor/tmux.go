package executor

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tmux wraps tmux commands via os/exec. It is stateless and safe for
// concurrent use.
type Tmux struct{}

// NewTmux returns a new Tmux wrapper.
func NewTmux() *Tmux { return &Tmux{} }

// NewSession creates a detached tmux session with the given name and working
// directory.
func (t *Tmux) NewSession(name, workDir string) error {
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", workDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux new-session: %s: %w", bytes.TrimSpace(out), err)
	}
	return nil
}

// HasSession reports whether a tmux session with the given name exists.
func (t *Tmux) HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// KillSession destroys the tmux session with the given name.
func (t *Tmux) KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux kill-session: %s: %w", bytes.TrimSpace(out), err)
	}
	return nil
}

// ListSessions returns the names of all running tmux sessions. If the tmux
// server is not running, it returns an empty slice (not an error).
func (t *Tmux) ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		// tmux exits non-zero when the server is not running.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// SendKeys sends text to the given tmux session followed by Enter. The text is
// sent with the -l (literal) flag so tmux does not interpret key names. Enter
// is sent as a separate command after a 300ms delay.
func (t *Tmux) SendKeys(session, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-l", "-t", session, text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys (text): %s: %w", bytes.TrimSpace(out), err)
	}

	time.Sleep(300 * time.Millisecond)

	cmd = exec.Command("tmux", "send-keys", "-t", session, "Enter")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys (enter): %s: %w", bytes.TrimSpace(out), err)
	}
	return nil
}

// CapturePaneRaw captures the last N lines of the given tmux session's pane
// and returns the raw output including ANSI escape sequences. Callers should
// use StripANSI if they need clean text.
func (t *Tmux) CapturePaneRaw(session string, lines int) (string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-t", session, "-p", "-S", fmt.Sprintf("-%d", lines))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %w", err)
	}
	return string(out), nil
}
