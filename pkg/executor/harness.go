package executor

import "context"

// AgentHarness defines how a tmux-based agent is spawned, messaged, and
// monitored. The interface is stateless: snapshot state is passed explicitly
// between SendMessage and ReadResponse so a single harness instance can safely
// serve multiple concurrent sessions.
type AgentHarness interface {
	// Spawn starts the agent in the given tmux session (session already
	// created by the executor).
	Spawn(ctx context.Context, sessionName string) error

	// SendMessage snapshots the current pane output, sends the user message,
	// and returns the pre-send snapshot for use by ReadResponse.
	SendMessage(ctx context.Context, sessionName string, message string) (snapshot string, err error)

	// ReadResponse polls capture-pane until the agent finishes, diffs against
	// the provided pre-send snapshot, strips ANSI, and returns only the new
	// response text.
	ReadResponse(ctx context.Context, sessionName string, snapshot string) (string, error)

	// IsReady checks if the agent is at its prompt and ready for input.
	IsReady(ctx context.Context, sessionName string) (bool, error)

	// DetectStatus parses captured output for status signals.
	DetectStatus(output string) AgentStatus
}

// CLIHarness is for agents that use direct CLI calls instead of persistent
// tmux sessions.
type CLIHarness interface {
	Execute(ctx context.Context, workDir, message string) (string, error)
}

// AgentState represents the current state of an agent.
type AgentState string

const (
	AgentReady       AgentState = "ready"
	AgentWorking     AgentState = "working"
	AgentRateLimited AgentState = "rate_limited"
	AgentCrashed     AgentState = "crashed"
	AgentExited      AgentState = "exited"
)

// AgentStatus holds parsed status information from agent output.
type AgentStatus struct {
	State     AgentState
	Detail    string // human-readable explanation
	UsageInfo string // parsed usage data if available
}
