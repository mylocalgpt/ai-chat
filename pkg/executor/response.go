package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// ResponseFile is the JSON format for session response files.
type ResponseFile struct {
	Session   string            `json:"session"`
	Workspace string            `json:"workspace"`
	Agent     string            `json:"agent"`
	Created   time.Time         `json:"created"`
	Messages  []ResponseMessage `json:"messages"`
}

// ResponseMessage is a single message in a response file.
type ResponseMessage struct {
	Role      string    `json:"role"`      // "user" or "agent"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// DefaultResponseDir returns the default response directory path:
// ~/.config/ai-chat/responses/. Callers with config access should use
// cfg.ResponsesDir instead; this is a fallback for contexts without config.
func DefaultResponseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "ai-chat", "responses")
	}
	return filepath.Join(home, ".config", "ai-chat", "responses")
}

// ResponseFilePath returns the full path for a session's response file.
func ResponseFilePath(dir, sessionName string) string {
	return filepath.Join(dir, sessionName+".json")
}

// NewResponseFile creates a new response file on disk.
// Writes the initial JSON with session metadata and empty messages array.
// Uses O_CREATE|O_EXCL for atomic creation (fails if file already exists).
func NewResponseFile(dir string, info core.SessionInfo) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating response dir: %w", err)
	}

	path := ResponseFilePath(dir, info.Name)

	rf := ResponseFile{
		Session:   info.Name,
		Workspace: info.Workspace,
		Agent:     info.Agent,
		Created:   time.Now().UTC(),
		Messages:  []ResponseMessage{},
	}

	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling response file: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("creating response file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("writing response file: %w", err)
	}

	return path, nil
}

// AppendMessage appends a message to an existing response file.
// Uses file locking (flock) for safe concurrent access.
func AppendMessage(path string, msg ResponseMessage) error {
	return withFileLock(path, true, func(f *os.File) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading response file: %w", err)
		}

		var rf ResponseFile
		if err := json.Unmarshal(data, &rf); err != nil {
			return fmt.Errorf("parsing response file: %w", err)
		}

		rf.Messages = append(rf.Messages, msg)

		out, err := json.MarshalIndent(rf, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling response file: %w", err)
		}
		out = append(out, '\n')

		if err := os.WriteFile(path, out, 0o644); err != nil {
			return fmt.Errorf("writing response file: %w", err)
		}

		return nil
	})
}

// ReadResponseFile reads and parses a response file from disk.
func ReadResponseFile(path string) (*ResponseFile, error) {
	var rf ResponseFile
	err := withFileLock(path, false, func(f *os.File) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading response file: %w", err)
		}
		if err := json.Unmarshal(data, &rf); err != nil {
			return fmt.Errorf("parsing response file: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &rf, nil
}

// LatestAgentMessage returns the content of the last "agent" role message.
// Returns empty string if no agent messages exist.
func LatestAgentMessage(path string) (string, error) {
	rf, err := ReadResponseFile(path)
	if err != nil {
		return "", err
	}

	for i := len(rf.Messages) - 1; i >= 0; i-- {
		if rf.Messages[i].Role == "agent" {
			return rf.Messages[i].Content, nil
		}
	}
	return "", nil
}

// SessionPreview returns the first user message and last agent message
// from a response file. Used for session list display.
func SessionPreview(path string) (firstUser, lastAgent string, err error) {
	rf, err := ReadResponseFile(path)
	if err != nil {
		return "", "", err
	}

	for _, m := range rf.Messages {
		if m.Role == "user" {
			firstUser = m.Content
			break
		}
	}

	for i := len(rf.Messages) - 1; i >= 0; i-- {
		if rf.Messages[i].Role == "agent" {
			lastAgent = rf.Messages[i].Content
			break
		}
	}

	return firstUser, lastAgent, nil
}

// withFileLock opens a file and holds a flock for the duration of fn.
// If exclusive is true, an exclusive lock (LOCK_EX) is used for writes;
// otherwise a shared lock (LOCK_SH) is used for reads.
func withFileLock(path string, exclusive bool, fn func(*os.File) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file for lock: %w", err)
	}
	defer func() { _ = f.Close() }()

	lockType := syscall.LOCK_SH
	if exclusive {
		lockType = syscall.LOCK_EX
	}

	if err := syscall.Flock(int(f.Fd()), lockType); err != nil {
		return fmt.Errorf("acquiring file lock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn(f)
}
