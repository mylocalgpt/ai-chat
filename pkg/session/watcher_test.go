package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

type watcherMockStore struct {
	sessions         map[string]*core.Session
	sessionErr       error
	activeSession    *core.ActiveWorkspaceSession
	activeSessionErr error
	activeCalls      []int64
}

func (m *watcherMockStore) GetActiveWorkspace(_ context.Context, _, _ string) (*core.ActiveWorkspace, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) GetActiveWorkspaceSessionBySessionID(_ context.Context, sessionID int64) (*core.ActiveWorkspaceSession, error) {
	m.activeCalls = append(m.activeCalls, sessionID)
	return m.activeSession, m.activeSessionErr
}

func (m *watcherMockStore) SetActiveWorkspace(_ context.Context, _, _ string, _ int64) error {
	return nil
}

func (m *watcherMockStore) GetActiveSessionForWorkspace(_ context.Context, _, _ string, _ int64) (*core.ActiveWorkspaceSession, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) SetActiveSessionForWorkspace(_ context.Context, _, _ string, _, _ int64) error {
	return nil
}

func (m *watcherMockStore) ClearActiveSessionForWorkspace(_ context.Context, _, _ string, _ int64) error {
	return nil
}

func (m *watcherMockStore) GetWorkspaceByID(_ context.Context, _ int64) (*core.Workspace, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) GetWorkspaceByName(_ context.Context, _ string) (*core.Workspace, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) GetWorkspaceByAlias(_ context.Context, _ string) (*core.Workspace, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) GetSessionByID(_ context.Context, _ int64) (*core.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) GetSessionByTmuxSession(_ context.Context, tmuxSession string) (*core.Session, error) {
	if m.sessionErr != nil {
		return nil, m.sessionErr
	}
	if m.sessions != nil {
		if s, ok := m.sessions[tmuxSession]; ok {
			return s, nil
		}
		return nil, errors.New("session not found")
	}
	return nil, errors.New("no sessions configured")
}

func (m *watcherMockStore) GetActiveSession(_ context.Context, _ int64) (*core.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) GetActiveSessionForSender(_ context.Context, _, _ string, _ int64) (*core.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) CreateSession(_ context.Context, _ int64, _, _, _ string) (*core.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) UpdateSessionStatus(_ context.Context, _ int64, _ string) error {
	return nil
}

func (m *watcherMockStore) TouchSession(_ context.Context, _ int64) error {
	return nil
}

func (m *watcherMockStore) ListSessions(_ context.Context) ([]core.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) ListActiveSessionsForWorkspace(_ context.Context, _ int64) ([]core.Session, error) {
	return nil, errors.New("not implemented")
}

func (m *watcherMockStore) CountActiveSessionsForWorkspace(_ context.Context, _ int64) (int, error) {
	return 0, errors.New("not implemented")
}

func (m *watcherMockStore) UpdateWorkspaceMetadata(_ context.Context, _ int64, _ json.RawMessage) error {
	return nil
}

func (m *watcherMockStore) UpdateAgentSessionID(_ context.Context, _ int64, _ string) error {
	return nil
}

func (m *watcherMockStore) GetSessionByAgentSessionID(_ context.Context, _ string) (*core.Session, error) {
	return nil, errors.New("not implemented")
}

