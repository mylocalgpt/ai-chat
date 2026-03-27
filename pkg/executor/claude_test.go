package executor

import (
	"testing"
)

func TestDetectStatus(t *testing.T) {
	h := NewClaudeHarness(NewTmux())

	tests := []struct {
		name      string
		output    string
		wantState AgentState
	}{
		{
			"ready with prompt",
			"some output\n\n >\n",
			AgentReady,
		},
		{
			"rate limited usage",
			"Error: usage limit exceeded. Please try again later.",
			AgentRateLimited,
		},
		{
			"rate limited too many",
			"Too many requests, please slow down.",
			AgentRateLimited,
		},
		{
			"crashed panic",
			"goroutine 1 [running]:\npanic: runtime error",
			AgentCrashed,
		},
		{
			"crashed error",
			"error: connection refused",
			AgentCrashed,
		},
		{
			"exited",
			"Claude session ended. Goodbye!",
			AgentExited,
		},
		{
			"exited process terminated",
			"Process terminated with code 1",
			AgentExited,
		},
		{
			"working",
			"Thinking about your request...\nAnalyzing code...",
			AgentWorking,
		},
		{
			"empty output",
			"",
			AgentWorking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := h.DetectStatus(tt.output)
			if status.State != tt.wantState {
				t.Errorf("DetectStatus() state = %q, want %q (detail: %s)", status.State, tt.wantState, status.Detail)
			}
		})
	}
}

func TestClaudeOneShotParsing(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantResult    string
		wantSessionID string
		wantErr       bool
	}{
		{
			"valid response",
			`{"result":"Hello, world!","session_id":"abc123","total_cost_usd":0.05,"duration_ms":1234,"num_turns":1}`,
			"Hello, world!",
			"abc123",
			false,
		},
		{
			"empty result",
			`{"result":"","session_id":"xyz","total_cost_usd":0,"duration_ms":100,"num_turns":1}`,
			"",
			"xyz",
			false,
		},
		{
			"malformed json",
			`not json at all`,
			"",
			"",
			true,
		},
		{
			"partial json",
			`{"result": "partial`,
			"",
			"",
			true,
		},
		{
			"multiline result",
			`{"result":"line1\nline2\nline3","session_id":"sess1","total_cost_usd":0.1,"duration_ms":2000,"num_turns":2}`,
			"line1\nline2\nline3",
			"sess1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, sessionID, err := parseOneShotResponse([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseOneShotResponse() err = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if result != tt.wantResult {
				t.Errorf("parseOneShotResponse() result = %q, want %q", result, tt.wantResult)
			}
			if sessionID != tt.wantSessionID {
				t.Errorf("parseOneShotResponse() sessionID = %q, want %q", sessionID, tt.wantSessionID)
			}
		})
	}
}
