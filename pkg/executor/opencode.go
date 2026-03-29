package executor

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// OpenCodeAdapter implements AgentAdapter for OpenCode running in tmux.
type OpenCodeAdapter struct {
	tmux         tmuxRunner
	pollInterval time.Duration
	timeout      time.Duration
	spawnTimeout time.Duration
	proxy        *SecurityProxy
}

// NewOpenCodeAdapter returns a new OpenCode adapter.
func NewOpenCodeAdapter(tmux tmuxRunner, proxy *SecurityProxy) *OpenCodeAdapter {
	return &OpenCodeAdapter{
		tmux:         tmux,
		pollInterval: 1 * time.Second,
		timeout:      5 * time.Minute,
		spawnTimeout: 20 * time.Second,
		proxy:        proxy,
	}
}

// openCodeReadyRe matches the OpenCode input prompt area.
var openCodeReadyRe = regexp.MustCompile(`(?im)(>\s*$|ask anything|input|opencode)`)

// Name returns the adapter name.
func (a *OpenCodeAdapter) Name() string {
	return "opencode"
}

// Spawn starts OpenCode in a tmux session and creates the response file.
func (a *OpenCodeAdapter) Spawn(ctx context.Context, session core.SessionInfo) error {
	// Create tmux session.
	if err := a.tmux.NewSession(session.Name, session.WorkspacePath); err != nil {
		return fmt.Errorf("opencode spawn: create tmux session: %w", err)
	}

	// Send opencode command.
	if err := a.tmux.SendKeys(session.Name, "opencode"); err != nil {
		_ = a.tmux.KillSession(session.Name)
		return fmt.Errorf("opencode spawn: send keys: %w", err)
	}

	// Wait for TUI ready prompt.
	deadline := time.After(a.spawnTimeout)
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = a.tmux.KillSession(session.Name)
			return ctx.Err()
		case <-deadline:
			_ = a.tmux.KillSession(session.Name)
			return ErrSpawnTimeout
		case <-ticker.C:
			out, err := a.tmux.CapturePaneRaw(session.Name, 200)
			if err != nil {
				continue
			}
			clean := StripANSI(out)
			if openCodeReadyRe.MatchString(clean) {
				// Create response file. Use the directory from session.ResponseFile.
				dir := filepath.Dir(session.ResponseFile)
				_, err := NewResponseFile(dir, session)
				if err != nil {
					_ = a.tmux.KillSession(session.Name)
					return fmt.Errorf("opencode spawn: create response file: %w", err)
				}
				return nil
			}
		}
	}
}

// Send sends a message to OpenCode and writes the response to the file.
func (a *OpenCodeAdapter) Send(ctx context.Context, session core.SessionInfo, message string) error {
	// Capture pre-send snapshot.
	snapshot, err := a.tmux.CapturePaneRaw(session.Name, 200)
	if err != nil {
		return fmt.Errorf("opencode send: capture snapshot: %w", err)
	}

	// Send message.
	if err := a.tmux.SendKeys(session.Name, message); err != nil {
		return fmt.Errorf("opencode send: send keys: %w", err)
	}

	// Poll for response.
	deadline := time.After(a.timeout)
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	snapshotLines := strings.Split(snapshot, "\n")

	var response string
	var sendErr error

	for {
		select {
		case <-ctx.Done():
			sendErr = ctx.Err()
			goto done
		case <-deadline:
			sendErr = ErrResponseTimeout
			goto done
		case <-ticker.C:
			raw, err := a.tmux.CapturePaneRaw(session.Name, 200)
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
				response = strings.TrimSpace(clean)
				goto done
			}
		}
	}

done:
	// Append agent response to response file.
	if response != "" {
		if err := AppendMessage(session.ResponseFile, ResponseMessage{
			Role:      "agent",
			Content:   response,
			Timestamp: time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("opencode send: append agent message: %w", err)
		}
	}

	return sendErr
}

// IsAlive reports whether the tmux session is still running.
func (a *OpenCodeAdapter) IsAlive(session core.SessionInfo) bool {
	return a.tmux.HasSession(session.Name)
}

// Stop kills the tmux session.
func (a *OpenCodeAdapter) Stop(_ context.Context, session core.SessionInfo) error {
	return a.tmux.KillSession(session.Name)
}

// OpenCode-specific status patterns.
var (
	openCodeRateLimitRe = regexp.MustCompile(`(?i)(rate limit|model error|too many requests)`)
	openCodeCrashRe     = regexp.MustCompile(`(?im)(^fatal:|panic:|\bsegfault\b|segmentation fault|fatal error|SIGSEGV|SIGABRT)`)
)

// DetectStatus parses output for OpenCode status signals.
// Kept for diagnostics even though not part of AgentAdapter interface.
func (a *OpenCodeAdapter) DetectStatus(output string) AgentStatus {
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
