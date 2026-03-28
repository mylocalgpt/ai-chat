package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

type Reaper struct {
	store    sessionStore
	adapters AdapterRegistry
	cfg      ReaperConfig
}

type ReaperConfig struct {
	SoftIdleTimeout time.Duration
	HardIdleTimeout time.Duration
	Interval        time.Duration
}

func NewReaper(store sessionStore, adapters AdapterRegistry, cfg ReaperConfig) *Reaper {
	if cfg.SoftIdleTimeout == 0 {
		cfg.SoftIdleTimeout = 30 * time.Minute
	}
	if cfg.HardIdleTimeout == 0 {
		cfg.HardIdleTimeout = 2 * time.Hour
	}
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Minute
	}
	return &Reaper{
		store:    store,
		adapters: adapters,
		cfg:      cfg,
	}
}

func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	slog.Info("reaper started", "interval", r.cfg.Interval, "soft_timeout", r.cfg.SoftIdleTimeout, "hard_timeout", r.cfg.HardIdleTimeout)

	for {
		select {
		case <-ctx.Done():
			slog.Info("reaper stopped")
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Reaper) tick(ctx context.Context) {
	sessions, err := r.store.ListSessions(ctx)
	if err != nil {
		slog.Warn("reaper: failed to list sessions", "error", err)
		return
	}

	now := time.Now()

	for _, sess := range sessions {
		if sess.Status == string(core.SessionCrashed) || sess.Status == string(core.SessionExpired) {
			continue
		}

		effectiveTime := sess.LastActivity
		if effectiveTime.IsZero() {
			effectiveTime = sess.StartedAt
		}

		idleDuration := now.Sub(effectiveTime)

		if idleDuration > r.cfg.HardIdleTimeout {
			r.hardKill(ctx, sess)
		} else if idleDuration > r.cfg.SoftIdleTimeout && sess.Status != string(core.SessionIdle) {
			r.softKill(ctx, sess)
		}
	}
}

func (r *Reaper) softKill(ctx context.Context, sess core.Session) {
	adapter, err := r.adapters.GetAdapter(sess.Agent)
	if err != nil {
		slog.Warn("reaper: soft kill failed to get adapter", "session_id", sess.ID, "agent", sess.Agent, "error", err)
		return
	}

	if adapter.Name() == "opencode" {
		info := &core.SessionInfo{
			Name:      sess.TmuxSession,
			Slug:      sess.Slug,
			Agent:     sess.Agent,
			Workspace: "",
		}
		if err := adapter.Stop(ctx, *info); err != nil {
			slog.Warn("reaper: soft kill stop failed", "session_id", sess.ID, "error", err)
		}
	}

	if err := r.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionIdle)); err != nil {
		slog.Warn("reaper: soft kill status update failed", "session_id", sess.ID, "error", err)
		return
	}

	slog.Info("reaper: soft killed session", "session_id", sess.ID, "agent", sess.Agent)
}

func (r *Reaper) hardKill(ctx context.Context, sess core.Session) {
	adapter, err := r.adapters.GetAdapter(sess.Agent)
	if err != nil {
		slog.Warn("reaper: hard kill failed to get adapter", "session_id", sess.ID, "agent", sess.Agent, "error", err)
		return
	}

	info := &core.SessionInfo{
		Name:  sess.TmuxSession,
		Slug:  sess.Slug,
		Agent: sess.Agent,
	}

	if adapter.IsAlive(*info) {
		if err := adapter.Stop(ctx, *info); err != nil {
			slog.Warn("reaper: hard kill stop failed", "session_id", sess.ID, "error", err)
		}
	}

	if err := r.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired)); err != nil {
		slog.Warn("reaper: hard kill status update failed", "session_id", sess.ID, "error", err)
		return
	}

	slog.Info("reaper: hard killed session", "session_id", sess.ID, "agent", sess.Agent)
}

type ReconcileResult struct {
	Reconciled int
	Crashed    int
	Orphaned   int
	Errors     int
}

func (m *Manager) ReconcileOnStartup(ctx context.Context) (*ReconcileResult, error) {
	result := &ReconcileResult{}

	allSessions, err := m.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	var activeSessions []core.Session
	for _, sess := range allSessions {
		if sess.Status == string(core.SessionActive) || sess.Status == string(core.SessionIdle) {
			activeSessions = append(activeSessions, sess)
		}
	}

	for _, sess := range activeSessions {
		adapter, err := m.adapters.GetAdapter(sess.Agent)
		if err != nil {
			slog.Warn("reconcile: failed to get adapter", "session_id", sess.ID, "agent", sess.Agent, "error", err)
			result.Errors++
			continue
		}

		info := m.buildSessionInfoFromSession(ctx, &sess)
		if !adapter.IsAlive(*info) {
			if err := m.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionCrashed)); err != nil {
				slog.Error("reconcile: failed to mark crashed", "session_id", sess.ID, "error", err)
				result.Errors++
				continue
			}
			result.Crashed++
			slog.Info("reconcile: marked crashed", "session_id", sess.ID, "tmux_session", sess.TmuxSession)
		} else {
			if err := m.store.TouchSession(ctx, sess.ID); err != nil {
				slog.Error("reconcile: failed to touch session", "session_id", sess.ID, "error", err)
				result.Errors++
				continue
			}
			result.Reconciled++
		}
	}

	orphaned, err := m.findOrphanedTmuxSessions(ctx, activeSessions)
	if err != nil {
		slog.Warn("reconcile: failed to find orphaned sessions", "error", err)
	} else {
		result.Orphaned = orphaned
	}

	return result, nil
}

func (m *Manager) findOrphanedTmuxSessions(ctx context.Context, activeSessions []core.Session) (int, error) {
	tmux := executor.NewTmux()
	liveSessions, err := tmux.ListSessions()
	if err != nil {
		return 0, err
	}

	dbSessionNames := make(map[string]bool, len(activeSessions))
	for _, sess := range activeSessions {
		dbSessionNames[sess.TmuxSession] = true
	}

	orphaned := 0
	for _, name := range liveSessions {
		_, _, ok := executor.ParseSessionSlug(name)
		if !ok {
			continue
		}
		if !dbSessionNames[name] {
			orphaned++
			slog.Warn("reconcile: orphaned tmux session", "tmux_session", name)
			if err := tmux.KillSession(name); err != nil {
				slog.Warn("reconcile: failed to kill orphaned session", "tmux_session", name, "error", err)
			}
		}
	}

	return orphaned, nil
}

func (m *Manager) buildSessionInfoFromSession(ctx context.Context, sess *core.Session) *core.SessionInfo {
	ws, err := m.store.GetWorkspaceByID(ctx, sess.WorkspaceID)
	if err != nil {
		return &core.SessionInfo{
			Name:      sess.TmuxSession,
			Slug:      sess.Slug,
			Agent:     sess.Agent,
			Workspace: "",
		}
	}
	return m.buildSessionInfo(ws, sess)
}
