package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func (s *Store) GetActiveWorkspace(ctx context.Context, senderID, channel string) (*core.ActiveWorkspace, error) {
	var active core.ActiveWorkspace
	err := s.db.QueryRowContext(ctx,
		`SELECT sender_id, channel, workspace_id
		 FROM active_workspaces
		 WHERE sender_id = ? AND channel = ?`,
		senderID, channel,
	).Scan(&active.SenderID, &active.Channel, &active.WorkspaceID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("active workspace sender=%q channel=%q: %w", senderID, channel, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting active workspace: %w", err)
	}
	return &active, nil
}

func (s *Store) SetActiveWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO active_workspaces (sender_id, channel, workspace_id, updated_at)
		 VALUES (?, ?, ?, datetime('now'))
		 ON CONFLICT(sender_id, channel) DO UPDATE SET
		   workspace_id = excluded.workspace_id,
		   updated_at = datetime('now')`,
		senderID, channel, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("setting active workspace: %w", err)
	}
	return nil
}

func (s *Store) GetActiveSessionForWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) (*core.ActiveWorkspaceSession, error) {
	var active core.ActiveWorkspaceSession
	err := s.db.QueryRowContext(ctx,
		`SELECT sender_id, channel, workspace_id, session_id
		 FROM active_workspace_sessions
		 WHERE sender_id = ? AND channel = ? AND workspace_id = ?`,
		senderID, channel, workspaceID,
	).Scan(&active.SenderID, &active.Channel, &active.WorkspaceID, &active.SessionID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("active session sender=%q channel=%q workspace=%d: %w", senderID, channel, workspaceID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting active session mapping: %w", err)
	}
	return &active, nil
}

func (s *Store) SetActiveSessionForWorkspace(ctx context.Context, senderID, channel string, workspaceID, sessionID int64) error {
	sess, err := s.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("getting session for active session update: %w", err)
	}
	if sess.WorkspaceID != workspaceID {
		return fmt.Errorf("setting active session: session %d is not in workspace %d", sessionID, workspaceID)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning active session update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM active_workspace_sessions
		 WHERE session_id = ?
		   AND NOT (sender_id = ? AND channel = ? AND workspace_id = ?)`,
		sessionID, senderID, channel, workspaceID,
	); err != nil {
		return fmt.Errorf("clearing prior session owner: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO active_workspace_sessions (sender_id, channel, workspace_id, session_id, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(sender_id, channel, workspace_id) DO UPDATE SET
		   session_id = excluded.session_id,
		   updated_at = datetime('now')`,
		senderID, channel, workspaceID, sessionID,
	); err != nil {
		return fmt.Errorf("setting active session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing active session update: %w", err)
	}
	return nil
}

func (s *Store) ClearActiveSessionForWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM active_workspace_sessions
		 WHERE sender_id = ? AND channel = ? AND workspace_id = ?`,
		senderID, channel, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("clearing active session: %w", err)
	}
	return nil
}

func (s *Store) ListActiveWorkspaceSessions(ctx context.Context, senderID, channel string) ([]core.ActiveWorkspaceSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sender_id, channel, workspace_id, session_id
		 FROM active_workspace_sessions
		 WHERE sender_id = ? AND channel = ?
		 ORDER BY workspace_id`,
		senderID, channel,
	)
	if err != nil {
		return nil, fmt.Errorf("listing active session mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	mappings := []core.ActiveWorkspaceSession{}
	for rows.Next() {
		var active core.ActiveWorkspaceSession
		if err := rows.Scan(&active.SenderID, &active.Channel, &active.WorkspaceID, &active.SessionID); err != nil {
			return nil, fmt.Errorf("scanning active session mapping: %w", err)
		}
		mappings = append(mappings, active)
	}
	return mappings, rows.Err()
}

func (s *Store) GetActiveWorkspaceSessionBySessionID(ctx context.Context, sessionID int64) (*core.ActiveWorkspaceSession, error) {
	var active core.ActiveWorkspaceSession
	err := s.db.QueryRowContext(ctx,
		`SELECT sender_id, channel, workspace_id, session_id
		 FROM active_workspace_sessions
		 WHERE session_id = ?`,
		sessionID,
	).Scan(&active.SenderID, &active.Channel, &active.WorkspaceID, &active.SessionID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("active session mapping for session %d: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting active session mapping by session id: %w", err)
	}
	return &active, nil
}
