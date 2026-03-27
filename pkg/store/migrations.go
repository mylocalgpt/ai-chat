package store

import (
	"database/sql"
	"fmt"
	"strconv"
)

// migrations is a sequential list of schema migration functions.
// Each runs in its own transaction. Append new migrations to the end.
var migrations = []func(*sql.Tx) error{
	migration001,
	migration002,
}

// Migrate runs all pending migrations against the database.
func Migrate(db *sql.DB) error {
	// Ensure the meta table exists.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _meta (
		key TEXT PRIMARY KEY,
		value TEXT
	)`); err != nil {
		return fmt.Errorf("creating _meta table: %w", err)
	}

	current, err := schemaVersion(db)
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	for i := current; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration %d: %w", i+1, err)
		}

		if err := migrations[i](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO _meta (key, value) VALUES ('schema_version', ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			strconv.Itoa(i+1),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("updating schema version to %d: %w", i+1, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", i+1, err)
		}
	}

	return nil
}

// schemaVersion reads the current schema version from the _meta table.
// Returns 0 if no version is recorded.
func schemaVersion(db *sql.DB) (int, error) {
	var val string
	err := db.QueryRow(`SELECT value FROM _meta WHERE key = 'schema_version'`).Scan(&val)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid schema version %q: %w", val, err)
	}
	return v, nil
}

// migration001 creates the initial schema: workspaces, messages, sessions, user_context.
func migration001(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE workspaces (
			id INTEGER PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			path TEXT NOT NULL,
			host TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,

		`CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			channel TEXT NOT NULL,
			channel_msg_id TEXT,
			sender_id TEXT NOT NULL,
			workspace_id INTEGER REFERENCES workspaces(id),
			content TEXT NOT NULL,
			direction TEXT NOT NULL,
			status TEXT DEFAULT 'pending',
			created_at TEXT DEFAULT (datetime('now'))
		)`,

		`CREATE INDEX idx_messages_channel_sender ON messages(channel, sender_id)`,

		`CREATE TABLE sessions (
			id INTEGER PRIMARY KEY,
			workspace_id INTEGER REFERENCES workspaces(id),
			agent TEXT NOT NULL,
			tmux_session TEXT,
			status TEXT DEFAULT 'active',
			started_at TEXT DEFAULT (datetime('now')),
			last_activity TEXT
		)`,

		`CREATE INDEX idx_sessions_workspace_status ON sessions(workspace_id, status)`,

		`CREATE TABLE user_context (
			sender_id TEXT NOT NULL,
			channel TEXT NOT NULL,
			active_workspace_id INTEGER REFERENCES workspaces(id),
			updated_at TEXT DEFAULT (datetime('now')),
			PRIMARY KEY (sender_id, channel)
		)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:40], err)
		}
	}
	return nil
}

// migration002 creates the model_config table for runtime model configuration.
func migration002(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE model_config (
		role TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		metadata TEXT DEFAULT '{}',
		updated_at TEXT DEFAULT (datetime('now'))
	)`)
	return err
}
