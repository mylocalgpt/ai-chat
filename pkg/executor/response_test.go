package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func testSessionInfo() core.SessionInfo {
	return core.SessionInfo{
		Name:          "ai-chat-lab-a3f2",
		Slug:          "a3f2",
		Workspace:     "lab",
		WorkspacePath: "/tmp/lab",
		Agent:         "claude",
	}
}

func TestNewResponseFileCreatesValidJSON(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	want := filepath.Join(dir, "ai-chat-lab-a3f2.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	rf, err := ReadResponseFile(path)
	if err != nil {
		t.Fatalf("ReadResponseFile: %v", err)
	}

	if rf.Session != "ai-chat-lab-a3f2" {
		t.Errorf("session = %q, want %q", rf.Session, "ai-chat-lab-a3f2")
	}
	if rf.Workspace != "lab" {
		t.Errorf("workspace = %q, want %q", rf.Workspace, "lab")
	}
	if rf.Agent != "claude" {
		t.Errorf("agent = %q, want %q", rf.Agent, "claude")
	}
	if rf.Created.IsZero() {
		t.Error("created should not be zero")
	}
	if len(rf.Messages) != 0 {
		t.Errorf("messages length = %d, want 0", len(rf.Messages))
	}
}

func TestNewResponseFileFailsIfExists(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	_, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("first NewResponseFile: %v", err)
	}

	_, err = NewResponseFile(dir, info)
	if err == nil {
		t.Error("expected error on duplicate creation")
	}
}

func TestAppendMessage(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	msg := ResponseMessage{
		Role:      "user",
		Content:   "hello",
		Timestamp: time.Now().UTC(),
	}
	if err := AppendMessage(path, msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	rf, err := ReadResponseFile(path)
	if err != nil {
		t.Fatalf("ReadResponseFile: %v", err)
	}
	if len(rf.Messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(rf.Messages))
	}
	if rf.Messages[0].Role != "user" {
		t.Errorf("role = %q, want %q", rf.Messages[0].Role, "user")
	}
	if rf.Messages[0].Content != "hello" {
		t.Errorf("content = %q, want %q", rf.Messages[0].Content, "hello")
	}
}

func TestReadResponseFileAfterMultipleAppends(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	msgs := []ResponseMessage{
		{Role: "user", Content: "hi", Timestamp: time.Now().UTC()},
		{Role: "agent", Content: "hello!", Timestamp: time.Now().UTC()},
		{Role: "user", Content: "how are you?", Timestamp: time.Now().UTC()},
		{Role: "agent", Content: "doing great", Timestamp: time.Now().UTC()},
	}

	for _, m := range msgs {
		if err := AppendMessage(path, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	rf, err := ReadResponseFile(path)
	if err != nil {
		t.Fatalf("ReadResponseFile: %v", err)
	}
	if len(rf.Messages) != 4 {
		t.Fatalf("messages length = %d, want 4", len(rf.Messages))
	}
}

func TestLatestAgentMessage(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	// No messages yet.
	content, err := LatestAgentMessage(path)
	if err != nil {
		t.Fatalf("LatestAgentMessage: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty, got %q", content)
	}

	// Add user then agent messages.
	_ = AppendMessage(path, ResponseMessage{Role: "user", Content: "hi", Timestamp: time.Now().UTC()})
	_ = AppendMessage(path, ResponseMessage{Role: "agent", Content: "first response", Timestamp: time.Now().UTC()})
	_ = AppendMessage(path, ResponseMessage{Role: "user", Content: "more", Timestamp: time.Now().UTC()})
	_ = AppendMessage(path, ResponseMessage{Role: "agent", Content: "second response", Timestamp: time.Now().UTC()})

	content, err = LatestAgentMessage(path)
	if err != nil {
		t.Fatalf("LatestAgentMessage: %v", err)
	}
	if content != "second response" {
		t.Errorf("got %q, want %q", content, "second response")
	}
}

func TestSessionPreview(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	// Empty file.
	firstUser, lastAgent, err := SessionPreview(path)
	if err != nil {
		t.Fatalf("SessionPreview: %v", err)
	}
	if firstUser != "" || lastAgent != "" {
		t.Errorf("empty file: firstUser=%q, lastAgent=%q", firstUser, lastAgent)
	}

	// Add messages.
	_ = AppendMessage(path, ResponseMessage{Role: "user", Content: "first question", Timestamp: time.Now().UTC()})
	_ = AppendMessage(path, ResponseMessage{Role: "agent", Content: "answer 1", Timestamp: time.Now().UTC()})
	_ = AppendMessage(path, ResponseMessage{Role: "user", Content: "second question", Timestamp: time.Now().UTC()})
	_ = AppendMessage(path, ResponseMessage{Role: "agent", Content: "answer 2", Timestamp: time.Now().UTC()})

	firstUser, lastAgent, err = SessionPreview(path)
	if err != nil {
		t.Fatalf("SessionPreview: %v", err)
	}
	if firstUser != "first question" {
		t.Errorf("firstUser = %q, want %q", firstUser, "first question")
	}
	if lastAgent != "answer 2" {
		t.Errorf("lastAgent = %q, want %q", lastAgent, "answer 2")
	}
}

func TestSessionPreviewOnlyUserMessages(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	_ = AppendMessage(path, ResponseMessage{Role: "user", Content: "hello", Timestamp: time.Now().UTC()})

	firstUser, lastAgent, err := SessionPreview(path)
	if err != nil {
		t.Fatalf("SessionPreview: %v", err)
	}
	if firstUser != "hello" {
		t.Errorf("firstUser = %q, want %q", firstUser, "hello")
	}
	if lastAgent != "" {
		t.Errorf("lastAgent = %q, want empty", lastAgent)
	}
}

func TestSessionPreviewOnlyAgentMessages(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	_ = AppendMessage(path, ResponseMessage{Role: "agent", Content: "unprompted", Timestamp: time.Now().UTC()})

	firstUser, lastAgent, err := SessionPreview(path)
	if err != nil {
		t.Fatalf("SessionPreview: %v", err)
	}
	if firstUser != "" {
		t.Errorf("firstUser = %q, want empty", firstUser)
	}
	if lastAgent != "unprompted" {
		t.Errorf("lastAgent = %q, want %q", lastAgent, "unprompted")
	}
}

func TestConcurrentAppendMessage(t *testing.T) {
	dir := t.TempDir()
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(idx int) {
			defer wg.Done()
			msg := ResponseMessage{
				Role:      "user",
				Content:   fmt.Sprintf("message-%d", idx),
				Timestamp: time.Now().UTC(),
			}
			if err := AppendMessage(path, msg); err != nil {
				t.Errorf("AppendMessage(%d): %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	rf, err := ReadResponseFile(path)
	if err != nil {
		t.Fatalf("ReadResponseFile: %v", err)
	}
	if len(rf.Messages) != n {
		t.Errorf("messages length = %d, want %d", len(rf.Messages), n)
	}
}

func TestDefaultResponseDir(t *testing.T) {
	dir := DefaultResponseDir()
	if dir == "" {
		t.Error("DefaultResponseDir returned empty string")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	want := filepath.Join(home, ".config", "ai-chat", "responses")
	if dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
}

func TestResponseFilePath(t *testing.T) {
	path := ResponseFilePath("/some/dir", "ai-chat-lab-a3f2")
	want := "/some/dir/ai-chat-lab-a3f2.json"
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestNewResponseFileCreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "sub", "dir")
	info := testSessionInfo()

	path, err := NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("response file not found at %q: %v", path, err)
	}
}
