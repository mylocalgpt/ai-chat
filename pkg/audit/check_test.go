package audit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestLog creates a JSONL file with the given lines in dir.
func writeTestLog(t *testing.T, dir, date string, lines []string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	path := filepath.Join(dir, date+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test log: %v", err)
	}
}

func TestRunAuditCheckPatternCounts(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	lines := []string{
		`{"ts":"2026-03-28T10:00:00Z","type":"error","error":"something broke"}`,
		`{"ts":"2026-03-28T10:01:00Z","type":"error","error":"another error"}`,
		`{"ts":"2026-03-28T10:02:00Z","type":"inbound","channel":"telegram"}`,
		`{"ts":"2026-03-28T10:03:00Z","type":"error","error":"third error"}`,
		`{"ts":"2026-03-28T10:04:00Z","status":"crashed","type":"health"}`,
		`{"ts":"2026-03-28T10:05:00Z","type":"route","confidence":0.3}`,
		`{"ts":"2026-03-28T10:06:00Z","type":"unauthorized"}`,
		`{"ts":"2026-03-28T10:07:00Z","type":"unauthorized"}`,
		`{"ts":"2026-03-28T10:08:00Z","message":"rate limit exceeded"}`,
	}
	writeTestLog(t, dir, today, lines)

	result, err := runAuditCheckInternal(dir, 1, true)
	if err != nil {
		t.Fatalf("RunAuditCheck: %v", err)
	}

	if result.TotalEntries != 9 {
		t.Errorf("TotalEntries = %d, want 9", result.TotalEntries)
	}

	expected := map[string]int{
		"errors":        3,
		"rate_limits":   1,
		"crashes":       1,
		"failed_routes": 1,
		"unauthorized":  2,
	}

	for _, p := range result.Patterns {
		want, ok := expected[p.Name]
		if !ok {
			continue
		}
		if p.Count != want {
			t.Errorf("pattern %s: count = %d, want %d", p.Name, p.Count, want)
		}
	}
}

func TestRunAuditCheckFlagged(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	// Create enough errors to exceed threshold (5).
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, `{"ts":"2026-03-28T10:00:00Z","type":"error","error":"err"}`)
	}
	writeTestLog(t, dir, today, lines)

	result, err := runAuditCheckInternal(dir, 1, true)
	if err != nil {
		t.Fatalf("RunAuditCheck: %v", err)
	}

	for _, p := range result.Patterns {
		if p.Name == "errors" {
			if !p.Flagged {
				t.Errorf("errors pattern should be flagged with count %d > threshold %d", p.Count, p.Threshold)
			}
			if len(p.Details) == 0 {
				t.Error("flagged pattern should have details")
			}
			if len(p.Details) > 3 {
				t.Errorf("details should have at most 3 entries, got %d", len(p.Details))
			}
		}
		if p.Name == "crashes" && p.Flagged {
			t.Error("crashes should not be flagged with 0 matches")
		}
	}
}

func TestRunAuditCheckStringFormat(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	lines := []string{
		`{"ts":"2026-03-28T10:00:00Z","type":"error","error":"something"}`,
		`{"ts":"2026-03-28T10:01:00Z","type":"inbound"}`,
	}
	writeTestLog(t, dir, today, lines)

	result, err := runAuditCheckInternal(dir, 1, true)
	if err != nil {
		t.Fatalf("RunAuditCheck: %v", err)
	}

	output := result.String()
	if !strings.Contains(output, "ai-chat audit check (last 24h):") {
		t.Errorf("missing header in output:\n%s", output)
	}
	if !strings.Contains(output, "errors:") {
		t.Errorf("missing errors pattern in output:\n%s", output)
	}
	if !strings.Contains(output, "(OK)") {
		t.Errorf("missing OK status in output:\n%s", output)
	}
}

func TestRunAuditCheckEmptyDir(t *testing.T) {
	dir := t.TempDir()

	result, err := runAuditCheckInternal(dir, 1, true)
	if err != nil {
		t.Fatalf("RunAuditCheck: %v", err)
	}

	if result.TotalEntries != 0 {
		t.Errorf("TotalEntries = %d, want 0", result.TotalEntries)
	}

	for _, p := range result.Patterns {
		if p.Count != 0 {
			t.Errorf("pattern %s: count = %d, want 0", p.Name, p.Count)
		}
		if p.Flagged {
			t.Errorf("pattern %s should not be flagged", p.Name)
		}
	}
}

func TestRunAuditCheckPartialLines(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	// Include a partial line (no newline, truncated JSON).
	content := `{"ts":"2026-03-28T10:00:00Z","type":"error","error":"real"}` + "\n" +
		`{"ts":"2026-03-28T10:01:00Z","type":"err` // truncated, no newline

	path := filepath.Join(dir, today+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runAuditCheckInternal(dir, 1, true)
	if err != nil {
		t.Fatalf("RunAuditCheck: %v", err)
	}

	// Should have counted the first complete line.
	found := false
	for _, p := range result.Patterns {
		if p.Name == "errors" && p.Count >= 1 {
			found = true
		}
	}
	if !found {
		t.Error("expected at least 1 error match from complete line")
	}
}

func TestRunAuditCheckWithRg(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}

	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	lines := []string{
		`{"ts":"2026-03-28T10:00:00Z","type":"error","error":"boom"}`,
		`{"ts":"2026-03-28T10:01:00Z","type":"error","error":"boom2"}`,
		`{"ts":"2026-03-28T10:02:00Z","type":"inbound"}`,
	}
	writeTestLog(t, dir, today, lines)

	// Test with rg (forceGoFallback = false).
	result, err := runAuditCheckInternal(dir, 1, false)
	if err != nil {
		t.Fatalf("RunAuditCheck with rg: %v", err)
	}

	for _, p := range result.Patterns {
		if p.Name == "errors" && p.Count != 2 {
			t.Errorf("rg path: errors count = %d, want 2", p.Count)
		}
	}
}

func TestRunAuditCheckMultiDay(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC()
	yesterday := today.AddDate(0, 0, -1)

	writeTestLog(t, dir, today.Format("2006-01-02"), []string{
		`{"ts":"now","type":"error","error":"today"}`,
	})
	writeTestLog(t, dir, yesterday.Format("2006-01-02"), []string{
		`{"ts":"yesterday","type":"error","error":"yesterday"}`,
	})

	// Check 2 days.
	result, err := runAuditCheckInternal(dir, 2, true)
	if err != nil {
		t.Fatalf("RunAuditCheck: %v", err)
	}

	if result.TimeRange != "last 2d" {
		t.Errorf("TimeRange = %q, want %q", result.TimeRange, "last 2d")
	}

	for _, p := range result.Patterns {
		if p.Name == "errors" && p.Count != 2 {
			t.Errorf("errors count = %d, want 2 across two days", p.Count)
		}
	}
}
