// Package audit provides structured JSONL logging for all system events.
// Every event (inbound message, routing decision, agent call, outbound reply,
// error, health check) is recorded as one JSON object per line, one file per
// day. Files live in a configurable directory and are named YYYY-MM-DD.jsonl
// using UTC dates.
//
// Integration contract:
//   - Part 2 (Telegram): logs Inbound on message receive, Outbound on message send
//   - Part 3 (Orchestrator): logs Route on every routing decision, calls RunScheduledCheck
//   - Part 4 (Executor): logs AgentSend when forwarding to agent, AgentResponse on reply,
//     Error with Status "crashed" on session failures
//   - Part 5 (MCP): logs Health entries on health checks
//
// Call audit.Init() in main before starting any subsystem.
// Call audit.CloseGlobal() on shutdown.
package audit

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	done        chan struct{}
	closeOnce   sync.Once
}

// NewAuditLogger creates an AuditLogger that writes to dir. If dir does not
// exist it is created. retainDays controls how long old files are kept; old
// files are cleaned up on startup and once per day via a background goroutine.
func NewAuditLogger(dir string, retainDays int) (*AuditLogger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: create log dir: %w", err)
	}

	// Run startup cleanup.
	if count, err := RotateOldLogs(dir, retainDays); err != nil {
		slog.Error("audit: startup rotation failed", "error", err)
	} else if count > 0 {
		slog.Info("audit: cleaned up old log files", "count", count)
	}

	a := &AuditLogger{
		dir:        dir,
		retainDays: retainDays,
		nowFunc:    time.Now,
		done:       make(chan struct{}),
	}

	if err := a.openToday(); err != nil {
		return nil, err
	}

	go a.backgroundRotation()

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

// Close stops the background rotation goroutine and closes the file handle.
// It is safe to call multiple times.
func (a *AuditLogger) Close() error {
	var closeErr error
	a.closeOnce.Do(func() {
		close(a.done)

		a.mu.Lock()
		defer a.mu.Unlock()

		if a.current != nil {
			closeErr = a.current.Close()
			a.current = nil
		}
	})
	return closeErr
}

// rotate closes the current file and opens a new one for today's date.
func (a *AuditLogger) rotate() error {
	if a.current != nil {
		_ = a.current.Close()
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

// backgroundRotation runs daily log cleanup in the background. It fires at
// the next UTC midnight and then every 24 hours. Errors are logged but never
// cause a crash.
func (a *AuditLogger) backgroundRotation() {
	now := a.now().UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	timer := time.NewTimer(nextMidnight.Sub(now))
	defer timer.Stop()

	for {
		select {
		case <-a.done:
			return
		case <-timer.C:
			if count, err := RotateOldLogs(a.dir, a.retainDays); err != nil {
				slog.Error("audit: rotation failed", "error", err)
			} else if count > 0 {
				slog.Info("audit: cleaned up old log files", "count", count)
			}
			timer.Reset(24 * time.Hour)
		}
	}
}
