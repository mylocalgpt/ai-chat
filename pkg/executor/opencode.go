package executor

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// OpenCodeTmuxAdapter implements AgentAdapter for OpenCode running in tmux.
type OpenCodeTmuxAdapter struct {
	tmux         tmuxRunner
	pollInterval time.Duration
	timeout      time.Duration
	spawnTimeout time.Duration
	proxy        *SecurityProxy
}

// NewOpenCodeTmuxAdapter returns a new OpenCode tmux adapter.
func NewOpenCodeTmuxAdapter(tmux tmuxRunner, proxy *SecurityProxy) *OpenCodeTmuxAdapter {
	return &OpenCodeTmuxAdapter{
		tmux:         tmux,
		pollInterval: 1 * time.Second,
		timeout:      5 * time.Minute,
		spawnTimeout: 20 * time.Second,
		proxy:        proxy,
	}
}

// openCodeReadyRe matches the OpenCode input prompt area.
var openCodeReadyRe = regexp.MustCompile(`(?im)(>\s*$|ask anything|input|opencode)`)

// openCodeBusyRe matches the transient busy footer OpenCode shows while a
// reply is still being generated.
var openCodeBusyRe = regexp.MustCompile(`(?im)esc interrupt`)

// Name returns the adapter name.
func (a *OpenCodeTmuxAdapter) Name() string {
	return "opencode-tmux"
}

// Spawn starts OpenCode in a tmux session and creates the response file.
func (a *OpenCodeTmuxAdapter) Spawn(ctx context.Context, session core.SessionInfo) error {
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
func (a *OpenCodeTmuxAdapter) Send(ctx context.Context, session core.SessionInfo, message string) error {
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
	messageStarted := false

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
			cleanCurrent := StripANSI(raw)
			if !messageStarted && (paneContainsPromptEcho(cleanCurrent, message) || openCodeBusyRe.MatchString(cleanCurrent)) {
				messageStarted = true
			}
			if !messageStarted {
				continue
			}

			currentLines := strings.Split(raw, "\n")
			divergeIdx := findDivergence(snapshotLines, currentLines)
			if divergeIdx >= len(currentLines) {
				continue
			}

			newContent := currentLines[divergeIdx:]
			clean := StripANSI(strings.Join(newContent, "\n"))
			if openCodeBusyRe.MatchString(clean) {
				continue
			}

			candidate := extractOpenCodeResponse(clean)
			if candidate == "" {
				continue
			}

			response = candidate
			goto done
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

func extractOpenCodeResponse(pane string) string {
	var lines []string
	for _, line := range strings.Split(pane, "\n") {
		line = stripOpenCodeSidebar(line)
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmedLeft := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmedLeft, "┃") || strings.HasPrefix(trimmedLeft, "╹") {
			continue
		}
		if isOpenCodeChromeLine(trimmed) {
			continue
		}
		lines = append(lines, trimmed)
	}
	lines = dedupeAdjacentLines(lines)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stripOpenCodeSidebar(line string) string {
	if idx := strings.IndexRune(line, '█'); idx >= 0 {
		return line[:idx]
	}
	return line
}

func isOpenCodeChromeLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"ask anything...",
		"tab agents",
		"ctrl+p commands",
		"tip press f2",
		"tip opencode uses",
		"opencode 1.",
		"new session -",
		"build  gpt-",
		"build · gpt-",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.HasPrefix(lower, "context") || strings.HasPrefix(lower, "mcp") || strings.HasPrefix(lower, "lsp") || strings.HasPrefix(lower, "greeting:") || strings.HasPrefix(line, "~/") {
		return true
	}
	if !containsAlphaNumeric(line) {
		return true
	}
	return false
}

func paneContainsPromptEcho(pane, message string) bool {
	message = normalizeWhitespace(message)
	if message == "" {
		return true
	}
	return strings.Contains(normalizeWhitespace(pane), message)
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func dedupeAdjacentLines(lines []string) []string {
	if len(lines) < 2 {
		return lines
	}
	result := make([]string, 0, len(lines))
	var prev string
	for i, line := range lines {
		if i > 0 && line == prev {
			continue
		}
		result = append(result, line)
		prev = line
	}
	return result
}

func containsAlphaNumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// IsAlive reports whether the tmux session is still running.
func (a *OpenCodeTmuxAdapter) IsAlive(session core.SessionInfo) bool {
	return a.tmux.HasSession(session.Name)
}

// Stop kills the tmux session.
func (a *OpenCodeTmuxAdapter) Stop(_ context.Context, session core.SessionInfo) error {
	return a.tmux.KillSession(session.Name)
}

// OpenCode-specific status patterns.
var (
	openCodeRateLimitRe = regexp.MustCompile(`(?i)(rate limit|model error|too many requests)`)
	openCodeCrashRe     = regexp.MustCompile(`(?im)(^fatal:|panic:|\bsegfault\b|segmentation fault|fatal error|SIGSEGV|SIGABRT)`)
)

// DetectStatus parses output for OpenCode status signals.
// Kept for diagnostics even though not part of AgentAdapter interface.
func (a *OpenCodeTmuxAdapter) DetectStatus(output string) AgentStatus {
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
