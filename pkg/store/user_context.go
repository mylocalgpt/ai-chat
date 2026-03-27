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

	err := s.db.QueryRowContext(ctx,
		`SELECT sender_id, channel, active_workspace_id, updated_at
		 FROM user_context WHERE sender_id = ? AND channel = ?`,
		senderID, channel,
	).Scan(&uc.SenderID, &uc.Channel, &uc.ActiveWorkspaceID, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user context sender=%q channel=%q: %w", senderID, channel, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting user context: %w", err)
	}

	uc.UpdatedAt = parseTime(updatedAt)
	return &uc, nil
}

// SetActiveWorkspace upserts the active workspace for a sender/channel pair.
func (s *Store) SetActiveWorkspace(ctx context.Context, senderID, channel string, workspaceID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_context (sender_id, channel, active_workspace_id, updated_at)
		 VALUES (?, ?, ?, datetime('now'))`,
		senderID, channel, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("setting active workspace: %w", err)
	}
	return nil
}
