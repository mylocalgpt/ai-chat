package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// CreateSession inserts a new session and returns it with server-set defaults.
func (s *Store) CreateSession(ctx context.Context, workspaceID int64, agent, tmuxSession string) (*core.Session, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (workspace_id, agent, tmux_session) VALUES (?, ?, ?)`,
		workspaceID, agent, tmuxSession,
	)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting session id: %w", err)
	}

	return s.getSessionByID(ctx, id)
}

// GetActiveSession returns the most recent active session for a workspace.
func (s *Store) GetActiveSession(ctx context.Context, workspaceID int64) (*core.Session, error) {
	sess, err := s.scanSession(s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, agent, tmux_session, status, started_at, last_activity
		 FROM sessions WHERE workspace_id = ? AND status = 'active'
		 ORDER BY started_at DESC LIMIT 1`,
		workspaceID,
	))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("active session for workspace %d: %w", workspaceID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting active session: %w", err)
	}
	return sess, nil
}

// UpdateSessionStatus changes the status of a session.
func (s *Store) UpdateSessionStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET status = ? WHERE id = ?`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("updating session status id=%d: %w", id, err)
	}
	return nil
}

// TouchSession updates the last_activity timestamp to now.
func (s *Store) TouchSession(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_activity = datetime('now') WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("touching session id=%d: %w", id, err)
	}
	return nil
}

// ListSessions returns all sessions ordered by last_activity descending (nulls last),
// then by started_at descending. Returns an empty slice if none.
func (s *Store) ListSessions(ctx context.Context) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, agent, tmux_session, status, started_at, last_activity
		 FROM sessions ORDER BY last_activity DESC NULLS LAST, started_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	defer rows.Close()

	sessions := []core.Session{}
	for rows.Next() {
		var sess core.Session
		var tmuxSession sql.NullString
		var startedAt string
		var lastActivity sql.NullString

		err := rows.Scan(
			&sess.ID, &sess.WorkspaceID, &sess.Agent, &tmuxSession,
			&sess.Status, &startedAt, &lastActivity,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}

		if tmuxSession.Valid {
			sess.TmuxSession = tmuxSession.String
		}
		sess.StartedAt = parseTime(startedAt)
		if lastActivity.Valid {
			sess.LastActivity = parseTime(lastActivity.String)
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) getSessionByID(ctx context.Context, id int64) (*core.Session, error) {
	sess, err := s.scanSession(s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, agent, tmux_session, status, started_at, last_activity
		 FROM sessions WHERE id = ?`, id,
	))
	if err != nil {
		return nil, fmt.Errorf("getting session id=%d: %w", id, err)
	}
	return sess, nil
}

func (s *Store) scanSession(row *sql.Row) (*core.Session, error) {
	var sess core.Session
	var tmuxSession sql.NullString
	var startedAt string
	var lastActivity sql.NullString

	err := row.Scan(
		&sess.ID, &sess.WorkspaceID, &sess.Agent, &tmuxSession,
		&sess.Status, &startedAt, &lastActivity,
	)
	if err != nil {
		return nil, err
	}

	if tmuxSession.Valid {
		sess.TmuxSession = tmuxSession.String
	}
	sess.StartedAt = parseTime(startedAt)
	if lastActivity.Valid {
		sess.LastActivity = parseTime(lastActivity.String)
	}
	return &sess, nil
}
