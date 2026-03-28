package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// GetUserContext returns the user context for a given sender and channel.
func (s *Store) GetUserContext(ctx context.Context, senderID, channel string) (*core.UserContext, error) {
	var uc core.UserContext
	var updatedAt string
	var activeSessionID sql.NullInt64

	err := s.db.QueryRowContext(ctx,
		`SELECT sender_id, channel, active_workspace_id, active_session_id, updated_at
		 FROM user_context WHERE sender_id = ? AND channel = ?`,
		senderID, channel,
	).Scan(&uc.SenderID, &uc.Channel, &uc.ActiveWorkspaceID, &activeSessionID, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user context sender=%q channel=%q: %w", senderID, channel, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting user context: %w", err)
	}

	if activeSessionID.Valid {
		uc.ActiveSessionID = &activeSessionID.Int64
	}
	uc.UpdatedAt = parseTime(updatedAt)
	return &uc, nil
}

// SetActiveWorkspace upserts the active workspace for a sender/channel pair.
// Also clears the active_session_id since switching workspaces invalidates the session.
func (s *Store) SetActiveWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_context (sender_id, channel, active_workspace_id, active_session_id, updated_at)
		 VALUES (?, ?, ?, NULL, datetime('now'))
		 ON CONFLICT(sender_id, channel) DO UPDATE SET 
		   active_workspace_id = excluded.active_workspace_id,
		   active_session_id = NULL,
		   updated_at = datetime('now')`,
		senderID, channel, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("setting active workspace: %w", err)
	}
	return nil
}

// SetActiveSession sets the active session for a sender/channel pair.
func (s *Store) SetActiveSession(ctx context.Context, senderID, channel string, sessionID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_context (sender_id, channel, active_workspace_id, active_session_id, updated_at)
		 VALUES (?, ?, 0, ?, datetime('now'))
		 ON CONFLICT(sender_id, channel) DO UPDATE SET 
		   active_session_id = excluded.active_session_id,
		   updated_at = datetime('now')`,
		senderID, channel, sessionID,
	)
	if err != nil {
		return fmt.Errorf("setting active session: %w", err)
	}
	return nil
}

// ClearActiveSession clears the active session for a sender/channel pair.
func (s *Store) ClearActiveSession(ctx context.Context, senderID, channel string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_context SET active_session_id = NULL, updated_at = datetime('now')
		 WHERE sender_id = ? AND channel = ?`,
		senderID, channel,
	)
	if err != nil {
		return fmt.Errorf("clearing active session: %w", err)
	}
	return nil
}

// GetUserContextBySession returns the user context that has the given session as active.
// Used by the watcher for reverse lookup.
func (s *Store) GetUserContextBySession(ctx context.Context, sessionID int64) (*core.UserContext, error) {
	var uc core.UserContext
	var updatedAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT sender_id, channel, active_workspace_id, active_session_id, updated_at
		 FROM user_context WHERE active_session_id = ?`,
		sessionID,
	).Scan(&uc.SenderID, &uc.Channel, &uc.ActiveWorkspaceID, &uc.ActiveSessionID, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user context for session %d: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting user context by session: %w", err)
	}

	uc.UpdatedAt = parseTime(updatedAt)
	return &uc, nil
}
