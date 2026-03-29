package main

import (
	"context"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	mcppkg "github.com/mylocalgpt/ai-chat/pkg/mcp"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type executorSessionManager struct {
	exec  *executor.Executor
	store *store.Store
}

func newExecutorSessionManager(exec *executor.Executor, st *store.Store) *executorSessionManager {
	return &executorSessionManager{exec: exec, store: st}
}

func (e *executorSessionManager) ListSessions(ctx context.Context) ([]core.Session, error) {
	infos, err := e.exec.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]core.Session, len(infos))
	for i, info := range infos {
		sessions[i] = info.Session
	}
	return sessions, nil
}

func (e *executorSessionManager) ListSessionsForWorkspace(ctx context.Context, workspaceID int64) ([]core.Session, error) {
	return e.store.ListActiveSessionsForWorkspace(ctx, workspaceID)
}

func (e *executorSessionManager) GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error) {
	return e.store.GetActiveSession(ctx, workspaceID)
}

func (e *executorSessionManager) GetSessionByName(ctx context.Context, name string) (*core.Session, error) {
	return e.store.GetSessionByTmuxSession(ctx, name)
}

func (e *executorSessionManager) SetActiveSession(ctx context.Context, workspaceID int64, sessionID int64) error {
	return fmt.Errorf("SetActiveSession not implemented for MCP adapter")
}

func (e *executorSessionManager) CreateSession(ctx context.Context, ws core.Workspace, agent string) (*core.Session, error) {
	if err := e.exec.SpawnSession(ctx, ws, agent); err != nil {
		return nil, err
	}
	return e.store.GetActiveSession(ctx, ws.ID)
}

func (e *executorSessionManager) ClearSession(ctx context.Context, session core.Session) (*core.Session, error) {
	ws, err := e.store.GetWorkspaceByID(ctx, session.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("getting workspace: %w", err)
	}
	if err := e.exec.KillSession(ctx, ws.ID, session.Agent); err != nil {
		return nil, err
	}
	if err := e.exec.SpawnSession(ctx, *ws, session.Agent); err != nil {
		return nil, err
	}
	return e.store.GetActiveSession(ctx, ws.ID)
}

func (e *executorSessionManager) KillSession(ctx context.Context, sessionID int64) error {
	sess, err := e.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return err
	}
	return e.exec.KillSession(ctx, sess.WorkspaceID, sess.Agent)
}

func (e *executorSessionManager) Send(ctx context.Context, sessionID int64, message string) error {
	sess, err := e.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return err
	}
	ws, err := e.store.GetWorkspaceByID(ctx, sess.WorkspaceID)
	if err != nil {
		return err
	}
	_, err = e.exec.Execute(ctx, *ws, sess.Agent, message)
	return err
}

var _ mcppkg.SessionManager = (*executorSessionManager)(nil)
