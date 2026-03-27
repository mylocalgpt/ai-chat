package audit

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// RotateOldLogs deletes JSONL log files in logDir that are older than
// retainDays days (UTC). Files that do not match the YYYY-MM-DD.jsonl naming
// pattern are left alone. Returns the number of files deleted.
//
// If retainDays <= 0, no cleanup is performed (safe zero-value behavior).
func RotateOldLogs(logDir string, retainDays int) (int, error) {
	if retainDays <= 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return 0, fmt.Errorf("audit: read log dir: %w", err)
	}

	cutoff := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -retainDays)
	deleted := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		stem := strings.TrimSuffix(name, ".jsonl")
		fileDate, err := time.Parse("2006-01-02", stem)
		if err != nil {
			// Not a date-formatted file, skip it.
			continue
		}

		if fileDate.Before(cutoff) {
			path := logDir + "/" + name
			if err := os.Remove(path); err != nil {
				return deleted, fmt.Errorf("audit: remove old log %s: %w", path, err)
			}
			deleted++
		}
	}

	return deleted, nil
}
