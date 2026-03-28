package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogBeforeInit(t *testing.T) {
	resetGlobal()
	defer resetGlobal()

	err := LogGlobal(AuditEntry{Type: "test"})
	if err == nil {
		t.Fatal("expected error when logging before Init")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCloseBeforeInit(t *testing.T) {
	resetGlobal()
	defer resetGlobal()

	err := CloseGlobal()
	if err != nil {
		t.Fatalf("Close before Init should return nil, got: %v", err)
	}
}

func TestDoubleInit(t *testing.T) {
	resetGlobal()
	defer resetGlobal()

	dir := t.TempDir()

	if err := Init(dir, 7); err != nil {
		t.Fatalf("first Init: %v", err)
	}

	// Second Init with a different (nonexistent) dir should be a no-op.
	if err := Init("/nonexistent/path", 7); err != nil {
		t.Fatalf("second Init should succeed (no-op), got: %v", err)
	}
}

func TestGlobalIntegration(t *testing.T) {
	resetGlobal()
	defer resetGlobal()

	dir := t.TempDir()

	if err := Init(dir, 7); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Log entries of various types.
	entries := []AuditEntry{
		Inbound("telegram", "user1", "lab", "hello"),
		Route("lab", "agent_task", "opencode", 0.9),
		AgentSend("lab", "opencode", "sess1", "do task"),
		AgentResponse("lab", "opencode", "sess1", 500, 1200),
		Outbound("telegram", "user1", "lab", 200),
		Health("lab", "active"),
	}

	for _, e := range entries {
		if err := LogGlobal(e); err != nil {
			t.Fatalf("LogGlobal: %v", err)
		}
	}

	// Verify file exists with correct content.
	today := time.Now().UTC().Format("2006-01-02")
	logFile := filepath.Join(dir, today+".jsonl")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(entries) {
		t.Errorf("expected %d lines, got %d", len(entries), len(lines))
	}

	// Run audit check over the directory.
	result, err := RunAuditCheck(dir, 1)
	if err != nil {
		t.Fatalf("RunAuditCheck: %v", err)
	}

	if result.TotalEntries != len(entries) {
		t.Errorf("TotalEntries = %d, want %d", result.TotalEntries, len(entries))
	}

	// Close cleanly.
	if err := CloseGlobal(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
