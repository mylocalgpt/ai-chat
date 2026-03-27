package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// ClaudeOneShotHarness implements CLIHarness for quick Claude queries via the
// CLI's one-shot mode. It is stateless and safe for concurrent use.
type ClaudeOneShotHarness struct {
	timeout time.Duration
}

// OneShotOption configures a ClaudeOneShotHarness.
type OneShotOption func(*ClaudeOneShotHarness)

// WithOneShotTimeout sets the maximum time Execute waits for a response.
func WithOneShotTimeout(d time.Duration) OneShotOption {
	return func(h *ClaudeOneShotHarness) { h.timeout = d }
}

// NewClaudeOneShotHarness returns a new one-shot harness with the given
// options.
func NewClaudeOneShotHarness(opts ...OneShotOption) *ClaudeOneShotHarness {
	h := &ClaudeOneShotHarness{
		timeout: 2 * time.Minute,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// claudeOneShotResponse is the JSON structure returned by claude -p
// --output-format json.
type claudeOneShotResponse struct {
	Result    string  `json:"result"`
	SessionID string  `json:"session_id"`
	CostUSD   float64 `json:"total_cost_usd"`
	Duration  int64   `json:"duration_ms"`
	NumTurns  int     `json:"num_turns"`
}

// parseOneShotResponse extracts the result text from a Claude one-shot JSON
// response.
func parseOneShotResponse(data []byte) (string, string, error) {
	var resp claudeOneShotResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", fmt.Errorf("claude oneshot: parse response: %w", err)
	}
	return resp.Result, resp.SessionID, nil
}

// Execute runs a one-shot Claude query and returns the response text.
// Each call is independent; session continuity via --resume should be managed
// by the caller if needed.
func (h *ClaudeOneShotHarness) Execute(ctx context.Context, workDir, message string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	args := []string{"-p", "--output-format", "json", "--bare", message}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = workDir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude oneshot: %w", err)
	}

	result, _, err := parseOneShotResponse(out)
	if err != nil {
		return "", err
	}
	return result, nil
}
