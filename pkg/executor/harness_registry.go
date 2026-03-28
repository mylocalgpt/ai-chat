package executor

import (
	"fmt"
	"sort"
)

// HarnessRegistry maps agent names to their harness implementations.
type HarnessRegistry struct {
	tmuxHarnesses map[string]AgentHarness
	cliHarnesses  map[string]CLIHarness
}

// NewHarnessRegistry returns a registry with the default harnesses registered.
func NewHarnessRegistry(tmux *Tmux) *HarnessRegistry {
	r := &HarnessRegistry{
		tmuxHarnesses: make(map[string]AgentHarness),
		cliHarnesses:  make(map[string]CLIHarness),
	}
	r.tmuxHarnesses["opencode"] = NewOpenCodeHarness(tmux)
	return r
}

// RegisterTmux adds a tmux harness for the given agent name.
func (r *HarnessRegistry) RegisterTmux(name string, h AgentHarness) {
	r.tmuxHarnesses[name] = h
}

// RegisterCLI adds a CLI harness for the given agent name.
func (r *HarnessRegistry) RegisterCLI(name string, h CLIHarness) {
	r.cliHarnesses[name] = h
}

// GetTmux returns the tmux harness for the given agent, or an error if not
// found.
func (r *HarnessRegistry) GetTmux(agent string) (AgentHarness, error) {
	h, ok := r.tmuxHarnesses[agent]
	if !ok {
		return nil, fmt.Errorf("no tmux harness for agent %q", agent)
	}
	return h, nil
}

// GetCLI returns the CLI harness for the given agent, or an error if not
// found.
func (r *HarnessRegistry) GetCLI(agent string) (CLIHarness, error) {
	h, ok := r.cliHarnesses[agent]
	if !ok {
		return nil, fmt.Errorf("no CLI harness for agent %q", agent)
	}
	return h, nil
}

// IsTmux reports whether the agent is registered as a tmux harness.
func (r *HarnessRegistry) IsTmux(agent string) bool {
	_, ok := r.tmuxHarnesses[agent]
	return ok
}

// KnownAgents returns all registered agent names (both tmux and CLI), sorted
// alphabetically.
func (r *HarnessRegistry) KnownAgents() []string {
	seen := make(map[string]struct{})
	for name := range r.tmuxHarnesses {
		seen[name] = struct{}{}
	}
	for name := range r.cliHarnesses {
		seen[name] = struct{}{}
	}
	agents := make([]string, 0, len(seen))
	for name := range seen {
		agents = append(agents, name)
	}
	sort.Strings(agents)
	return agents
}
