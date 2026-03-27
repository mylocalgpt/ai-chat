package store

import (
	"context"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestMessageCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create message without workspace.
	msg := &core.Message{
		Channel:   "telegram",
		SenderID:  "user1",
		Content:   "hello",
		Direction: core.InboundDirection,
		Status:    core.StatusPending,
	}
	if err := s.CreateMessage(ctx, msg); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if msg.ID == 0 {
		t.Error("expected non-zero ID")
	}

	// Create message with workspace.
	w, err := s.CreateWorkspace(ctx, "test-ws", "/tmp/ws", "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	msg2 := &core.Message{
		Channel:     "telegram",
		SenderID:    "user1",
		WorkspaceID: &w.ID,
		Content:     "hello from workspace",
		Direction:   core.InboundDirection,
		Status:      core.StatusPending,
	}
	if err := s.CreateMessage(ctx, msg2); err != nil {
		t.Fatalf("CreateMessage with workspace: %v", err)
	}

	// Get pending messages.
	pending, err := s.GetPendingMessages(ctx, "telegram")
	if err != nil {
		t.Fatalf("GetPendingMessages: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("pending len = %d, want 2", len(pending))
	}

	// Update status.
	if err := s.UpdateMessageStatus(ctx, msg.ID, string(core.StatusDone)); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}
	pending, err = s.GetPendingMessages(ctx, "telegram")
	if err != nil {
		t.Fatalf("GetPendingMessages after update: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("pending len = %d, want 1", len(pending))
	}
}

func TestMessageForeignKeyConstraint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	badID := int64(9999)
	msg := &core.Message{
		Channel:     "telegram",
		SenderID:    "user1",
		WorkspaceID: &badID,
		Content:     "should fail",
		Direction:   core.InboundDirection,
		Status:      core.StatusPending,
	}
	err := s.CreateMessage(ctx, msg)
	if err == nil {
		t.Fatal("expected error for invalid workspace_id, got nil")
	}
}

func TestGetPendingMessagesEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pending, err := s.GetPendingMessages(ctx, "web")
	if err != nil {
		t.Fatalf("GetPendingMessages: %v", err)
	}
	if pending == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(pending) != 0 {
		t.Errorf("len = %d, want 0", len(pending))
	}
}
