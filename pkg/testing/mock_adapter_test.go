package testing

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

func TestMockAdapter_Name(t *testing.T) {
	m := NewMockAdapter("test-agent")
	if m.Name() != "test-agent" {
		t.Errorf("Name() = %q, want %q", m.Name(), "test-agent")
	}
}

func TestMockAdapter_Send_WritesResponseFile(t *testing.T) {
	tmpDir := t.TempDir()
	sessionName := "test-session"
	responseFile := filepath.Join(tmpDir, sessionName+".json")

	info := core.SessionInfo{
		Name:          sessionName,
		Slug:          "test",
		Workspace:     "test-ws",
		WorkspacePath: tmpDir,
		Agent:         "test-agent",
		ResponseFile:  responseFile,
	}

	if _, err := executor.NewResponseFile(tmpDir, info); err != nil {
		t.Fatalf("NewResponseFile() error = %v", err)
	}

	m := NewMockAdapter("test-agent")
	ctx := context.Background()

	if err := m.Send(ctx, info, "ping"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	rf, err := executor.ReadResponseFile(responseFile)
	if err != nil {
		t.Fatalf("ReadResponseFile() error = %v", err)
	}

	if len(rf.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(rf.Messages))
	}

	if rf.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q, want %q", rf.Messages[0].Role, "user")
	}
	if rf.Messages[0].Content != "ping" {
		t.Errorf("Messages[0].Content = %q, want %q", rf.Messages[0].Content, "ping")
	}

	if rf.Messages[1].Role != "agent" {
		t.Errorf("Messages[1].Role = %q, want %q", rf.Messages[1].Role, "agent")
	}
	if rf.Messages[1].Content != "pong" {
		t.Errorf("Messages[1].Content = %q, want %q", rf.Messages[1].Content, "pong")
	}
}

func TestMockAdapter_Send_FallbackEcho(t *testing.T) {
	tmpDir := t.TempDir()
	sessionName := "test-session"
	responseFile := filepath.Join(tmpDir, sessionName+".json")

	info := core.SessionInfo{
		Name:          sessionName,
		Slug:          "test",
		Workspace:     "test-ws",
		WorkspacePath: tmpDir,
		Agent:         "test-agent",
		ResponseFile:  responseFile,
	}

	if _, err := executor.NewResponseFile(tmpDir, info); err != nil {
		t.Fatalf("NewResponseFile() error = %v", err)
	}

	m := NewMockAdapter("test-agent")
	ctx := context.Background()

	if err := m.Send(ctx, info, "unknown message"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	rf, err := executor.ReadResponseFile(responseFile)
	if err != nil {
		t.Fatalf("ReadResponseFile() error = %v", err)
	}

	if len(rf.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(rf.Messages))
	}

	want := "Echo: unknown message"
	if rf.Messages[1].Content != want {
		t.Errorf("Messages[1].Content = %q, want %q", rf.Messages[1].Content, want)
	}
}

