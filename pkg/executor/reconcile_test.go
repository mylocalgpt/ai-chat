package executor

import (
	"context"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// trackingStore wraps mockStore and records calls.
type trackingStore struct {
	*mockStore
	updatedStatuses map[int64]string
	touchedIDs      map[int64]bool
}

func newTrackingStore() *trackingStore {
	return &trackingStore{
		mockStore:       newMockStore(),
		updatedStatuses: make(map[int64]string),
		touchedIDs:      make(map[int64]bool),
	}
}

func (s *trackingStore) UpdateSessionStatus(_ context.Context, id int64, status string) error {
	s.updatedStatuses[id] = status
	for _, sess := range s.sessions {
		if sess.ID == id {
			sess.Status = status
		}
	}
	return nil
}

func (s *trackingStore) TouchSession(_ context.Context, id int64) error {
	s.touchedIDs[id] = true
	return nil
}

func TestReconcileMarksCrashedSessions(t *testing.T) {
	st := newTrackingStore()
	st.sessions = []*core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "opencode", Slug: "a1b2", TmuxSession: "ai-chat-proj-a1b2", Status: string(core.SessionActive)},
		{ID: 2, WorkspaceID: 2, Agent: "opencode", Slug: "c3d4", TmuxSession: "ai-chat-other-c3d4", Status: string(core.SessionActive)},
	}

	// No live tmux sessions.
	tmx := newMockTmux()
	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	exec := NewExecutor(st, tmx, reg, "")

	result, err := exec.ReconcileSessions(context.Background())
	if err != nil {
		t.Fatalf("ReconcileSessions: %v", err)
	}

	if result.Crashed != 2 {
		t.Errorf("crashed = %d, want 2", result.Crashed)
	}

	for _, id := range []int64{1, 2} {
		if st.updatedStatuses[id] != string(core.SessionCrashed) {
			t.Errorf("session %d status = %q, want %q", id, st.updatedStatuses[id], core.SessionCrashed)
		}
	}
}

func TestReconcileKeepsAliveSessions(t *testing.T) {
	st := newTrackingStore()
	st.sessions = []*core.Session{
		{ID: 1, WorkspaceID: 1, Agent: "opencode", Slug: "a1b2", TmuxSession: "ai-chat-proj-a1b2", Status: string(core.SessionActive)},
	}

	tmx := newMockTmux()
	tmx.sessions["ai-chat-proj-a1b2"] = true

	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	exec := NewExecutor(st, tmx, reg, "")

	result, err := exec.ReconcileSessions(context.Background())
	if err != nil {
		t.Fatalf("ReconcileSessions: %v", err)
	}

	if result.Reconciled != 1 {
		t.Errorf("reconciled = %d, want 1", result.Reconciled)
	}
	if !st.touchedIDs[1] {
		t.Error("session 1 should have been touched")
	}
	if _, changed := st.updatedStatuses[1]; changed {
		t.Error("session 1 status should not have been changed")
	}
}

func TestReconcileCountsOrphans(t *testing.T) {
	st := newTrackingStore()
	// No DB sessions.

	tmx := newMockTmux()
	tmx.sessions["ai-chat-orphan-x1y2"] = true

	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	exec := NewExecutor(st, tmx, reg, "")

	result, err := exec.ReconcileSessions(context.Background())
	if err != nil {
		t.Fatalf("ReconcileSessions: %v", err)
	}

	if result.Orphaned != 1 {
		t.Errorf("orphaned = %d, want 1", result.Orphaned)
	}
}

func TestCleanupKillsStale(t *testing.T) {
	st := newTrackingStore()
	old := time.Now().Add(-48 * time.Hour)
	st.sessions = []*core.Session{
		{
			ID: 1, WorkspaceID: 1, Agent: "opencode",
			TmuxSession:  "ai-chat-proj-a1b2",
			Status:       string(core.SessionActive),
			LastActivity: old,
			StartedAt:    old,
		},
	}

	tmx := newMockTmux()
	tmx.sessions["ai-chat-proj-a1b2"] = true

	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	exec := NewExecutor(st, tmx, reg, "")

	result, err := exec.CleanupStaleSessions(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}

	if result.Cleaned != 1 {
		t.Errorf("cleaned = %d, want 1", result.Cleaned)
	}

	// tmux session should have been killed.
	if tmx.sessions["ai-chat-proj-a1b2"] {
		t.Error("stale tmux session should have been killed")
	}

	// DB status should be expired.
	if st.updatedStatuses[1] != string(core.SessionExpired) {
		t.Errorf("session status = %q, want %q", st.updatedStatuses[1], core.SessionExpired)
	}
}

func TestCleanupSkipsFresh(t *testing.T) {
	st := newTrackingStore()
	recent := time.Now().Add(-1 * time.Hour)
	st.sessions = []*core.Session{
		{
			ID: 1, WorkspaceID: 1, Agent: "opencode",
			TmuxSession:  "ai-chat-proj-a1b2",
			Status:       string(core.SessionActive),
			LastActivity: recent,
			StartedAt:    recent,
		},
	}

	tmx := newMockTmux()
	tmx.sessions["ai-chat-proj-a1b2"] = true

	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	exec := NewExecutor(st, tmx, reg, "")

	result, err := exec.CleanupStaleSessions(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}

	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
	if result.Cleaned != 0 {
		t.Errorf("cleaned = %d, want 0", result.Cleaned)
	}
}

func TestCleanupFallsBackToStartedAt(t *testing.T) {
	st := newTrackingStore()
	old := time.Now().Add(-48 * time.Hour)
	st.sessions = []*core.Session{
		{
			ID: 1, WorkspaceID: 1, Agent: "opencode",
			TmuxSession: "ai-chat-proj-a1b2",
			Status:      string(core.SessionActive),
			StartedAt:   old,
			// LastActivity is zero (NULL in DB).
		},
	}

	tmx := newMockTmux()
	tmx.sessions["ai-chat-proj-a1b2"] = true

	reg := NewHarnessRegistry(NewTmux(), NewServerManager(), NewSecurityProxy())
	exec := NewExecutor(st, tmx, reg, "")

	result, err := exec.CleanupStaleSessions(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}

	if result.Cleaned != 1 {
		t.Errorf("cleaned = %d, want 1 (should fall back to StartedAt)", result.Cleaned)
	}
}
