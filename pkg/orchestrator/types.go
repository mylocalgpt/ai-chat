package orchestrator

import "github.com/mylocalgpt/ai-chat/pkg/core"

// Action type constants.
const (
	ActionWorkspaceSwitch = "workspace_switch"
	ActionAgentTask       = "agent_task"
	ActionStatus          = "status"
	ActionDirectAnswer    = "direct_answer"
	ActionMetaCommand     = "meta_command"
)

var validActionTypes = map[string]bool{
	ActionWorkspaceSwitch: true,
	ActionAgentTask:       true,
	ActionStatus:          true,
	ActionDirectAnswer:    true,
	ActionMetaCommand:     true,
}

// Action represents the orchestrator's decision about how to handle a message.
type Action struct {
	Type       string  `json:"type"`
	Workspace  string  `json:"workspace"`
	Agent      string  `json:"agent"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// UserContext holds the current state for a user in a channel.
type UserContext struct {
	SenderID        string
	ActiveWorkspace *core.Workspace
	Channel         string
}

// Message represents a single chat message in OpenAI-compatible format.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the request body for OpenRouter chat completions.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// ChatResponse is the response from OpenRouter chat completions.
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
