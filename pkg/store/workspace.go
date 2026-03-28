package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// CreateWorkspace inserts a new workspace and returns it with server-set defaults.
func (s *Store) CreateWorkspace(ctx context.Context, name, path, host string) (*core.Workspace, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO workspaces (name, path, host) VALUES (?, ?, ?)`,
		name, path, host,
	)
	if err != nil {
		return nil, fmt.Errorf("creating workspace %q: %w", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting workspace id: %w", err)
	}
	return s.GetWorkspaceByID(ctx, id)
}

// GetWorkspace fetches a workspace by name.
func (s *Store) GetWorkspace(ctx context.Context, name string) (*core.Workspace, error) {
	w, err := s.scanWorkspace(s.db.QueryRowContext(ctx,
		`SELECT id, name, path, host, metadata, created_at, updated_at
		 FROM workspaces WHERE name = ?`, name,
	))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting workspace %q: %w", name, err)
	}
	return w, nil
}

// GetWorkspaceByID fetches a workspace by id.
func (s *Store) GetWorkspaceByID(ctx context.Context, id int64) (*core.Workspace, error) {
	w, err := s.scanWorkspace(s.db.QueryRowContext(ctx,
		`SELECT id, name, path, host, metadata, created_at, updated_at
		 FROM workspaces WHERE id = ?`, id,
	))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace id=%d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting workspace id=%d: %w", id, err)
	}
	return w, nil
}

// ListWorkspaces returns all workspaces ordered by name.
// Returns an empty slice (not nil) if there are no workspaces.
func (s *Store) ListWorkspaces(ctx context.Context) ([]core.Workspace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, path, host, metadata, created_at, updated_at
		 FROM workspaces ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()

	workspaces := []core.Workspace{}
	for rows.Next() {
		w, err := s.scanWorkspaceRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning workspace: %w", err)
		}
		workspaces = append(workspaces, *w)
	}
	return workspaces, rows.Err()
}

// UpdateWorkspaceMetadata updates the metadata JSON and updated_at timestamp.
func (s *Store) UpdateWorkspaceMetadata(ctx context.Context, id int64, metadata json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workspaces SET metadata = ?, updated_at = datetime('now') WHERE id = ?`,
		string(metadata), id,
	)
	if err != nil {
		return fmt.Errorf("updating workspace metadata id=%d: %w", id, err)
	}
	return nil
}

// RenameWorkspace updates a workspace's name.
func (s *Store) RenameWorkspace(ctx context.Context, id int64, newName string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workspaces SET name = ?, updated_at = datetime('now') WHERE id = ?`,
		newName, id,
	)
	if err != nil {
		return fmt.Errorf("renaming workspace id=%d to %q: %w", id, newName, err)
	}
	return nil
}

// DeleteWorkspace removes a workspace by id.
func (s *Store) DeleteWorkspace(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting workspace id=%d: %w", id, err)
	}
	return nil
}

// FindWorkspaceByAlias searches for a workspace whose metadata contains the given alias.
func (s *Store) FindWorkspaceByAlias(ctx context.Context, alias string) (*core.Workspace, error) {
	w, err := s.scanWorkspace(s.db.QueryRowContext(ctx,
		`SELECT w.id, w.name, w.path, w.host, w.metadata, w.created_at, w.updated_at
		 FROM workspaces w, json_each(json_extract(w.metadata, '$.aliases')) je
		 WHERE je.value = ?`, alias,
	))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace alias %q: %w", alias, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("finding workspace by alias %q: %w", alias, err)
	}
	return w, nil
}

// GetWorkspaceByName fetches a workspace by name. Alias for GetWorkspace.
func (s *Store) GetWorkspaceByName(ctx context.Context, name string) (*core.Workspace, error) {
	return s.GetWorkspace(ctx, name)
}

// GetWorkspaceByAlias fetches a workspace by alias. Alias for FindWorkspaceByAlias.
func (s *Store) GetWorkspaceByAlias(ctx context.Context, alias string) (*core.Workspace, error) {
	return s.FindWorkspaceByAlias(ctx, alias)
}

// scanWorkspace scans a single row into a Workspace.
func (s *Store) scanWorkspace(row *sql.Row) (*core.Workspace, error) {
	var w core.Workspace
	var metadata string
	var createdAt, updatedAt string

	err := row.Scan(&w.ID, &w.Name, &w.Path, &w.Host, &metadata, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	w.Metadata = json.RawMessage(metadata)
	w.CreatedAt = parseTime(createdAt)
	w.UpdatedAt = parseTime(updatedAt)
	return &w, nil
}

// scanWorkspaceRow scans a rows iterator into a Workspace.
func (s *Store) scanWorkspaceRow(rows *sql.Rows) (*core.Workspace, error) {
	var w core.Workspace
	var metadata string
	var createdAt, updatedAt string

	err := rows.Scan(&w.ID, &w.Name, &w.Path, &w.Host, &metadata, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	w.Metadata = json.RawMessage(metadata)
	w.CreatedAt = parseTime(createdAt)
	w.UpdatedAt = parseTime(updatedAt)
	return &w, nil
}

// parseTime parses a SQLite UTC timestamp string.
func parseTime(s string) time.Time {
	t, _ := time.Parse(time.DateTime, s)
	return t
}
