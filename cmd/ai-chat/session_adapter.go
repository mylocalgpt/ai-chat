package main

import (
	"context"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	mcppkg "github.com/mylocalgpt/ai-chat/pkg/mcp"
	"github.com/mylocalgpt/ai-chat/pkg/session"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

const (
	mcpSenderID = "mcp"
	mcpChannel  = "system"
)

type executorSessionManager struct {
	manager *session.Manager
	store   *store.Store
}

func newExecutorSessionManager(manager *session.Manager, st *store.Store) *executorSessionManager {
	return &executorSessionManager{manager: manager, store: st}
}

func (e *executorSessionManager) CreateSession(ctx context.Context, ws core.Workspace, agent string) (*core.Session, error) {
	info, err := e.manager.CreateSession(ctx, ws.ID, agent)
	if err != nil {
		return nil, err
	}
	return e.store.GetSessionByTmuxSession(ctx, info.Name)
}

func (e *executorSessionManager) ClearSession(ctx context.Context, sessionID int64) (*core.Session, error) {
	info, err := e.manager.ClearSessionByID(ctx, mcpSenderID, mcpChannel, sessionID)
	if err != nil {
		return nil, err
	}
	return e.store.GetSessionByTmuxSession(ctx, info.Name)
}

func (e *executorSessionManager) KillSession(ctx context.Context, sessionID int64) error {
	return e.manager.KillSessionByID(ctx, mcpSenderID, mcpChannel, sessionID)
}

func (e *executorSessionManager) Send(ctx context.Context, sessionID int64, message string) error {
	return e.manager.SendToSession(ctx, mcpSenderID, mcpChannel, sessionID, message, "")
}

func (e *executorSessionManager) ApproveSend(ctx context.Context, pendingID string, approved bool) (string, error) {
	return e.manager.HandleSecurityDecision(ctx, mcpSenderID, mcpChannel, pendingID, approved)
}

func (e *executorSessionManager) SwitchSession(ctx context.Context, workspaceID, sessionID int64) (*core.Session, error) {
	if err := e.store.SetActiveWorkspace(ctx, mcpSenderID, mcpChannel, workspaceID); err != nil {
		return nil, fmt.Errorf("setting active workspace: %w", err)
	}
	if err := e.manager.SwitchActiveSession(ctx, mcpSenderID, mcpChannel, workspaceID, sessionID); err != nil {
		return nil, err
	}
	sess, err := e.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

var _ mcppkg.SessionManager = (*executorSessionManager)(nil)
