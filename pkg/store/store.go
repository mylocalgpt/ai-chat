package store

import (
	"database/sql"
	"errors"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps a database connection and provides typed CRUD operations.
type Store struct {
	db *sql.DB
}

// New creates a Store wrapping the given database connection.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
