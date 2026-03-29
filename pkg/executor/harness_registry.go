package executor

import (
	"fmt"
	"sort"
)

type HarnessRegistry struct {
	adapters map[string]AgentAdapter
}

func NewHarnessRegistry(tmux TmuxRunner, serverMgr *ServerManager, proxy *SecurityProxy) *HarnessRegistry {
	r := &HarnessRegistry{
		adapters: make(map[string]AgentAdapter),
	}
	r.adapters["opencode"] = NewOpenCodeServeAdapter(serverMgr)
	r.adapters["opencode-tmux"] = NewOpenCodeTmuxAdapter(tmux, proxy)
	r.adapters["copilot"] = NewCopilotAdapter(proxy)
	return r
}

func (r *HarnessRegistry) RegisterAdapter(name string, a AgentAdapter) {
	r.adapters[name] = a
}

func (r *HarnessRegistry) GetAdapter(agent string) (AgentAdapter, error) {
	a, ok := r.adapters[agent]
	if !ok {
		return nil, fmt.Errorf("no adapter for agent %q", agent)
	}
	return a, nil
}

func (r *HarnessRegistry) IsAdapter(agent string) bool {
	_, ok := r.adapters[agent]
	return ok
}

func (r *HarnessRegistry) KnownAgents() []string {
	agents := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		agents = append(agents, name)
	}
	sort.Strings(agents)
	return agents
}
