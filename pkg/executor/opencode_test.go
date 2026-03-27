package executor

import "testing"

func TestOpenCodeDetectStatus(t *testing.T) {
	h := NewOpenCodeHarness(NewTmux())

	tests := []struct {
		name      string
		output    string
		wantState AgentState
	}{
		{
			"ready with prompt",
			"Some response text\n> \n",
			AgentReady,
		},
		{
			"ready with ask anything",
			"Welcome to OpenCode\nAsk anything...",
			AgentReady,
		},
		{
			"rate limited",
			"Model error: rate limit exceeded for gpt-4",
			AgentRateLimited,
		},
		{
			"rate limited too many",
			"Too many requests. Please wait.",
			AgentRateLimited,
		},
		{
			"crashed",
			"fatal: unexpected error in rendering",
			AgentCrashed,
		},
		{
			"crashed panic",
			"panic: runtime error: index out of range",
			AgentCrashed,
		},
		{
			"working",
			"Generating response...\nThinking...",
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
