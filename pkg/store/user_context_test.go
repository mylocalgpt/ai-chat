package store

import (
	"context"
	"errors"
	"testing"
)

func TestActiveWorkspaceSetAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	// Set active workspace.
	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w.ID); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}

	// Get.
	active, err := s.GetActiveWorkspace(ctx, "user1", "telegram")
	if err != nil {
		t.Fatalf("GetActiveWorkspace: %v", err)
	}
	if active.SenderID != "user1" {
		t.Errorf("sender_id = %q, want %q", active.SenderID, "user1")
	}
	if active.Channel != "telegram" {
		t.Errorf("channel = %q, want %q", active.Channel, "telegram")
	}
	if active.WorkspaceID != w.ID {
		t.Errorf("workspace_id = %d, want %d", active.WorkspaceID, w.ID)
	}
}

func TestActiveWorkspaceUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w1, err := s.CreateWorkspace(ctx, "ws-1", "/tmp/ws1", "")
	if err != nil {
		t.Fatalf("CreateWorkspace 1: %v", err)
	}
	w2, err := s.CreateWorkspace(ctx, "ws-2", "/tmp/ws2", "")
	if err != nil {
		t.Fatalf("CreateWorkspace 2: %v", err)
	}

	// Set to ws-1.
	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w1.ID); err != nil {
		t.Fatalf("SetActiveWorkspace 1: %v", err)
	}

	// Upsert to ws-2.
	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w2.ID); err != nil {
		t.Fatalf("SetActiveWorkspace 2: %v", err)
	}

	active, err := s.GetActiveWorkspace(ctx, "user1", "telegram")
	if err != nil {
		t.Fatalf("GetActiveWorkspace: %v", err)
	}
	if active.WorkspaceID != w2.ID {
		t.Errorf("workspace_id = %d, want %d", active.WorkspaceID, w2.ID)
	}
}

func TestActiveWorkspaceNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetActiveWorkspace(ctx, "nonexistent", "telegram")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSetActiveSessionForWorkspaceCreatesMapping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	sess, err := s.CreateSession(ctx, w.ID, "opencode", "slug1", "tmux-1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w.ID); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}

	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w.ID, sess.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace: %v", err)
	}

	active, err := s.GetActiveSessionForWorkspace(ctx, "user1", "telegram", w.ID)
	if err != nil {
		t.Fatalf("GetActiveSessionForWorkspace: %v", err)
	}
	if active.WorkspaceID != w.ID {
		t.Fatalf("workspace_id = %d, want %d", active.WorkspaceID, w.ID)
	}
	if active.SessionID != sess.ID {
		t.Fatalf("session_id = %d, want %d", active.SessionID, sess.ID)
	}
}

func TestWorkspaceSwitchPreservesSavedSessionMappings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w1, err := s.CreateWorkspace(ctx, "ws-1", "/tmp/ws1", "")
	if err != nil {
		t.Fatalf("CreateWorkspace 1: %v", err)
	}
	w2, err := s.CreateWorkspace(ctx, "ws-2", "/tmp/ws2", "")
	if err != nil {
		t.Fatalf("CreateWorkspace 2: %v", err)
	}

	sess1, err := s.CreateSession(ctx, w1.ID, "opencode", "slug1", "tmux-1")
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}

	sess2, err := s.CreateSession(ctx, w2.ID, "opencode", "slug2", "tmux-2")
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}

	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w1.ID); err != nil {
		t.Fatalf("SetActiveWorkspace 1: %v", err)
	}

	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w1.ID, sess1.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace 1: %v", err)
	}

	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w2.ID); err != nil {
		t.Fatalf("SetActiveWorkspace 2: %v", err)
	}
	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w2.ID, sess2.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace 2: %v", err)
	}

	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w1.ID); err != nil {
		t.Fatalf("SetActiveWorkspace 3: %v", err)
	}

	activeWorkspace, err := s.GetActiveWorkspace(ctx, "user1", "telegram")
	if err != nil {
		t.Fatalf("GetActiveWorkspace: %v", err)
	}
	if activeWorkspace.WorkspaceID != w1.ID {
		t.Fatalf("workspace_id = %d, want %d", activeWorkspace.WorkspaceID, w1.ID)
	}

	activeSession1, err := s.GetActiveSessionForWorkspace(ctx, "user1", "telegram", w1.ID)
	if err != nil {
		t.Fatalf("GetActiveSessionForWorkspace 1: %v", err)
	}
	if activeSession1.SessionID != sess1.ID {
		t.Fatalf("session_id = %d, want %d", activeSession1.SessionID, sess1.ID)
	}

	activeSession2, err := s.GetActiveSessionForWorkspace(ctx, "user1", "telegram", w2.ID)
	if err != nil {
		t.Fatalf("GetActiveSessionForWorkspace 2: %v", err)
	}
	if activeSession2.SessionID != sess2.ID {
		t.Fatalf("session_id = %d, want %d", activeSession2.SessionID, sess2.ID)
	}
}

func TestGetActiveWorkspaceSessionBySessionID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	sess, err := s.CreateSession(ctx, w.ID, "opencode", "slug1", "tmux-1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w.ID); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}
	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w.ID, sess.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace: %v", err)
	}

	active, err := s.GetActiveWorkspaceSessionBySessionID(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetActiveWorkspaceSessionBySessionID: %v", err)
	}
	if active.SenderID != "user1" || active.Channel != "telegram" || active.WorkspaceID != w.ID {
		t.Fatalf("unexpected active mapping: %+v", active)
	}
}
