package core

import (
	"context"
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
	Name          string // "ai-chat-lab-a3f2"
	Slug          string // "a3f2"
	Workspace     string // "lab"
	WorkspacePath string // "/home/chief/code/research-lab"
	Agent         string // "opencode"
	ResponseFile  string // full path to response JSON
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

// ResponseEvent represents a response from an agent session.
type ResponseEvent struct {
	SessionName string
	SessionID   int64
	SenderID    string
	Channel     string
	Content     string
}
