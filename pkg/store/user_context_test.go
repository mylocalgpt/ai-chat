package store

import (
	"context"
	"errors"
	"testing"
)

func TestUserContextSetAndGet(t *testing.T) {
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
	uc, err := s.GetUserContext(ctx, "user1", "telegram")
	if err != nil {
		t.Fatalf("GetUserContext: %v", err)
	}
	if uc.SenderID != "user1" {
		t.Errorf("sender_id = %q, want %q", uc.SenderID, "user1")
	}
	if uc.Channel != "telegram" {
		t.Errorf("channel = %q, want %q", uc.Channel, "telegram")
	}
	if uc.ActiveWorkspaceID != w.ID {
		t.Errorf("active_workspace_id = %d, want %d", uc.ActiveWorkspaceID, w.ID)
	}
}

func TestUserContextUpsert(t *testing.T) {
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

	uc, err := s.GetUserContext(ctx, "user1", "telegram")
	if err != nil {
		t.Fatalf("GetUserContext: %v", err)
	}
	if uc.ActiveWorkspaceID != w2.ID {
		t.Errorf("active_workspace_id = %d, want %d", uc.ActiveWorkspaceID, w2.ID)
	}
}

func TestUserContextNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetUserContext(ctx, "nonexistent", "telegram")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
