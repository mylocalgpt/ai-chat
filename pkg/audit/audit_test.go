package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogCreatesFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir, 7)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "inbound",
		Channel:   "telegram",
		Sender:    "user1",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	expected := filepath.Join(dir, today+".jsonl")

	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("expected file %s to exist", expected)
	}
}

func TestLogRoundTrip(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir, 7)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	ts := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	entry := AuditEntry{
		Timestamp: ts,
		Type:      "agent_response",
		Agent:     "gpt4",
		Duration:  1500,
		Length:    200,
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, today+".jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var got AuditEntry
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Type != "agent_response" {
		t.Errorf("Type = %q, want %q", got.Type, "agent_response")
	}
	if got.Agent != "gpt4" {
		t.Errorf("Agent = %q, want %q", got.Agent, "gpt4")
	}
	if got.Duration != 1500 {
		t.Errorf("Duration = %d, want %d", got.Duration, 1500)
	}
	if got.Length != 200 {
		t.Errorf("Length = %d, want %d", got.Length, 200)
	}
}

func TestOmitemptyFields(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir, 7)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: time.Now().UTC(),
		Type:      "inbound",
		Channel:   "telegram",
		Sender:    "user1",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, today+".jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	line := strings.TrimSpace(string(data))

	// These fields should be absent when zero-valued.
	absent := []string{"agent", "duration_ms", "length", "confidence", "error", "session", "workspace"}
	for _, key := range absent {
		if strings.Contains(line, `"`+key+`"`) {
			t.Errorf("expected %q to be absent from output, got: %s", key, line)
		}
	}

	// These fields should be present.
	present := []string{"ts", "type", "channel", "sender"}
	for _, key := range present {
		if !strings.Contains(line, `"`+key+`"`) {
			t.Errorf("expected %q to be present in output, got: %s", key, line)
		}
	}
}

func TestDateRotation(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir, 7)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	day1 := time.Date(2026, 3, 28, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 29, 0, 1, 0, 0, time.UTC)

	// Override nowFunc for day 1.
	logger.nowFunc = func() time.Time { return day1 }

	entry1 := AuditEntry{
		Timestamp: day1,
		Type:      "inbound",
		Channel:   "web",
	}
	if err := logger.Log(entry1); err != nil {
		t.Fatalf("Log day1: %v", err)
	}

	// Advance to day 2.
	logger.nowFunc = func() time.Time { return day2 }

	entry2 := AuditEntry{
		Timestamp: day2,
		Type:      "outbound",
		Channel:   "web",
	}
	if err := logger.Log(entry2); err != nil {
		t.Fatalf("Log day2: %v", err)
	}

	// Both files should exist.
	file1 := filepath.Join(dir, "2026-03-28.jsonl")
	file2 := filepath.Join(dir, "2026-03-29.jsonl")

	if _, err := os.Stat(file1); os.IsNotExist(err) {
		t.Fatalf("expected %s to exist", file1)
	}
	if _, err := os.Stat(file2); os.IsNotExist(err) {
		t.Fatalf("expected %s to exist", file2)
	}

	// Verify each file has exactly one entry.
	for _, path := range []string{file1, file2} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", path, err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 1 {
			t.Errorf("%s: expected 1 line, got %d", path, len(lines))
		}
	}
}

func TestOutputFormat(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir, 7)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	ts := time.Date(2026, 3, 28, 14, 30, 0, 0, time.UTC)
	entry := AuditEntry{
		Timestamp:  ts,
		Type:       "route",
		Channel:    "telegram",
		Workspace:  "default",
		Action:     "agent_task",
		Confidence: 0.95,
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, today+".jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	line := strings.TrimSpace(string(data))

	// Should be valid JSON.
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("invalid JSON: %v\nline: %s", err, line)
	}

	// Verify specific field values.
	if raw["type"] != "route" {
		t.Errorf("type = %v, want %q", raw["type"], "route")
	}
	if raw["action"] != "agent_task" {
		t.Errorf("action = %v, want %q", raw["action"], "agent_task")
	}
	if raw["confidence"] != 0.95 {
		t.Errorf("confidence = %v, want %v", raw["confidence"], 0.95)
	}
}
