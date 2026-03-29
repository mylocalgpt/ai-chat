package executor

import (
	"context"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

type AgentAdapter interface {
	Name() string
	Spawn(ctx context.Context, session core.SessionInfo) error
	Send(ctx context.Context, session core.SessionInfo, message string) error
	IsAlive(session core.SessionInfo) bool
	Stop(ctx context.Context, session core.SessionInfo) error
}

// StreamingAdapter extends AgentAdapter with real-time event streaming.
// Only adapters that support structured output (e.g. opencode serve) implement this.
type StreamingAdapter interface {
	AgentAdapter
	// SendStream sends a message and returns a channel of streaming events.
	// The channel is closed when the agent finishes (after EventIdle),
	// on context cancellation, or on stream error.
	SendStream(ctx context.Context, session core.SessionInfo, message string) (<-chan core.AgentEvent, error)
	// AbortStream cancels in-flight generation for the given session.
	AbortStream(ctx context.Context, session core.SessionInfo) error
	// GetAgentSessionID returns the agent's own session ID after Spawn.
	// Returns empty string if the adapter does not produce session IDs.
	GetAgentSessionID(session core.SessionInfo) string
}