func TestMockAdapter_Send_RespectsDelay(t *testing.T) {
	tmpDir := t.TempDir()
	sessionName := "test-session"
	responseFile := filepath.Join(tmpDir, sessionName+".json")

	info := core.SessionInfo{
		Name:          sessionName,
		Slug:          "test",
		Workspace:     "test-ws",
		WorkspacePath: tmpDir,
		Agent:         "test-agent",
		ResponseFile:  responseFile,
	}

	if _, err := executor.NewResponseFile(tmpDir, info); err != nil {
		t.Fatalf("NewResponseFile() error = %v", err)
	}

	m := NewMockAdapter("test-agent")
	ctx := context.Background()

	start := time.Now()
	if err := m.Send(ctx, info, "slow"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 1900*time.Millisecond {
		t.Errorf("Send() took %v, want at least 2s", elapsed)
	}
}

func TestMockAdapter_Send_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	sessionName := "test-session"
	responseFile := filepath.Join(tmpDir, sessionName+".json")

	info := core.SessionInfo{
		Name:          sessionName,
		Slug:          "test",
		Workspace:     "test-ws",
		WorkspacePath: tmpDir,
		Agent:         "test-agent",
		ResponseFile:  responseFile,
	}

	if _, err := executor.NewResponseFile(tmpDir, info); err != nil {
		t.Fatalf("NewResponseFile() error = %v", err)
	}

	m := NewMockAdapter("test-agent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Send(ctx, info, "slow")
	if err == nil {
		t.Error("Send() expected error for cancelled context, got nil")
	}
}

func TestMockAdapter_AddResponse(t *testing.T) {
	tmpDir := t.TempDir()
	sessionName := "test-session"
	responseFile := filepath.Join(tmpDir, sessionName+".json")

	info := core.SessionInfo{
		Name:          sessionName,
		Slug:          "test",
		Workspace:     "test-ws",
		WorkspacePath: tmpDir,
		Agent:         "test-agent",
		ResponseFile:  responseFile,
	}

	if _, err := executor.NewResponseFile(tmpDir, info); err != nil {
		t.Fatalf("NewResponseFile() error = %v", err)
	}

	m := NewMockAdapter("test-agent")
	m.AddResponse("custom", MockResponse{Content: "custom response", Delay: 0})

	ctx := context.Background()
	if err := m.Send(ctx, info, "custom"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	rf, err := executor.ReadResponseFile(responseFile)
	if err != nil {
		t.Fatalf("ReadResponseFile() error = %v", err)
	}

	if rf.Messages[1].Content != "custom response" {
		t.Errorf("Messages[1].Content = %q, want %q", rf.Messages[1].Content, "custom response")
	}
}

func TestMockAdapter_IsAlive(t *testing.T) {
	m := NewMockAdapter("test-agent")
	if !m.IsAlive(core.SessionInfo{}) {
		t.Error("IsAlive() = false, want true")
	}
}

func TestMockAdapter_Spawn(t *testing.T) {
	m := NewMockAdapter("test-agent")
	if err := m.Spawn(context.Background(), core.SessionInfo{}); err != nil {
		t.Errorf("Spawn() error = %v, want nil", err)
	}
}

func TestMockAdapter_Stop(t *testing.T) {
	m := NewMockAdapter("test-agent")
	if err := m.Stop(context.Background(), core.SessionInfo{}); err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}
}

func TestMockAdapter_PreconfiguredResponses(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ping", "pong"},
		{"long", ""},
		{"markdown", ""},
		{"code", ""},
		{"slow", "delayed response"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tmpDir := t.TempDir()
			sessionName := "test-session"
			responseFile := filepath.Join(tmpDir, sessionName+".json")

			info := core.SessionInfo{
				Name:          sessionName,
				Slug:          "test",
				Workspace:     "test-ws",
				WorkspacePath: tmpDir,
				Agent:         "test-agent",
				ResponseFile:  responseFile,
			}

			if _, err := executor.NewResponseFile(tmpDir, info); err != nil {
				t.Fatalf("NewResponseFile() error = %v", err)
			}

			m := NewMockAdapter("test-agent")
			ctx := context.Background()

			if tt.input == "slow" {
				ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				if err := m.Send(ctx, info, tt.input); err != nil {
					t.Fatalf("Send() error = %v", err)
				}
			} else {
				if err := m.Send(ctx, info, tt.input); err != nil {
					t.Fatalf("Send() error = %v", err)
				}
			}

			rf, err := executor.ReadResponseFile(responseFile)
			if err != nil {
				t.Fatalf("ReadResponseFile() error = %v", err)
			}

			if tt.expected != "" && rf.Messages[1].Content != tt.expected {
				t.Errorf("Messages[1].Content = %q, want %q", rf.Messages[1].Content, tt.expected)
			}

			if tt.expected == "" && rf.Messages[1].Content == "" {
				t.Errorf("Messages[1].Content is empty, expected non-empty")
			}
		})
	}
}
