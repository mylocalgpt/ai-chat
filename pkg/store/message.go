package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// CreateMessage inserts a message and sets msg.ID from the insert result.
func (s *Store) CreateMessage(ctx context.Context, msg *core.Message) error {
	var workspaceID any
	if msg.WorkspaceID != nil {
		workspaceID = *msg.WorkspaceID
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (channel, channel_msg_id, sender_id, workspace_id, content, direction, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msg.Channel, msg.ChannelMsgID, msg.SenderID, workspaceID,
		msg.Content, string(msg.Direction), string(msg.Status),
	)
	if err != nil {
		return fmt.Errorf("creating message: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting message id: %w", err)
	}
	msg.ID = id
	return nil
}

// GetPendingMessages returns all pending messages for the given channel,
// ordered by creation time ascending. Returns an empty slice if none.
func (s *Store) GetPendingMessages(ctx context.Context, channel string) ([]core.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel, channel_msg_id, sender_id, workspace_id, content, direction, status, created_at
		 FROM messages WHERE channel = ? AND status = 'pending'
		 ORDER BY created_at ASC`,
		channel,
	)
	if err != nil {
		return nil, fmt.Errorf("getting pending messages: %w", err)
	}
	defer rows.Close()

	messages := []core.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, *m)
	}
	return messages, rows.Err()
}

// ListMessages returns the most recent N messages for a workspace, ordered
// chronologically (oldest first). Returns an empty slice if none.
func (s *Store) ListMessages(ctx context.Context, workspaceID int64, limit int) ([]core.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel, channel_msg_id, sender_id, workspace_id, content, direction, status, created_at
		 FROM messages WHERE workspace_id = ?
		 ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing messages for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	messages := []core.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to chronological order (oldest first).
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// ListMessagesSince returns messages for a workspace created after the given
// timestamp, ordered chronologically. Capped at 200 results.
func (s *Store) ListMessagesSince(ctx context.Context, workspaceID int64, since time.Time) ([]core.Message, error) {
	sinceStr := since.UTC().Format(time.DateTime)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel, channel_msg_id, sender_id, workspace_id, content, direction, status, created_at
		 FROM messages WHERE workspace_id = ? AND created_at > ?
		 ORDER BY created_at ASC LIMIT 200`, workspaceID, sinceStr)
	if err != nil {
		return nil, fmt.Errorf("listing messages since %s for workspace %d: %w", sinceStr, workspaceID, err)
	}
	defer rows.Close()

	messages := []core.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, *m)
	}
	return messages, rows.Err()
}

// UpdateMessageStatus changes the status of a message.
func (s *Store) UpdateMessageStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE messages SET status = ? WHERE id = ?`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("updating message status id=%d: %w", id, err)
	}
	return nil
}

// scanMessage scans a row from a query into a Message.
func scanMessage(rows *sql.Rows) (*core.Message, error) {
	var m core.Message
	var channelMsgID sql.NullString
	var workspaceID sql.NullInt64
	var createdAt string

	err := rows.Scan(
		&m.ID, &m.Channel, &channelMsgID, &m.SenderID, &workspaceID,
		&m.Content, &m.Direction, &m.Status, &createdAt,
	)
	if err != nil {
		return nil, err
	}

	if channelMsgID.Valid {
		m.ChannelMsgID = channelMsgID.String
	}
	if workspaceID.Valid {
		wid := workspaceID.Int64
		m.WorkspaceID = &wid
	}
	m.CreatedAt = parseTime(createdAt)
	return &m, nil
}
