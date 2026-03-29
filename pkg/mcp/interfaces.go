package mcp

import (
	"context"
	"encoding/json"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

type SessionManager interface {
	ListSessions(ctx context.Context) ([]core.Session, error)
	ListSessionsForWorkspace(ctx context.Context, workspaceID int64) ([]core.Session, error)
	GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error)
	GetSessionByName(ctx context.Context, name string) (*core.Session, error)

	CreateSession(ctx context.Context, workspace core.Workspace, agent string) (*core.Session, error)
	ClearSession(ctx context.Context, session core.Session) (*core.Session, error)
	KillSession(ctx context.Context, sessionID int64) error
	Send(ctx context.Context, sessionID int64, message string) error
}

type MCPStore interface {
	Ping(ctx context.Context) error

	CreateWorkspace(ctx context.Context, name, path, host string) (*core.Workspace, error)
	GetWorkspace(ctx context.Context, name string) (*core.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]core.Workspace, error)
	UpdateWorkspaceMetadata(ctx context.Context, id int64, metadata json.RawMessage) error
	DeleteWorkspace(ctx context.Context, id int64) error
	RenameWorkspace(ctx context.Context, id int64, newName string) error

	GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error)
	ListSessions(ctx context.Context) ([]core.Session, error)
	ListSessionsForWorkspace(ctx context.Context, workspaceID int64) ([]core.Session, error)
	GetSessionByName(ctx context.Context, name string) (*core.Session, error)
}

type WorkspaceChangeNotifier interface {
	OnWorkspacesChanged()
}

type ChannelAdapter interface {
	Send(ctx context.Context, msg core.OutboundMessage) error
	IsConnected() bool
}
