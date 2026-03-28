package executor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// CopilotAdapter implements AgentAdapter for GitHub Copilot CLI.
// Uses the standalone copilot binary (GA Feb 2026), not the deprecated gh copilot extension.
type CopilotAdapter struct {
	timeout time.Duration
	proxy   *SecurityProxy
}

// NewCopilotAdapter returns a new Copilot adapter.
func NewCopilotAdapter(proxy *SecurityProxy) *CopilotAdapter {
	return &CopilotAdapter{
		timeout: 2 * time.Minute,
		proxy:   proxy,
	}
}

// Name returns the adapter name.
func (a *CopilotAdapter) Name() string {
	return "copilot"
}

// Spawn validates copilot is on PATH and creates the response file.
// Stateless adapter, no persistent process to start.
func (a *CopilotAdapter) Spawn(_ context.Context, session core.SessionInfo) error {
	// Verify copilot is on PATH.
	if _, err := exec.LookPath("copilot"); err != nil {
		return fmt.Errorf("copilot not found on PATH: %w", err)
	}

	// Create response file. Use the directory from session.ResponseFile.
	dir := filepath.Dir(session.ResponseFile)
	_, err := NewResponseFile(dir, session)
	if err != nil {
		return fmt.Errorf("copilot spawn: create response file: %w", err)
	}

	return nil
}

// Send runs copilot with the message and writes the response to the file.
func (a *CopilotAdapter) Send(ctx context.Context, session core.SessionInfo, message string) error {
	var flags []SecurityFlag

	// Scan input message for security patterns.
	if a.proxy != nil {
		flags = append(flags, a.proxy.Scan(message)...)
	}

	// Build command: copilot -p <message> -s --allow-all-tools
	// -p: non-interactive mode, passes prompt directly
	// -s: silent mode, outputs only agent response
	// --allow-all-tools: skips tool confirmation prompts
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "copilot", "-p", message, "-s", "--allow-all-tools")
	cmd.Dir = session.WorkspacePath

	output, err := cmd.Output()
	var sendErr error
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			sendErr = ErrResponseTimeout
		} else {
			sendErr = fmt.Errorf("copilot send: %w", err)
		}
	}

	// Scan response for security patterns.
	if a.proxy != nil && len(output) > 0 {
		flags = append(flags, a.proxy.Scan(string(output))...)
	}

	// Append agent response to response file.
	if len(output) > 0 {
		if err := AppendMessage(session.ResponseFile, ResponseMessage{
			Role:      "agent",
			Content:   string(output),
			Timestamp: time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("copilot send: append agent message: %w", err)
		}
	}

	// Return security flags if any.
	if len(flags) > 0 {
		return &SecurityFlagError{Flags: flags, Err: sendErr}
	}

	return sendErr
}

// IsAlive always returns true. Copilot is stateless.
func (a *CopilotAdapter) IsAlive(_ core.SessionInfo) bool {
	return true
}

// Stop is a no-op. Nothing to clean up for stateless adapter.
func (a *CopilotAdapter) Stop(_ context.Context, _ core.SessionInfo) error {
	return nil
}
