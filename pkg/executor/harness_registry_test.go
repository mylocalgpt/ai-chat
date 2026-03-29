package executor

import (
	"testing"
)

func TestRegistryGetAdapter(t *testing.T) {
	r := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())

	for _, agent := range []string{"opencode", "opencode-tmux", "copilot"} {
		a, err := r.GetAdapter(agent)
		if err != nil {
			t.Errorf("GetAdapter(%q) unexpected error: %v", agent, err)
		}
		if a == nil {
			t.Errorf("GetAdapter(%q) returned nil adapter", agent)
		}
	}

	_, err := r.GetAdapter("unknown")
	if err == nil {
		t.Error("GetAdapter(unknown) expected error, got nil")
	}
}

func TestRegistryIsAdapter(t *testing.T) {
	r := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())

	tests := []struct {
		agent string
		want  bool
	}{
		{"opencode", true},
		{"opencode-tmux", true},
		{"copilot", true},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			if got := r.IsAdapter(tt.agent); got != tt.want {
				t.Errorf("IsAdapter(%q) = %v, want %v", tt.agent, got, tt.want)
			}
		})
	}
}

func TestRegistryKnownAgents(t *testing.T) {
	r := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	agents := r.KnownAgents()

	want := []string{"copilot", "opencode", "opencode-tmux"}
	if len(agents) != len(want) {
		t.Fatalf("KnownAgents() = %v, want %v", agents, want)
	}
	for i, a := range want {
		if agents[i] != a {
			t.Errorf("KnownAgents()[%d] = %q, want %q", i, agents[i], a)
		}
	}
}
