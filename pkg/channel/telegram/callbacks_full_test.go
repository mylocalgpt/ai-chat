package telegram

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

// writeTestResponseFile creates a response JSON file in dir for the given session
// with the provided messages.
func writeTestResponseFile(t *testing.T, dir, sessionName string, messages []executor.ResponseMessage) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating test dir: %v", err)
	}
	rf := executor.ResponseFile{
		Session:  sessionName,
		Messages: messages,
	}
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		t.Fatalf("marshaling test response: %v", err)
	}
	path := filepath.Join(dir, sessionName+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing test response file: %v", err)
	}
	return path
}

func TestHandleFullOutputCallback_Valid(t *testing.T) {
	dir := t.TempDir()
	content := "This is the full agent response content that was too long for a message."
	writeTestResponseFile(t, dir, "eager-canyon", []executor.ResponseMessage{
		{Role: "user", Content: "What is the meaning of life?"},
		{Role: "agent", Content: content},
	})

	mb := &mockDocumentBot{}
	h := newCallbackHandler(nil, map[int64]bool{}, nil, mb, dir)

	h.handleFullOutputCallback(context.Background(), &mockCallbackBot{}, 123, 42, "eager-canyon:0")

	if len(mb.sentDocuments) != 1 {
		t.Fatalf("expected 1 SendDocument call, got %d", len(mb.sentDocuments))
	}

	params := mb.sentDocuments[0]
	if params.ChatID != int64(123) {
		t.Errorf("ChatID = %v, want 123", params.ChatID)
	}

	if len(mb.uploadedData) != 1 {
		t.Fatalf("expected 1 uploaded data entry, got %d", len(mb.uploadedData))
	}
	if mb.uploadedData[0] != content {
		t.Errorf("uploaded content = %q, want %q", mb.uploadedData[0], content)
	}

	// Verify filename follows documentFilename pattern.
	// The filename is set via documentFilename which is tested in document_test.go.
	// Here we just verify the call went through successfully.
}

func TestHandleFullOutputCallback_SecondAgentMessage(t *testing.T) {
	dir := t.TempDir()
	firstContent := "First agent response."
	secondContent := "Second agent response with more detail."
	writeTestResponseFile(t, dir, "test-session", []executor.ResponseMessage{
		{Role: "user", Content: "First question"},
		{Role: "agent", Content: firstContent},
		{Role: "user", Content: "Second question"},
		{Role: "agent", Content: secondContent},
	})

	mb := &mockDocumentBot{}
	h := newCallbackHandler(nil, map[int64]bool{}, nil, mb, dir)

	// Request index 1 (second agent message).
	h.handleFullOutputCallback(context.Background(), &mockCallbackBot{}, 123, 42, "test-session:1")

	if len(mb.uploadedData) != 1 {
		t.Fatalf("expected 1 uploaded data entry, got %d", len(mb.uploadedData))
	}
	if mb.uploadedData[0] != secondContent {
		t.Errorf("uploaded content = %q, want %q", mb.uploadedData[0], secondContent)
	}
}

func TestHandleFullOutputCallback_InvalidFormat(t *testing.T) {
	mb := &mockDocumentBot{}
	h := newCallbackHandler(nil, map[int64]bool{}, nil, mb, "/tmp/nonexistent")

	// No colon separator - should handle gracefully without panic.
	h.handleFullOutputCallback(context.Background(), &mockCallbackBot{}, 123, 42, "nodataseparator")

	if len(mb.sentDocuments) != 0 {
		t.Errorf("expected 0 SendDocument calls for invalid format, got %d", len(mb.sentDocuments))
	}
}

func TestHandleFullOutputCallback_InvalidIndex(t *testing.T) {
	mb := &mockDocumentBot{}
	h := newCallbackHandler(nil, map[int64]bool{}, nil, mb, "/tmp/nonexistent")

	// Non-numeric index - should handle gracefully without panic.
	h.handleFullOutputCallback(context.Background(), &mockCallbackBot{}, 123, 42, "session:notanumber")

	if len(mb.sentDocuments) != 0 {
		t.Errorf("expected 0 SendDocument calls for invalid index, got %d", len(mb.sentDocuments))
	}
}

func TestHandleFullOutputCallback_MissingFile(t *testing.T) {
	dir := t.TempDir() // Empty dir, no response files.

	mb := &mockDocumentBot{}
	h := newCallbackHandler(nil, map[int64]bool{}, nil, mb, dir)

	// Valid format but file doesn't exist.
	h.handleFullOutputCallback(context.Background(), &mockCallbackBot{}, 123, 42, "nonexistent-session:0")

	if len(mb.sentDocuments) != 0 {
		t.Errorf("expected 0 SendDocument calls for missing file, got %d", len(mb.sentDocuments))
	}
}

func TestHandleFullOutputCallback_IndexOutOfRange(t *testing.T) {
	dir := t.TempDir()
	writeTestResponseFile(t, dir, "small-session", []executor.ResponseMessage{
		{Role: "user", Content: "Hello"},
		{Role: "agent", Content: "Hi there"},
	})

	mb := &mockDocumentBot{}
	h := newCallbackHandler(nil, map[int64]bool{}, nil, mb, dir)

	// Index 5 is way beyond the 1 agent message.
	h.handleFullOutputCallback(context.Background(), &mockCallbackBot{}, 123, 42, "small-session:5")

	if len(mb.sentDocuments) != 0 {
		t.Errorf("expected 0 SendDocument calls for out-of-range index, got %d", len(mb.sentDocuments))
	}
}
