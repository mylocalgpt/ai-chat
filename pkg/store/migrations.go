package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// migrations defines the fresh schema bootstrap for resettable databases.
var migrations = []func(*sql.Tx) error{
	migration001,
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

	needsReset, err := needsSchemaReset(db, current)
	if err != nil {
		return fmt.Errorf("checking schema compatibility: %w", err)
	}
	if needsReset {
		if err := resetSchema(db); err != nil {
			return fmt.Errorf("resetting incompatible schema: %w", err)
		}
		current = 0
	}

	for i := current; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration %d: %w", i+1, err)
		}

		if err := migrations[i](tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO _meta (key, value) VALUES ('schema_version', ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			strconv.Itoa(i+1),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("updating schema version to %d: %w", i+1, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", i+1, err)
		}
	}

	return nil
}

var requiredRuntimeTables = []string{
	"workspaces",
	"messages",
	"sessions",
	"active_workspaces",
	"active_workspace_sessions",
}

func needsSchemaReset(db *sql.DB, current int) (bool, error) {
	if current > len(migrations) {
		return true, nil
	}

	tables, err := listTables(db)
	if err != nil {
		return false, err
	}

	haveTable := make(map[string]bool, len(tables))
	haveNonMetaTables := false
	for _, table := range tables {
		haveTable[table] = true
		if table != "_meta" {
			haveNonMetaTables = true
		}
	}

	missingRequired := false
	for _, table := range requiredRuntimeTables {
		if !haveTable[table] {
			missingRequired = true
			break
		}
	}

	if !missingRequired {
		return false, nil
	}

	// A brand new database only has _meta at this point and should run the
	// current bootstrap normally. Any populated but incomplete app schema is
	// treated as incompatible and rebuilt from scratch.
	return haveNonMetaTables, nil
}

func resetSchema(db *sql.DB) error {
	tables, err := listTables(db)
	if err != nil {
		return err
	}

	ordered := orderTablesForDrop(tables)
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning schema reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range ordered {
		if _, err := tx.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, escapeIdent(table))); err != nil {
			return fmt.Errorf("dropping table %q: %w", table, err)
		}
	}

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS _meta (
		key TEXT PRIMARY KEY,
		value TEXT
	)`); err != nil {
		return fmt.Errorf("recreating _meta table: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM _meta`); err != nil {
		return fmt.Errorf("clearing _meta table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing schema reset: %w", err)
	}

	return nil
}

func listTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning table name: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating table names: %w", err)
	}

	return tables, nil
}

func orderTablesForDrop(tables []string) []string {
	priority := map[string]int{
		"active_workspace_sessions": 0,
		"active_workspaces":         1,
		"messages":                  2,
		"sessions":                  3,
		"user_context":              4,
		"model_config":              5,
		"workspaces":                6,
		"_meta":                     7,
	}

	ordered := append([]string(nil), tables...)
	sort.Slice(ordered, func(i, j int) bool {
		pi, okI := priority[ordered[i]]
		pj, okJ := priority[ordered[j]]
		switch {
		case okI && okJ:
			if pi == pj {
				return ordered[i] < ordered[j]
			}
			return pi < pj
		case okI:
			return true
		case okJ:
			return false
		default:
			return ordered[i] < ordered[j]
		}
	})

	return ordered
}

func escapeIdent(name string) string {
	return strings.ReplaceAll(name, `"`, `""`)
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

// migration001 creates the reset schema used by the current runtime.
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
			slug TEXT NOT NULL,
			agent_session_id TEXT DEFAULT '',
			tmux_session TEXT,
			status TEXT DEFAULT 'active',
			started_at TEXT DEFAULT (datetime('now')),
			last_activity TEXT
		)`,

		`CREATE INDEX idx_sessions_workspace_status ON sessions(workspace_id, status)`,
		`CREATE UNIQUE INDEX idx_sessions_workspace_slug ON sessions(workspace_id, slug)`,

		`CREATE TABLE active_workspaces (
			sender_id TEXT NOT NULL,
			channel TEXT NOT NULL,
			workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
			updated_at TEXT DEFAULT (datetime('now')),
			PRIMARY KEY (sender_id, channel)
		)`,

		`CREATE TABLE active_workspace_sessions (
			sender_id TEXT NOT NULL,
			channel TEXT NOT NULL,
			workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
			session_id INTEGER NOT NULL REFERENCES sessions(id),
			updated_at TEXT DEFAULT (datetime('now')),
			PRIMARY KEY (sender_id, channel, workspace_id)
		)`,

		`CREATE UNIQUE INDEX idx_active_workspace_sessions_session ON active_workspace_sessions(session_id)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:40], err)
		}
	}
	return nil
}
