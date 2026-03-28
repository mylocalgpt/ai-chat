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
