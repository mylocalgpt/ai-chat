package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// OpenCodeHarness implements AgentHarness for OpenCode running in tmux.
type OpenCodeHarness struct {
	tmux         *Tmux
	pollInterval time.Duration
	timeout      time.Duration
}

// NewOpenCodeHarness returns a new OpenCode harness.
func NewOpenCodeHarness(tmux *Tmux) *OpenCodeHarness {
	return &OpenCodeHarness{
		tmux:         tmux,
		pollInterval: 1 * time.Second,
		timeout:      5 * time.Minute,
	}
}

// openCodeReadyRe matches the OpenCode input prompt area.
var openCodeReadyRe = regexp.MustCompile(`(?im)(>\s*$|ask anything|input|opencode)`)

// Spawn starts OpenCode in the given tmux session and waits for the TUI to
// initialize.
func (h *OpenCodeHarness) Spawn(ctx context.Context, sessionName string) error {
	if err := h.tmux.SendKeys(sessionName, "opencode"); err != nil {
		return fmt.Errorf("opencode spawn: %w", err)
	}

	deadline := time.After(20 * time.Second)
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
			if openCodeReadyRe.MatchString(clean) {
				return nil
			}
		}
	}
}

// SendMessage captures the current pane state and sends the message.
func (h *OpenCodeHarness) SendMessage(ctx context.Context, sessionName string, message string) (string, error) {
	snapshot, err := h.tmux.CapturePaneRaw(sessionName, 200)
	if err != nil {
		return "", fmt.Errorf("opencode send: capture snapshot: %w", err)
	}
	if err := h.tmux.SendKeys(sessionName, message); err != nil {
		return "", fmt.Errorf("opencode send: %w", err)
	}
	return snapshot, nil
}

// ReadResponse polls capture-pane until OpenCode returns to its input prompt,
// then extracts the new content since the pre-send snapshot.
func (h *OpenCodeHarness) ReadResponse(ctx context.Context, sessionName string, snapshot string) (string, error) {
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
			divergeIdx := findDivergence(snapshotLines, currentLines)
			if divergeIdx >= len(currentLines) {
				continue
			}

			newContent := currentLines[divergeIdx:]
			clean := StripANSI(strings.Join(newContent, "\n"))

			// Check if the input prompt has reappeared.
			if openCodeReadyRe.MatchString(clean) && len(newContent) > 1 {
				return strings.TrimSpace(clean), nil
			}
		}
	}
}

// IsReady checks if OpenCode's input prompt is visible.
func (h *OpenCodeHarness) IsReady(ctx context.Context, sessionName string) (bool, error) {
	raw, err := h.tmux.CapturePaneRaw(sessionName, 50)
	if err != nil {
		return false, err
	}
	clean := StripANSI(raw)
	return openCodeReadyRe.MatchString(clean), nil
}

// OpenCode-specific status patterns.
var (
	openCodeRateLimitRe = regexp.MustCompile(`(?i)(rate limit|model error|too many requests)`)
	openCodeCrashRe     = regexp.MustCompile(`(?i)(error|panic|fatal)`)
)

// DetectStatus parses output for OpenCode status signals.
func (h *OpenCodeHarness) DetectStatus(output string) AgentStatus {
	if openCodeRateLimitRe.MatchString(output) {
		return AgentStatus{
			State:  AgentRateLimited,
			Detail: "rate limited",
		}
	}
	if openCodeCrashRe.MatchString(output) {
		return AgentStatus{
			State:  AgentCrashed,
			Detail: "agent crashed or errored",
		}
	}
	if openCodeReadyRe.MatchString(output) {
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
