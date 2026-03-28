package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Open opens a SQLite database at dbPath with recommended pragmas.
// For file-based databases, WAL journal mode is enabled.
// For in-memory databases, pass ":memory:" (WAL is silently ignored).
func Open(dbPath string) (*sql.DB, error) {
	connStr := buildConnStr(dbPath)

	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// SQLite serializes writes; multiple connections just queue behind the WAL lock.
	db.SetMaxOpenConns(1)

	return db, nil
}

// buildConnStr constructs the SQLite connection string with pragmas.
func buildConnStr(dbPath string) string {
	if dbPath == ":memory:" {
		// WAL is silently ignored for in-memory databases.
		return "file::memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
	}

	var b strings.Builder
	b.WriteString("file:")
	b.WriteString(dbPath)
	b.WriteString("?_pragma=journal_mode(WAL)")
	b.WriteString("&_pragma=busy_timeout(5000)")
	b.WriteString("&_pragma=foreign_keys(ON)")
	b.WriteString("&_pragma=synchronous(NORMAL)")
	return b.String()
}
