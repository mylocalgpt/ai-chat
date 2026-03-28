package executor

import (
	"testing"
)

func TestRegistryGetTmux(t *testing.T) {
	r := NewHarnessRegistry(NewTmux())

	// Known tmux agents should return a harness.
	for _, agent := range []string{"opencode"} {
		h, err := r.GetTmux(agent)
		if err != nil {
			t.Errorf("GetTmux(%q) unexpected error: %v", agent, err)
		}
		if h == nil {
			t.Errorf("GetTmux(%q) returned nil harness", agent)
		}
	}

	// Unknown agent should return error.
	_, err := r.GetTmux("unknown")
	if err == nil {
		t.Error("GetTmux(unknown) expected error, got nil")
	}
}

func TestRegistryGetCLI(t *testing.T) {
	r := NewHarnessRegistry(NewTmux())

	// Unknown agent should return error.
	_, err := r.GetCLI("unknown")
	if err == nil {
		t.Error("GetCLI(unknown) expected error, got nil")
	}

	// Copilot is not registered by default.
	_, err = r.GetCLI("copilot")
	if err == nil {
		t.Error("GetCLI(copilot) expected error (not registered by default)")
	}
}

func TestRegistryIsTmux(t *testing.T) {
	r := NewHarnessRegistry(NewTmux())

	tests := []struct {
		agent string
		want  bool
	}{
		{"opencode", true},
		{"copilot", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			if got := r.IsTmux(tt.agent); got != tt.want {
				t.Errorf("IsTmux(%q) = %v, want %v", tt.agent, got, tt.want)
			}
		})
	}
}

func TestRegistryKnownAgents(t *testing.T) {
	r := NewHarnessRegistry(NewTmux())
	agents := r.KnownAgents()

	// Should include opencode (sorted).
	want := []string{"opencode"}
	if len(agents) != len(want) {
		t.Fatalf("KnownAgents() = %v, want %v", agents, want)
	}
	for i, a := range want {
		if agents[i] != a {
			t.Errorf("KnownAgents()[%d] = %q, want %q", i, agents[i], a)
		}
	}
}
