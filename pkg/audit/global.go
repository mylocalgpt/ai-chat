package audit

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	defaultLogger *AuditLogger
	initOnce      sync.Once
	initErr       error
)

// Init initializes the global audit logger. It is safe to call multiple times;
// only the first call takes effect.
func Init(dir string, retainDays int) error {
	initOnce.Do(func() {
		defaultLogger, initErr = NewAuditLogger(dir, retainDays)
	})
	return initErr
}

// LogGlobal writes an entry using the global audit logger.
// Returns an error if Init has not been called.
func LogGlobal(entry AuditEntry) error {
	if defaultLogger == nil {
		return fmt.Errorf("audit: not initialized")
	}
	return defaultLogger.Log(entry)
}

// CloseGlobal closes the global audit logger and stops its background rotation.
func CloseGlobal() error {
	if defaultLogger == nil {
		return nil
	}
	return defaultLogger.Close()
}

// OpenDailyLog opens (or creates) a date-stamped log file in dir for use as
// an slog output destination. The file is named YYYY-MM-DD.log and opened in
// append mode. Caller is responsible for closing it on shutdown.
func OpenDailyLog(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: create log dir: %w", err)
	}
	name := time.Now().UTC().Format("2006-01-02") + ".log"
	return os.OpenFile(dir+"/"+name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

// resetGlobal resets the global state for testing. Not exported.
func resetGlobal() {
	if defaultLogger != nil {
		_ = defaultLogger.Close()
		defaultLogger = nil
	}
	initOnce = sync.Once{}
	initErr = nil
}
