package executor

import (
	"context"
	"errors"
	"time"
)

// ErrNotImplemented indicates a harness is registered but not yet functional.
var ErrNotImplemented = errors.New("harness not implemented")

// CopilotHarness implements CLIHarness for GitHub Copilot CLI. This is a stub
// until the Copilot CLI interface is tested and confirmed.
type CopilotHarness struct {
	timeout time.Duration
}

// NewCopilotHarness returns a new Copilot harness stub.
func NewCopilotHarness() *CopilotHarness {
	return &CopilotHarness{
		timeout: 2 * time.Minute,
	}
}

// Execute is not yet implemented. Returns ErrNotImplemented.
func (h *CopilotHarness) Execute(ctx context.Context, workDir, message string) (string, error) {
	return "", ErrNotImplemented
}