func TestWatcher_EmitsEventOnWrite(t *testing.T) {
	dir := t.TempDir()
	ch := make(chan core.ResponseEvent, 10)

	sessionID := int64(42)
	store := &watcherMockStore{
		sessions: map[string]*core.Session{
			"test-session": {ID: sessionID, TmuxSession: "test-session"},
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID: "user1",
			Channel:  "telegram",
		},
	}

	w := NewWatcher(dir, ch, store, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = w.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	info := core.SessionInfo{
		Name:      "test-session",
		Workspace: "lab",
		Agent:     "opencode",
	}
	_, err := executor.NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err = executor.AppendMessage(filepath.Join(dir, "test-session.json"), executor.ResponseMessage{
		Role:      "agent",
		Content:   "hello world",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("appending message: %v", err)
	}

	select {
	case event := <-ch:
		if event.SessionName != "test-session" {
			t.Errorf("expected session name 'test-session', got %q", event.SessionName)
		}
		if event.Content != "hello world" {
			t.Errorf("expected content 'hello world', got %q", event.Content)
		}
		if event.SenderID != "user1" {
			t.Errorf("expected sender 'user1', got %q", event.SenderID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_NoDuplicateEvents(t *testing.T) {
	dir := t.TempDir()
	ch := make(chan core.ResponseEvent, 10)

	sessionID := int64(42)
	store := &watcherMockStore{
		sessions: map[string]*core.Session{
			"test-session": {ID: sessionID, TmuxSession: "test-session"},
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID: "user1",
			Channel:  "telegram",
		},
	}

	w := NewWatcher(dir, ch, store, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = w.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	info := core.SessionInfo{
		Name:      "test-session",
		Workspace: "lab",
		Agent:     "opencode",
	}
	_, err := executor.NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err = executor.AppendMessage(filepath.Join(dir, "test-session.json"), executor.ResponseMessage{
		Role:      "agent",
		Content:   "first message",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("appending message: %v", err)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first event")
	}

	time.Sleep(100 * time.Millisecond)

	err = executor.AppendMessage(filepath.Join(dir, "test-session.json"), executor.ResponseMessage{
		Role:      "user",
		Content:   "user message",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("appending user message: %v", err)
	}

	select {
	case event := <-ch:
		t.Errorf("unexpected event for user-only message: %v", event)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestWatcher_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	ch := make(chan core.ResponseEvent, 10)

	store := &watcherMockStore{
		sessions: map[string]*core.Session{
			"session-a": {ID: 1, TmuxSession: "session-a"},
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID: "user1",
			Channel:  "telegram",
		},
	}

	w := NewWatcher(dir, ch, store, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = w.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	infoA := core.SessionInfo{Name: "session-a", Workspace: "lab", Agent: "opencode"}
	_, err := executor.NewResponseFile(dir, infoA)
	if err != nil {
		t.Fatalf("creating response file a: %v", err)
	}

	err = executor.AppendMessage(filepath.Join(dir, "session-a.json"), executor.ResponseMessage{
		Role: "agent", Content: "message from a", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("appending message: %v", err)
	}

	select {
	case event := <-ch:
		if event.SessionName != "session-a" {
			t.Errorf("expected session 'session-a', got %q", event.SessionName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_PollingFallback(t *testing.T) {
	dir := t.TempDir()
	ch := make(chan core.ResponseEvent, 10)

	sessionID := int64(42)
	store := &watcherMockStore{
		sessions: map[string]*core.Session{
			"test-session": {ID: sessionID, TmuxSession: "test-session"},
		},
		activeSession: &core.ActiveWorkspaceSession{
			SenderID: "user1",
			Channel:  "telegram",
		},
	}

	w := NewWatcher(dir, ch, store, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = w.runPolling(ctx)
}

func TestWatcher_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	ch := make(chan core.ResponseEvent, 10)

	store := &watcherMockStore{
		sessions:      map[string]*core.Session{},
		sessionErr:    errors.New("not found"),
		activeSession: &core.ActiveWorkspaceSession{SenderID: "user1", Channel: "telegram"},
	}

	w := NewWatcher(dir, ch, store, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = w.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	info := core.SessionInfo{Name: "unknown-session", Workspace: "lab", Agent: "opencode"}
	_, err := executor.NewResponseFile(dir, info)
	if err != nil {
		t.Fatalf("creating response file: %v", err)
	}

	err = executor.AppendMessage(filepath.Join(dir, "unknown-session.json"), executor.ResponseMessage{
		Role: "agent", Content: "hello", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("appending message: %v", err)
	}

	select {
	case event := <-ch:
		t.Errorf("unexpected event for unknown session: %v", event)
	case <-time.After(500 * time.Millisecond):
	}
}
