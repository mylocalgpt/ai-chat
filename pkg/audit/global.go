package audit

import (
	"fmt"
	"sync"
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

// resetGlobal resets the global state for testing. Not exported.
func resetGlobal() {
	if defaultLogger != nil {
		_ = defaultLogger.Close()
		defaultLogger = nil
	}
	initOnce = sync.Once{}
	initErr = nil
}
