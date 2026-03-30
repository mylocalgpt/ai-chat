package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// Channel represents a messaging platform adapter (e.g. Telegram, web).
type Channel interface {
	Start(ctx context.Context) error
	Stop() error
	Send(ctx context.Context, msg OutboundMessage) error
}

// StreamResult holds the accumulated output from a streaming session,
// including the final response text and token/cost data from step-finish events.
type StreamResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
	Cost         float64
}

// StreamingChannel extends Channel with support for streaming agent events.
// Channels that implement this interface receive real-time progress updates
// instead of waiting for the final response.
type StreamingChannel interface {
	Channel
	SendStreaming(ctx context.Context, chatID int64, replyToID int, agentSessionID string, events <-chan AgentEvent) (StreamResult, error)
}

// InboundMessage is a message received from a messaging platform.
type InboundMessage struct {
	ID        string
	Channel   string // "telegram", "web"
	SenderID  string
	Content   string
	Timestamp time.Time
	Raw       any // platform-specific payload
}

// OutboundMessage is a message to be sent to a messaging platform.
type OutboundMessage struct {
	Channel     string
	RecipientID string
	Content     string
	ReplyToID   string // optional, for threading
}

// Workspace represents a project workspace for AI agent sessions.
type Workspace struct {
	ID        int64
	Name      string
	Path      string
	Host      string          // empty = local, "mac" = remote
	Metadata  json.RawMessage // aliases, description, default_agent, etc.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Session represents an active AI agent session in a workspace.
type Session struct {
	ID             int64
	WorkspaceID    int64
	Agent          string // "opencode", "copilot"
	Slug           string // random 4-char slug
	AgentSessionID string // agent's own session identifier
	TmuxSession    string
	Status         string // use SessionStatus constants
	StartedAt      time.Time
	LastActivity   time.Time
}

// SessionInfo holds computed, read-only information about a session.
type SessionInfo struct {
	Name           string // "ai-chat-lab-a3f2"
	Slug           string // "a3f2"
	Workspace      string // "lab"
	WorkspacePath  string // "/home/chief/code/research-lab"
	Agent          string // "opencode"
	ResponseFile   string // full path to response JSON
	AgentSessionID string // agent's own session identifier (e.g. opencode serve session ID)
}

// ActiveWorkspace tracks the selected workspace for a sender/channel pair.
type ActiveWorkspace struct {
	SenderID    string
	Channel     string
	WorkspaceID int64
}

// ActiveWorkspaceSession tracks the selected session for one workspace.
type ActiveWorkspaceSession struct {
	SenderID    string
	Channel     string
	WorkspaceID int64
	SessionID   int64
}

// MessageDirection indicates whether a message is inbound or outbound.
type MessageDirection string

const (
	InboundDirection  MessageDirection = "inbound"
	OutboundDirection MessageDirection = "outbound"
)

// MessageStatus tracks the processing state of a message.
type MessageStatus string

const (
	StatusPending    MessageStatus = "pending"
	StatusProcessing MessageStatus = "processing"
	StatusDone       MessageStatus = "done"
	StatusFailed     MessageStatus = "failed"
)

// SessionStatus tracks the lifecycle state of an agent session.
type SessionStatus string

const (
	SessionActive  SessionStatus = "active"
	SessionIdle    SessionStatus = "idle"
	SessionCrashed SessionStatus = "crashed"
	SessionExpired SessionStatus = "expired"
)

// AgentEventType identifies the kind of streaming event from an agent.
type AgentEventType string

const (
	EventText       AgentEventType = "text"
	EventTextDelta  AgentEventType = "text_delta"
	EventToolUse    AgentEventType = "tool_use"
	EventToolResult AgentEventType = "tool_result"
	EventStepStart  AgentEventType = "step_start"
	EventStepFinish AgentEventType = "step_finish"
	EventBusy       AgentEventType = "busy"
	EventIdle       AgentEventType = "idle"
	EventError      AgentEventType = "error"
)

// TokenUsage tracks input/output token counts for a step.
type TokenUsage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// AgentEvent is a single event from a streaming agent session.
// Fields are populated based on the event Type:
//   - EventText, EventTextDelta, EventError: Text
//   - EventToolUse: ToolName, ToolInput
//   - EventToolResult: ToolName, ToolOutput
//   - EventStepFinish: Tokens, Cost, Reason
//   - EventStepStart, EventBusy, EventIdle: no additional fields
type AgentEvent struct {
	Type       AgentEventType
	Text       string
	ToolName   string
	ToolInput  string
	ToolOutput string
	Tokens     *TokenUsage
	Cost       float64
	Reason     string
}

// ResponseSender sends a formatted response through a channel, handling
// summarization and document attachment for long content.
type ResponseSender interface {
	SendResponse(ctx context.Context, params ResponseParams) error
}

// ResponseParams contains everything needed to deliver a response to the user,
// including content, metadata for document filenames, and token usage.
type ResponseParams struct {
	ChatID       int64
	ReplyToID    string
	Content      string
	SessionName  string
	SessionSlug  string
	MsgIdx       int
	Workspace    string
	InputTokens  int
	OutputTokens int
	Cost         float64
}

// ResponseEvent represents a response from an agent session.
type ResponseEvent struct {
	SessionName    string
	SessionID      int64
	SenderID       string
	Channel        string
	Content        string
	Events         <-chan AgentEvent // nil for non-streaming responses
	ReplyToID      string            // original user message ID for reply threading
	AgentSessionID string            // opencode session ID (ses_xxx) for stop button
	ResponseFile   string            // path to response JSON for persistence
	SessionSlug    string            // short slug for filenames
	MsgIdx         int               // zero-indexed position among agent messages
	Workspace      string            // workspace path (needed by summarizer)
	InputTokens    int               // from step-finish event
	OutputTokens   int               // from step-finish event
	Cost           float64           // from step-finish event
}

// ctxKey is an unexported type for context keys in this package.
type ctxKey string

const requestIDKey ctxKey = "request_id"

// WithRequestID returns a new context with the given request ID attached.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID extracts the request ID from the context.
// Returns an empty string if no request ID is set.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// NewRequestID generates a new request correlation ID.
// Format: "req_" followed by 8 hex characters (4 random bytes).
func NewRequestID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}
