package mcp

import (
	"context"
	"encoding/json"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// MCPStore defines the store operations the MCP server needs.
type MCPStore interface {
	// Workspace operations
	CreateWorkspace(ctx context.Context, name, path, host string) (*core.Workspace, error)
	GetWorkspace(ctx context.Context, name string) (*core.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]core.Workspace, error)
	UpdateWorkspaceMetadata(ctx context.Context, id int64, metadata json.RawMessage) error
	DeleteWorkspace(ctx context.Context, id int64) error

	// Session operations
	GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error)
	ListSessions(ctx context.Context) ([]core.Session, error)
	UpdateSessionStatus(ctx context.Context, id int64, status string) error

	// Model config operations
	GetModelConfig(ctx context.Context, role string) (*store.ModelConfig, error)
	SetModelConfig(ctx context.Context, cfg store.ModelConfig) error
	ListModelConfigs(ctx context.Context) ([]store.ModelConfig, error)
}

// Executor defines the executor operations the MCP server needs.
type Executor interface {
	KillSession(ctx context.Context, workspaceID int64, agent string) error
	Execute(ctx context.Context, ws core.Workspace, agent, message string) (string, error)
}

// WorkspaceChangeNotifier is called after workspace mutations.
type WorkspaceChangeNotifier interface {
	OnWorkspacesChanged()
}

// ChannelAdapter provides platform connectivity checks and messaging.
type ChannelAdapter interface {
	Send(ctx context.Context, msg core.OutboundMessage) error
	IsConnected() bool
}
