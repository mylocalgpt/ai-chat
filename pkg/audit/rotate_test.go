package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotateOldLogs(t *testing.T) {
	dir := t.TempDir()

	// Create files spanning 28 days.
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 28; i++ {
		d := base.AddDate(0, 0, i)
		name := d.Format("2006-01-02") + ".jsonl"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"ts":"test"}`+"\n"), 0o644); err != nil {
			t.Fatalf("create file: %v", err)
		}
	}

	// Retain 7 days. Files older than 7 days from "today" (2026-03-28) should be deleted.
	count, err := RotateOldLogs(dir, 7)
	if err != nil {
		t.Fatalf("RotateOldLogs: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	// The cutoff is today - 7 days. Files from 2026-03-21 onward should remain.
	// That's 8 files: 03-21 through 03-28.
	remaining := len(entries)
	if count < 1 {
		t.Errorf("expected some files deleted, got count=%d", count)
	}
	if remaining+count != 28 {
		t.Errorf("remaining(%d) + deleted(%d) != 28", remaining, count)
	}

	// Verify remaining files are the recent ones.
	cutoff := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -7)
	for _, e := range entries {
		stem := e.Name()[:10]
		d, _ := time.Parse("2006-01-02", stem)
		if d.Before(cutoff) {
			t.Errorf("file %s should have been deleted (before cutoff %s)", e.Name(), cutoff.Format("2006-01-02"))
		}
	}
}

func TestRotateOldLogsLeavesNonMatching(t *testing.T) {
	dir := t.TempDir()

	// Create some non-matching files.
	nonMatching := []string{"notes.txt", "backup.jsonl", "2026-13-01.jsonl", "not-a-date.jsonl"}
	for _, name := range nonMatching {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatalf("create file: %v", err)
		}
	}

	// Also create an old matching file to verify deletion works.
	old := filepath.Join(dir, "2020-01-01.jsonl")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatalf("create old file: %v", err)
	}

	count, err := RotateOldLogs(dir, 7)
	if err != nil {
		t.Fatalf("RotateOldLogs: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 deleted, got %d", count)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != len(nonMatching) {
		t.Errorf("expected %d remaining files, got %d", len(nonMatching), len(entries))
	}
}

func TestRotateOldLogsZeroRetain(t *testing.T) {
	dir := t.TempDir()

	// Create a file that would be deleted with any positive retainDays.
	if err := os.WriteFile(filepath.Join(dir, "2020-01-01.jsonl"), []byte("old"), 0o644); err != nil {
		t.Fatalf("create file: %v", err)
	}

	count, err := RotateOldLogs(dir, 0)
	if err != nil {
		t.Fatalf("RotateOldLogs: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 deleted with retainDays=0, got %d", count)
	}

	// File should still exist.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file remaining, got %d", len(entries))
	}
}

func TestRotateOldLogsEmptyDir(t *testing.T) {
	dir := t.TempDir()

	count, err := RotateOldLogs(dir, 7)
	if err != nil {
		t.Fatalf("RotateOldLogs: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 deleted for empty dir, got %d", count)
	}
}

func TestRotateOldLogsInvalidDateSkipped(t *testing.T) {
	dir := t.TempDir()

	// Files with invalid date patterns in the name.
	invalid := []string{"abcd-ef-gh.jsonl", "2026-13-01.jsonl", "2026-00-15.jsonl"}
	for _, name := range invalid {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatalf("create file: %v", err)
		}
	}

	count, err := RotateOldLogs(dir, 7)
	if err != nil {
		t.Fatalf("RotateOldLogs: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 deleted for invalid files, got %d", count)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != len(invalid) {
		t.Errorf("expected %d files remaining, got %d", len(invalid), len(entries))
	}
}
