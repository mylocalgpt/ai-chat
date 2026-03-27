// Package audit provides structured JSONL logging for system events.
// Every event (inbound message, routing decision, agent call, outbound reply,
// error, health check) is recorded as one JSON object per line, one file per
// day. Files live in a configurable directory and are named YYYY-MM-DD.jsonl
// using UTC dates.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// AuditEntry represents a single audit event written as one JSON line.
type AuditEntry struct {
	Timestamp  time.Time `json:"ts"`
	Type       string    `json:"type"`                  // inbound, route, agent_send, agent_response, outbound, error, health
	Channel    string    `json:"channel,omitempty"`      // e.g. "telegram", "web"
	Sender     string    `json:"sender,omitempty"`       // who sent the message
	Recipient  string    `json:"recipient,omitempty"`    // for outbound messages
	Workspace  string    `json:"workspace,omitempty"`    // workspace name or alias
	Agent      string    `json:"agent,omitempty"`        // agent that handled the request
	Session    string    `json:"session,omitempty"`      // session identifier
	Action     string    `json:"action,omitempty"`       // for route entries e.g. "agent_task"
	Content    string    `json:"content,omitempty"`      // message content
	Message    string    `json:"message,omitempty"`      // for agent_send entries
	Error      string    `json:"error,omitempty"`        // error description
	Status     string    `json:"status,omitempty"`       // e.g. "crashed", "active"
	Duration   int64     `json:"duration_ms,omitempty"`  // milliseconds
	Length     int       `json:"length,omitempty"`       // response length
	Confidence float64   `json:"confidence,omitempty"`   // routing confidence
	Extra      any       `json:"extra,omitempty"`        // arbitrary additional data
}

// AuditLogger writes AuditEntry values as JSONL to daily-rotated files.
type AuditLogger struct {
	dir         string
	retainDays  int
	mu          sync.Mutex
	current     *os.File
	currentDate string
	nowFunc     func() time.Time // defaults to time.Now, injectable for tests
}

// NewAuditLogger creates an AuditLogger that writes to dir. If dir does not
// exist it is created. retainDays controls how long old files are kept (used
// by the rotation logic added in Phase 2).
func NewAuditLogger(dir string, retainDays int) (*AuditLogger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: create log dir: %w", err)
	}

	a := &AuditLogger{
		dir:        dir,
		retainDays: retainDays,
		nowFunc:    time.Now,
	}

	if err := a.openToday(); err != nil {
		return nil, err
	}

	return a, nil
}

// Log writes an entry to the current day's JSONL file. If the date has
// changed since the last write, it rotates to a new file first.
func (a *AuditLogger) Log(entry AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	today := a.now().UTC().Format("2006-01-02")
	if today != a.currentDate {
		if err := a.rotate(); err != nil {
			return err
		}
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: marshal entry: %w", err)
	}

	data = append(data, '\n')
	if _, err := a.current.Write(data); err != nil {
		return fmt.Errorf("audit: write entry: %w", err)
	}

	return nil
}

// Close closes the underlying file handle.
func (a *AuditLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.current != nil {
		return a.current.Close()
	}
	return nil
}

// rotate closes the current file and opens a new one for today's date.
func (a *AuditLogger) rotate() error {
	if a.current != nil {
		a.current.Close()
	}
	return a.openToday()
}

// openToday opens the log file for today's UTC date.
func (a *AuditLogger) openToday() error {
	today := a.now().UTC().Format("2006-01-02")
	path := a.dir + "/" + today + ".jsonl"

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit: open log file %s: %w", path, err)
	}

	a.current = f
	a.currentDate = today
	return nil
}

// now returns the current time, using nowFunc for testability.
func (a *AuditLogger) now() time.Time {
	if a.nowFunc != nil {
		return a.nowFunc()
	}
	return time.Now()
}
