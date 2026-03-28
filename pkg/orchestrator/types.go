package orchestrator

import (
	"encoding/json"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

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
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []ToolDef `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
}

// ChatResponse is the response from OpenRouter chat completions.
type ChatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ToolDef defines a tool available for the LLM to call.
type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef is the function schema inside a ToolDef.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall represents an LLM's request to invoke a tool.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name and arguments inside a ToolCall.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultMessage is a message carrying a tool's response back to the LLM.
type ToolResultMessage struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// AssistantMessage is an assistant message that may contain tool calls.
type AssistantMessage struct {
	Role      string     `json:"role"`
	Content   *string    `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
