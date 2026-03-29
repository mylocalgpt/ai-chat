package store

import (
	"context"
	"errors"
	"testing"
)

func TestSessionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	// Create session.
	sess, err := s.CreateSession(ctx, w.ID, "opencode", "a1b2", "tmux-sess-1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if sess.Status != "active" {
		t.Errorf("status = %q, want %q", sess.Status, "active")
	}

	// Get active session.
	active, err := s.GetActiveSession(ctx, w.ID)
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if active.ID != sess.ID {
		t.Errorf("ID = %d, want %d", active.ID, sess.ID)
	}

	// Update status to idle.
	if err := s.UpdateSessionStatus(ctx, sess.ID, "idle"); err != nil {
		t.Fatalf("UpdateSessionStatus: %v", err)
	}

	// No active session.
	_, err = s.GetActiveSession(ctx, w.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTouchSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	sess, err := s.CreateSession(ctx, w.ID, "opencode", "c3d4", "tmux-1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// last_activity starts as zero/null.
	if !sess.LastActivity.IsZero() {
		t.Errorf("initial last_activity should be zero, got %v", sess.LastActivity)
	}

	// Touch.
	if err := s.TouchSession(ctx, sess.ID); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	// Verify via list.
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len = %d, want 1", len(sessions))
	}
	if sessions[0].LastActivity.IsZero() {
		t.Error("last_activity should be set after touch")
	}
}

func TestGetActiveSessionForSenderUsesExplicitWorkspaceMapping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	sess1, err := s.CreateSession(ctx, w.ID, "opencode", "a1b2", "tmux-1")
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	sess2, err := s.CreateSession(ctx, w.ID, "opencode", "c3d4", "tmux-2")
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w.ID); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}
	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w.ID, sess1.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace 1: %v", err)
	}
	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w.ID, sess2.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace 2: %v", err)
	}

	active, err := s.GetActiveSessionForSender(ctx, "user1", "telegram", w.ID)
	if err != nil {
		t.Fatalf("GetActiveSessionForSender: %v", err)
	}
	if active.ID != sess2.ID {
		t.Fatalf("ID = %d, want %d", active.ID, sess2.ID)
	}
}

func TestListSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	// Create two sessions.
	s1, err := s.CreateSession(ctx, w.ID, "opencode", "e5f6", "tmux-1")
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	_, err = s.CreateSession(ctx, w.ID, "opencode", "g7h8", "tmux-2")
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}

	// Touch the first one so it has a last_activity.
	if err := s.TouchSession(ctx, s1.ID); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len = %d, want 2", len(sessions))
	}
	// First session (touched) should be first due to ordering.
	if sessions[0].ID != s1.ID {
		t.Errorf("first session ID = %d, want %d", sessions[0].ID, s1.ID)
	}
}

func TestListSessionsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(sessions) != 0 {
		t.Errorf("len = %d, want 0", len(sessions))
	}
}
