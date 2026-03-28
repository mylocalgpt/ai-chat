package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestCopilotAdapterName(t *testing.T) {
	a := NewCopilotAdapter()
	if a.Name() != "copilot" {
		t.Errorf("Name() = %q, want %q", a.Name(), "copilot")
	}
}

func TestCopilotAdapterIsAlive(t *testing.T) {
	a := NewCopilotAdapter()
	if !a.IsAlive(core.SessionInfo{}) {
		t.Error("IsAlive() should always return true for stateless adapter")
	}
}

func TestCopilotAdapterStop(t *testing.T) {
	a := NewCopilotAdapter()
	if err := a.Stop(context.Background(), core.SessionInfo{}); err != nil {
		t.Errorf("Stop() should be no-op, got error: %v", err)
	}
}

func TestCopilotAdapterSpawnCopilotNotOnPath(t *testing.T) {
	// Save original PATH.
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)

	// Set PATH to empty so copilot is not found.
	os.Setenv("PATH", "")

	a := NewCopilotAdapter()
	session := core.SessionInfo{
		Name:          "test-session",
		WorkspacePath: "/tmp",
	}

	err := a.Spawn(context.Background(), session)
	if err == nil {
		t.Error("Spawn() should fail when copilot is not on PATH")
	}
}

func TestCopilotAdapterSpawnCreatesResponseFile(t *testing.T) {
	// Create a fake copilot binary.
	tmpDir := t.TempDir()
	fakeCopilot := filepath.Join(tmpDir, "copilot")
	if err := os.WriteFile(fakeCopilot, []byte("#!/bin/sh\necho 'ok'\n"), 0o755); err != nil {
		t.Fatalf("failed to create fake copilot: %v", err)
	}

	// Save original PATH.
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	os.Setenv("PATH", tmpDir)

	a := NewCopilotAdapter()
	session := core.SessionInfo{
		Name:          "test-session",
		WorkspacePath: "/tmp",
		ResponseFile:  filepath.Join(tmpDir, "test-session.json"),
	}

	err := a.Spawn(context.Background(), session)
	if err != nil {
		t.Fatalf("Spawn() failed: %v", err)
	}

	// Check response file was created.
	if _, err := os.Stat(session.ResponseFile); os.IsNotExist(err) {
		t.Error("response file should have been created")
	}
}

func TestCopilotAdapterSendWritesResponseFile(t *testing.T) {
	// Create a fake copilot binary that outputs a response.
	tmpDir := t.TempDir()
	fakeCopilot := filepath.Join(tmpDir, "copilot")
	script := `#!/bin/sh
echo "This is the copilot response"
`
	if err := os.WriteFile(fakeCopilot, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to create fake copilot: %v", err)
	}

	// Save original PATH.
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	os.Setenv("PATH", tmpDir)

	a := NewCopilotAdapter()
	session := core.SessionInfo{
		Name:          "test-session",
		WorkspacePath: tmpDir,
		ResponseFile:  filepath.Join(tmpDir, "test-session.json"),
	}

	// Create response file first (Spawn would do this).
	_, err := NewResponseFile(tmpDir, session)
	if err != nil {
		t.Fatalf("failed to create response file: %v", err)
	}

	err = a.Send(context.Background(), session, "hello")
	if err != nil {
		t.Fatalf("Send() failed: %v", err)
	}

	// Read response file and check content.
	rf, err := ReadResponseFile(session.ResponseFile)
	if err != nil {
		t.Fatalf("failed to read response file: %v", err)
	}

	if len(rf.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(rf.Messages))
	}

	if rf.Messages[0].Role != "user" {
		t.Errorf("first message role = %q, want %q", rf.Messages[0].Role, "user")
	}
	if rf.Messages[0].Content != "hello" {
		t.Errorf("first message content = %q, want %q", rf.Messages[0].Content, "hello")
	}

	if rf.Messages[1].Role != "agent" {
		t.Errorf("second message role = %q, want %q", rf.Messages[1].Role, "agent")
	}
	if rf.Messages[1].Content != "This is the copilot response\n" {
		t.Errorf("second message content = %q, want %q", rf.Messages[1].Content, "This is the copilot response\n")
	}
}

// TestHelperProcess is a helper for mocking exec.Command in tests.
// This is not a real test - it's used by other tests via exec.Command.
func TestHelperProcess(t *testing.T) {
	// This test is only called via exec.Command in other tests.
	// It's a pattern for mocking external commands.
}
