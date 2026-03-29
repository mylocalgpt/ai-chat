package store

import (
	"path/filepath"
	"testing"
)

func TestOpenInMemory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer func() { _ = db.Close() }()
}

func TestMigrateCreatesTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	tables := []string{"workspaces", "messages", "sessions", "active_workspaces", "active_workspace_sessions", "_meta"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}

	// Verify version matches total migration count.
	ver, err := schemaVersion(db)
	if err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	if ver != len(migrations) {
		t.Errorf("schema_version = %d, want %d", ver, len(migrations))
	}
}

func TestWALMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("querying journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("querying foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestIndexesCreated(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	indexes := []string{"idx_messages_channel_sender", "idx_sessions_workspace_status", "idx_sessions_workspace_slug", "idx_active_workspace_sessions_session"}
	for _, idx := range indexes {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q not found: %v", idx, err)
		}
	}
}

func TestMigrateResetsLegacySchemaWithoutActiveStateTables(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	legacyStmts := []string{
		`CREATE TABLE _meta (key TEXT PRIMARY KEY, value TEXT)`,
		`INSERT INTO _meta (key, value) VALUES ('schema_version', '3')`,
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
		`CREATE TABLE sessions (
			id INTEGER PRIMARY KEY,
			workspace_id INTEGER REFERENCES workspaces(id),
			agent TEXT NOT NULL,
			tmux_session TEXT,
			status TEXT DEFAULT 'active',
			started_at TEXT DEFAULT (datetime('now')),
			last_activity TEXT,
			slug TEXT NOT NULL DEFAULT '',
			agent_session_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE user_context (
			sender_id TEXT NOT NULL,
			channel TEXT NOT NULL,
			active_workspace_id INTEGER REFERENCES workspaces(id),
			updated_at TEXT DEFAULT (datetime('now')),
			active_session_id INTEGER REFERENCES sessions(id),
			PRIMARY KEY (sender_id, channel)
		)`,
		`CREATE TABLE model_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}

	for _, stmt := range legacyStmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("executing legacy stmt %q: %v", stmt, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	tables := []string{"workspaces", "messages", "sessions", "active_workspaces", "active_workspace_sessions", "_meta"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found after reset: %v", table, err)
		}
	}

	for _, removed := range []string{"user_context", "model_config"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, removed).Scan(&name)
		if err == nil {
			t.Errorf("legacy table %q still exists after reset", removed)
		}
	}

	ver, err := schemaVersion(db)
	if err != nil {
		t.Fatalf("reading schema version after reset: %v", err)
	}
	if ver != len(migrations) {
		t.Fatalf("schema_version = %d, want %d", ver, len(migrations))
	}
}
