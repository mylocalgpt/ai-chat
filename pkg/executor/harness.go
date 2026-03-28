package executor

type AgentState string

const (
	AgentReady       AgentState = "ready"
	AgentWorking     AgentState = "working"
	AgentRateLimited AgentState = "rate_limited"
	AgentCrashed     AgentState = "crashed"
	AgentExited      AgentState = "exited"
)

type AgentStatus struct {
	State     AgentState
	Detail    string
	UsageInfo string
}
