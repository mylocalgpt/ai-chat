package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// CreateSession inserts a new session and returns it with server-set defaults.
func (s *Store) CreateSession(ctx context.Context, workspaceID int64, agent, slug, tmuxSession string) (*core.Session, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (workspace_id, agent, slug, tmux_session) VALUES (?, ?, ?, ?)`,
		workspaceID, agent, slug, tmuxSession,
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
		`SELECT id, workspace_id, agent, slug, agent_session_id, tmux_session, status, started_at, last_activity
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
		`SELECT id, workspace_id, agent, slug, agent_session_id, tmux_session, status, started_at, last_activity
		 FROM sessions ORDER BY last_activity DESC NULLS LAST, started_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	sessions := []core.Session{}
	for rows.Next() {
		var sess core.Session
		var tmuxSession sql.NullString
		var startedAt string
		var lastActivity sql.NullString

		err := rows.Scan(
			&sess.ID, &sess.WorkspaceID, &sess.Agent, &sess.Slug, &sess.AgentSessionID,
			&tmuxSession, &sess.Status, &startedAt, &lastActivity,
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

// ListSessionsForWorkspace returns sessions for a workspace ordered by
// last_activity descending (nulls last), then by started_at descending.
func (s *Store) ListSessionsForWorkspace(ctx context.Context, workspaceID int64) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, agent, slug, agent_session_id, tmux_session, status, started_at, last_activity
		 FROM sessions WHERE workspace_id = ?
		 ORDER BY last_activity DESC NULLS LAST, started_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing sessions for workspace %d: %w", workspaceID, err)
	}
	defer func() { _ = rows.Close() }()

	sessions := []core.Session{}
	for rows.Next() {
		var sess core.Session
		var tmuxSession sql.NullString
		var startedAt string
		var lastActivity sql.NullString

		err := rows.Scan(
			&sess.ID, &sess.WorkspaceID, &sess.Agent, &sess.Slug, &sess.AgentSessionID,
			&tmuxSession, &sess.Status, &startedAt, &lastActivity,
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

// GetSessionBySlug finds a specific session by slug within a workspace.
func (s *Store) GetSessionBySlug(ctx context.Context, workspaceID int64, slug string) (*core.Session, error) {
	sess, err := s.scanSession(s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, agent, slug, agent_session_id, tmux_session, status, started_at, last_activity
		 FROM sessions WHERE workspace_id = ? AND slug = ?`,
		workspaceID, slug,
	))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session slug=%q workspace=%d: %w", slug, workspaceID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting session by slug: %w", err)
	}
	return sess, nil
}

func (s *Store) getSessionByID(ctx context.Context, id int64) (*core.Session, error) {
	sess, err := s.scanSession(s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, agent, slug, agent_session_id, tmux_session, status, started_at, last_activity
		 FROM sessions WHERE id = ?`, id,
	))
	if err != nil {
		return nil, fmt.Errorf("getting session id=%d: %w", id, err)
	}
	return sess, nil
}

// GetSessionByID fetches a session by its ID.
func (s *Store) GetSessionByID(ctx context.Context, id int64) (*core.Session, error) {
	return s.getSessionByID(ctx, id)
}

// ListActiveSessionsForWorkspace returns all active sessions for a workspace.
func (s *Store) ListActiveSessionsForWorkspace(ctx context.Context, workspaceID int64) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, agent, slug, agent_session_id, tmux_session, status, started_at, last_activity
		 FROM sessions WHERE workspace_id = ? AND status = 'active'
		 ORDER BY started_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing active sessions for workspace %d: %w", workspaceID, err)
	}
	defer func() { _ = rows.Close() }()

	sessions := []core.Session{}
	for rows.Next() {
		var sess core.Session
		var tmuxSession sql.NullString
		var startedAt string
		var lastActivity sql.NullString

		err := rows.Scan(
			&sess.ID, &sess.WorkspaceID, &sess.Agent, &sess.Slug, &sess.AgentSessionID,
			&tmuxSession, &sess.Status, &startedAt, &lastActivity,
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

// CountActiveSessionsForWorkspace returns the count of active sessions for a workspace.
func (s *Store) CountActiveSessionsForWorkspace(ctx context.Context, workspaceID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE workspace_id = ? AND status = 'active'`,
		workspaceID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting active sessions for workspace %d: %w", workspaceID, err)
	}
	return count, nil
}

// GetSessionByTmuxSession finds a session by its tmux session name.
func (s *Store) GetSessionByTmuxSession(ctx context.Context, tmuxSession string) (*core.Session, error) {
	sess, err := s.scanSession(s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, agent, slug, agent_session_id, tmux_session, status, started_at, last_activity
		 FROM sessions WHERE tmux_session = ?`,
		tmuxSession,
	))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session tmux_session=%q: %w", tmuxSession, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting session by tmux session: %w", err)
	}
	return sess, nil
}

func (s *Store) scanSession(row *sql.Row) (*core.Session, error) {
	var sess core.Session
	var tmuxSession sql.NullString
	var startedAt string
	var lastActivity sql.NullString

	err := row.Scan(
		&sess.ID, &sess.WorkspaceID, &sess.Agent, &sess.Slug, &sess.AgentSessionID,
		&tmuxSession, &sess.Status, &startedAt, &lastActivity,
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

func (s *Store) GetSessionByName(ctx context.Context, name string) (*core.Session, error) {
	workspace, slug := parseSessionName(name)
	if workspace == "" || slug == "" {
		return nil, fmt.Errorf("invalid session name format: %q", name)
	}

	ws, err := s.GetWorkspace(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("looking up workspace %q: %w", workspace, err)
	}

	return s.GetSessionBySlug(ctx, ws.ID, slug)
}

func parseSessionName(name string) (workspace, slug string) {
	const prefix = "ai-chat-"
	if !strings.HasPrefix(name, prefix) {
		return "", ""
	}
	parts := strings.TrimPrefix(name, prefix)
	idx := strings.LastIndex(parts, "-")
	if idx <= 0 {
		return "", ""
	}
	return parts[:idx], parts[idx+1:]
}
