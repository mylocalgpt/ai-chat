package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestWorkspaceCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create.
	w, err := s.CreateWorkspace(ctx, "my-project", "/home/user/projects/my-project", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if w.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if w.Name != "my-project" {
		t.Errorf("name = %q, want %q", w.Name, "my-project")
	}

	// Get by name.
	got, err := s.GetWorkspace(ctx, "my-project")
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got.ID != w.ID {
		t.Errorf("ID = %d, want %d", got.ID, w.ID)
	}

	// Get by ID.
	got2, err := s.GetWorkspaceByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceByID: %v", err)
	}
	if got2.Name != "my-project" {
		t.Errorf("name = %q, want %q", got2.Name, "my-project")
	}

	// List.
	list, err := s.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("len = %d, want 1", len(list))
	}

	// Update metadata.
	meta := json.RawMessage(`{"aliases": ["rl", "my-project"], "description": "test"}`)
	if err := s.UpdateWorkspaceMetadata(ctx, w.ID, meta); err != nil {
		t.Fatalf("UpdateWorkspaceMetadata: %v", err)
	}
	updated, err := s.GetWorkspaceByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceByID after update: %v", err)
	}
	if string(updated.Metadata) != string(meta) {
		t.Errorf("metadata = %s, want %s", updated.Metadata, meta)
	}

	// Delete.
	if err := s.DeleteWorkspace(ctx, w.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	_, err = s.GetWorkspace(ctx, "my-project")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestFindWorkspaceByAlias(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	w, err := s.CreateWorkspace(ctx, "rl-project", "/home/user/rl", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	meta := json.RawMessage(`{"aliases": ["rl", "my-project"]}`)
	if err := s.UpdateWorkspaceMetadata(ctx, w.ID, meta); err != nil {
		t.Fatalf("UpdateWorkspaceMetadata: %v", err)
	}

	// Find by alias.
	found, err := s.FindWorkspaceByAlias(ctx, "rl")
	if err != nil {
		t.Fatalf("FindWorkspaceByAlias: %v", err)
	}
	if found.ID != w.ID {
		t.Errorf("ID = %d, want %d", found.ID, w.ID)
	}

	// Not found.
	_, err = s.FindWorkspaceByAlias(ctx, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListWorkspacesEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	list, err := s.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if list == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(list) != 0 {
		t.Errorf("len = %d, want 0", len(list))
	}
}

func TestSetActiveWorkspaceKeepsOtherWorkspaceSessionMappings(t *testing.T) {
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
	sess1, err := s.CreateSession(ctx, w1.ID, "opencode", "a1b2", "tmux-1")
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	sess2, err := s.CreateSession(ctx, w2.ID, "opencode", "c3d4", "tmux-2")
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}

	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w1.ID); err != nil {
		t.Fatalf("SetActiveWorkspace 1: %v", err)
	}
	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w1.ID, sess1.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace 1: %v", err)
	}
	if err := s.SetActiveSessionForWorkspace(ctx, "user1", "telegram", w2.ID, sess2.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace 2: %v", err)
	}

	if err := s.SetActiveWorkspace(ctx, "user1", "telegram", w2.ID); err != nil {
		t.Fatalf("SetActiveWorkspace 2: %v", err)
	}

	active1, err := s.GetActiveSessionForWorkspace(ctx, "user1", "telegram", w1.ID)
	if err != nil {
		t.Fatalf("GetActiveSessionForWorkspace 1: %v", err)
	}
	if active1.SessionID != sess1.ID {
		t.Fatalf("session_id = %d, want %d", active1.SessionID, sess1.ID)
	}
	active2, err := s.GetActiveSessionForWorkspace(ctx, "user1", "telegram", w2.ID)
	if err != nil {
		t.Fatalf("GetActiveSessionForWorkspace 2: %v", err)
	}
	if active2.SessionID != sess2.ID {
		t.Fatalf("session_id = %d, want %d", active2.SessionID, sess2.ID)
	}
}
