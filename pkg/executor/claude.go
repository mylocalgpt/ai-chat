package executor

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Sentinel errors for Claude harness timeouts.
var (
	ErrSpawnTimeout    = errors.New("claude: spawn timed out waiting for prompt")
	ErrResponseTimeout = errors.New("claude: response timed out")
)

// ClaudeHarness implements AgentHarness for Claude Code running in tmux.
type ClaudeHarness struct {
	tmux           *Tmux
	permissionFlag string
	pollInterval   time.Duration
	timeout        time.Duration
}

// ClaudeOption configures a ClaudeHarness.
type ClaudeOption func(*ClaudeHarness)

// WithPermissionFlag sets the permission flag passed to the Claude CLI.
func WithPermissionFlag(flag string) ClaudeOption {
	return func(h *ClaudeHarness) { h.permissionFlag = flag }
}

// WithPollInterval sets how often ReadResponse polls for new output.
func WithPollInterval(d time.Duration) ClaudeOption {
	return func(h *ClaudeHarness) { h.pollInterval = d }
}

// WithTimeout sets the maximum time ReadResponse waits before returning
// ErrResponseTimeout.
func WithTimeout(d time.Duration) ClaudeOption {
	return func(h *ClaudeHarness) { h.timeout = d }
}

// NewClaudeHarness returns a new ClaudeHarness with the given options.
func NewClaudeHarness(tmux *Tmux, opts ...ClaudeOption) *ClaudeHarness {
	h := &ClaudeHarness{
		tmux:           tmux,
		permissionFlag: "--dangerously-skip-permissions",
		pollInterval:   1 * time.Second,
		timeout:        10 * time.Minute,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// promptRe matches a Claude Code prompt line (just a > with optional
// whitespace).
var promptRe = regexp.MustCompile(`(?m)^\s*>\s*$`)

// Spawn starts Claude Code in the given tmux session. It unsets CLAUDECODE
// and launches the CLI, then polls for the prompt to appear.
func (h *ClaudeHarness) Spawn(ctx context.Context, sessionName string) error {
	cmd := fmt.Sprintf("unset CLAUDECODE && claude %s", h.permissionFlag)
	if err := h.tmux.SendKeys(sessionName, cmd); err != nil {
		return fmt.Errorf("claude spawn: %w", err)
	}

	deadline := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return ErrSpawnTimeout
		case <-ticker.C:
			out, err := h.tmux.CapturePaneRaw(sessionName, 200)
			if err != nil {
				continue
			}
			clean := StripANSI(out)
			if promptRe.MatchString(clean) {
				return nil
			}
		}
	}
}

// SendMessage snapshots the current pane, sends the message, and returns the
// pre-send snapshot.
func (h *ClaudeHarness) SendMessage(ctx context.Context, sessionName string, message string) (string, error) {
	snapshot, err := h.tmux.CapturePaneRaw(sessionName, 200)
	if err != nil {
		return "", fmt.Errorf("claude send: capture snapshot: %w", err)
	}
	if err := h.tmux.SendKeys(sessionName, message); err != nil {
		return "", fmt.Errorf("claude send: %w", err)
	}
	return snapshot, nil
}

// ReadResponse polls capture-pane until Claude returns to its prompt, then
// extracts and returns only the new response text since the pre-send snapshot.
func (h *ClaudeHarness) ReadResponse(ctx context.Context, sessionName string, snapshot string) (string, error) {
	deadline := time.After(h.timeout)
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	snapshotLines := strings.Split(snapshot, "\n")

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", ErrResponseTimeout
		case <-ticker.C:
			raw, err := h.tmux.CapturePaneRaw(sessionName, 200)
			if err != nil {
				continue
			}

			currentLines := strings.Split(raw, "\n")

			// Find the divergence point between snapshot and current.
			divergeIdx := findDivergence(snapshotLines, currentLines)
			if divergeIdx >= len(currentLines) {
				// No new content yet.
				continue
			}

			newContent := currentLines[divergeIdx:]

			// Check if the prompt has reappeared in the new content.
			promptIdx := -1
			for i := len(newContent) - 1; i >= 0; i-- {
				clean := StripANSI(newContent[i])
				if promptRe.MatchString(clean) {
					promptIdx = i
					break
				}
			}

			if promptIdx < 0 {
				// Agent still working, no prompt yet.
				continue
			}

			// Extract response lines between divergence and prompt.
			responseLines := newContent[:promptIdx]
			response := StripANSI(strings.Join(responseLines, "\n"))
			return strings.TrimSpace(response), nil
		}
	}
}

// findDivergence returns the index of the first line in current that differs
// from snapshot.
func findDivergence(snapshot, current []string) int {
	n := min(len(snapshot), len(current))
	for i := range n {
		if snapshot[i] != current[i] {
			return i
		}
	}
	return n
}

// IsReady checks if the last non-empty line of the pane looks like a Claude
// prompt.
func (h *ClaudeHarness) IsReady(ctx context.Context, sessionName string) (bool, error) {
	raw, err := h.tmux.CapturePaneRaw(sessionName, 50)
	if err != nil {
		return false, err
	}
	clean := StripANSI(raw)
	lines := strings.Split(strings.TrimRight(clean, "\n"), "\n")
	if len(lines) == 0 {
		return false, nil
	}
	last := lines[len(lines)-1]
	return promptRe.MatchString(last), nil
}

// Status detection patterns (operate on ANSI-stripped text).
var (
	rateLimitRe = regexp.MustCompile(`(?i)(usage limit|rate limit|try again later|too many requests)`)
	crashRe     = regexp.MustCompile(`(?i)(^error:|panic:|segfault|segmentation fault|fatal error)`)
	exitedRe    = regexp.MustCompile(`(?i)(exited|session ended|process terminated)`)
)

// DetectStatus parses output for agent status signals.
func (h *ClaudeHarness) DetectStatus(output string) AgentStatus {
	if rateLimitRe.MatchString(output) {
		return AgentStatus{
			State:  AgentRateLimited,
			Detail: "rate limited",
		}
	}
	if crashRe.MatchString(output) {
		return AgentStatus{
			State:  AgentCrashed,
			Detail: "agent crashed or errored",
		}
	}
	if exitedRe.MatchString(output) {
		return AgentStatus{
			State:  AgentExited,
			Detail: "agent exited",
		}
	}
	if promptRe.MatchString(output) {
		return AgentStatus{
			State:  AgentReady,
			Detail: "waiting for input",
		}
	}
	return AgentStatus{
		State:  AgentWorking,
		Detail: "processing",
	}
}
