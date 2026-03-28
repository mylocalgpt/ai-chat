package executor

import "testing"

func TestOpenCodeDetectStatus(t *testing.T) {
	a := NewOpenCodeAdapter(NewTmux(), nil)

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
			"crashed fatal",
			"fatal: unexpected error in rendering",
			AgentCrashed,
		},
		{
			"crashed panic",
			"panic: runtime error: index out of range",
			AgentCrashed,
		},
		{
			"crashed segfault",
			"caught segfault signal",
			AgentCrashed,
		},
		{
			"normal response with error word",
			"The error you're seeing is caused by a missing import.\nHere's how to fix it.",
			AgentWorking,
		},
		{
			"working",
			"Generating response...\nThinking...",
			AgentWorking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := a.DetectStatus(tt.output)
			if status.State != tt.wantState {
				t.Errorf("DetectStatus() state = %q, want %q (detail: %s)", status.State, tt.wantState, status.Detail)
			}
		})
	}
}
