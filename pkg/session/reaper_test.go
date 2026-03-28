package session

import (
	"context"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

func TestReaper_SoftKillTriggersAtCorrectThreshold(t *testing.T) {
	now := time.Now()
	oldSession := core.Session{
		ID:           1,
		WorkspaceID:  1,
		Agent:        "opencode",
		TmuxSession:  "ai-chat-lab-a3f2",
		Slug:         "a3f2",
		Status:       string(core.SessionActive),
		StartedAt:    now.Add(-45 * time.Minute),
		LastActivity: now.Add(-45 * time.Minute),
	}

	store := &mockStore{
		sessions: []core.Session{oldSession},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	reaper := NewReaper(store, registry, ReaperConfig{
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		Interval:        1 * time.Second,
	})

	reaper.tick(context.Background())

	if !adapter.stopCalled {
		t.Error("expected Stop to be called for opencode soft kill")
	}
}

func TestReaper_HardKillTriggersForSessionsPastHardThreshold(t *testing.T) {
	now := time.Now()
	veryOldSession := core.Session{
		ID:           2,
		WorkspaceID:  1,
		Agent:        "opencode",
		TmuxSession:  "ai-chat-lab-b4c5",
		Slug:         "b4c5",
		Status:       string(core.SessionActive),
		StartedAt:    now.Add(-3 * time.Hour),
		LastActivity: now.Add(-3 * time.Hour),
	}

	store := &mockStore{
		sessions: []core.Session{veryOldSession},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	reaper := NewReaper(store, registry, ReaperConfig{
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		Interval:        1 * time.Second,
	})

	reaper.tick(context.Background())

	if !adapter.stopCalled {
		t.Error("expected Stop to be called for hard kill")
	}
}

func TestReaper_SessionsWithinThresholdAreSkipped(t *testing.T) {
	now := time.Now()
	recentSession := core.Session{
		ID:           3,
		WorkspaceID:  1,
		Agent:        "opencode",
		TmuxSession:  "ai-chat-lab-c6d7",
		Slug:         "c6d7",
		Status:       string(core.SessionActive),
		StartedAt:    now.Add(-10 * time.Minute),
		LastActivity: now.Add(-10 * time.Minute),
	}

	store := &mockStore{
		sessions: []core.Session{recentSession},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	reaper := NewReaper(store, registry, ReaperConfig{
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		Interval:        1 * time.Second,
	})

	reaper.tick(context.Background())

	if adapter.stopCalled {
		t.Error("expected Stop NOT to be called for session within threshold")
	}
}

func TestReaper_CopilotSoftKillDoesNotCallStop(t *testing.T) {
	now := time.Now()
	oldSession := core.Session{
		ID:           4,
		WorkspaceID:  1,
		Agent:        "copilot",
		TmuxSession:  "ai-chat-lab-e8f9",
		Slug:         "e8f9",
		Status:       string(core.SessionActive),
		StartedAt:    now.Add(-45 * time.Minute),
		LastActivity: now.Add(-45 * time.Minute),
	}

	store := &mockStore{
		sessions: []core.Session{oldSession},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "copilot", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"copilot": adapter},
		agents:   []string{"copilot"},
	}

	reaper := NewReaper(store, registry, ReaperConfig{
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		Interval:        1 * time.Second,
	})

	reaper.tick(context.Background())

	if adapter.stopCalled {
		t.Error("expected Stop NOT to be called for copilot soft kill (stateless)")
	}
}

func TestReaper_IdleSessionsNotSoftKilledAgain(t *testing.T) {
	now := time.Now()
	idleSession := core.Session{
		ID:           5,
		WorkspaceID:  1,
		Agent:        "opencode",
		TmuxSession:  "ai-chat-lab-g0h1",
		Slug:         "g0h1",
		Status:       string(core.SessionIdle),
		StartedAt:    now.Add(-45 * time.Minute),
		LastActivity: now.Add(-45 * time.Minute),
	}

	store := &mockStore{
		sessions: []core.Session{idleSession},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	reaper := NewReaper(store, registry, ReaperConfig{
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		Interval:        1 * time.Second,
	})

	reaper.tick(context.Background())

	if adapter.stopCalled {
		t.Error("expected Stop NOT to be called for already-idle session")
	}
}

func TestReconcileOnStartup_MarksDeadSessionsAsCrashed(t *testing.T) {
	sessionID := int64(1)
	store := &mockStore{
		sessions: []core.Session{
			{
				ID:           sessionID,
				WorkspaceID:  1,
				Agent:        "opencode",
				TmuxSession:  "ai-chat-lab-a3f2",
				Slug:         "a3f2",
				Status:       string(core.SessionActive),
				StartedAt:    time.Now(),
				LastActivity: time.Now(),
			},
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: false}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	result, err := m.ReconcileOnStartup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Crashed != 1 {
		t.Errorf("expected 1 crashed, got %d", result.Crashed)
	}
}

func TestReconcileOnStartup_TouchesAliveSessions(t *testing.T) {
	sessionID := int64(1)
	store := &mockStore{
		sessions: []core.Session{
			{
				ID:           sessionID,
				WorkspaceID:  1,
				Agent:        "opencode",
				TmuxSession:  "ai-chat-lab-a3f2",
				Slug:         "a3f2",
				Status:       string(core.SessionActive),
				StartedAt:    time.Now(),
				LastActivity: time.Now(),
			},
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	result, err := m.ReconcileOnStartup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Reconciled != 1 {
		t.Errorf("expected 1 reconciled, got %d", result.Reconciled)
	}
}

func TestReconcileOnStartup_SkipsNonActiveSessions(t *testing.T) {
	store := &mockStore{
		sessions: []core.Session{
			{
				ID:           1,
				WorkspaceID:  1,
				Agent:        "opencode",
				TmuxSession:  "ai-chat-lab-a3f2",
				Slug:         "a3f2",
				Status:       string(core.SessionExpired),
				StartedAt:    time.Now(),
				LastActivity: time.Now(),
			},
			{
				ID:           2,
				WorkspaceID:  1,
				Agent:        "opencode",
				TmuxSession:  "ai-chat-lab-b4c5",
				Slug:         "b4c5",
				Status:       string(core.SessionCrashed),
				StartedAt:    time.Now(),
				LastActivity: time.Now(),
			},
		},
		workspace: &core.Workspace{
			ID:   1,
			Name: "lab",
			Path: "/path/to/lab",
		},
	}

	adapter := &mockAdapter{name: "opencode", isAlive: true}
	registry := &mockRegistry{
		adapters: map[string]executor.AgentAdapter{"opencode": adapter},
		agents:   []string{"opencode"},
	}

	m := NewManager(store, registry, nil, ManagerConfig{})

	result, err := m.ReconcileOnStartup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Reconciled != 0 || result.Crashed != 0 {
		t.Errorf("expected 0 reconciled and 0 crashed, got %d reconciled, %d crashed", result.Reconciled, result.Crashed)
	}
}

func TestNewReaper_AppliesDefaults(t *testing.T) {
	store := &mockStore{}
	registry := &mockRegistry{adapters: make(map[string]executor.AgentAdapter), agents: []string{"opencode"}}

	reaper := NewReaper(store, registry, ReaperConfig{})

	if reaper.cfg.SoftIdleTimeout != 30*time.Minute {
		t.Errorf("expected SoftIdleTimeout 30m, got %v", reaper.cfg.SoftIdleTimeout)
	}
	if reaper.cfg.HardIdleTimeout != 2*time.Hour {
		t.Errorf("expected HardIdleTimeout 2h, got %v", reaper.cfg.HardIdleTimeout)
	}
	if reaper.cfg.Interval != 5*time.Minute {
		t.Errorf("expected Interval 5m, got %v", reaper.cfg.Interval)
	}
}

var _ executor.AgentAdapter = (*mockAdapter)(nil)
