package executor

import (
	"context"
	"log/slog"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// ReconcileResult holds the outcome of a startup reconciliation.
type ReconcileResult struct {
	Reconciled int // sessions that were alive and updated
	Crashed    int // sessions marked as crashed (DB active, tmux dead)
	Orphaned   int // tmux sessions with no DB record
	Errors     int // individual operations that failed (logged and skipped)
}

// CleanupResult holds the outcome of a stale session cleanup.
type CleanupResult struct {
	Cleaned int
	Skipped int // sessions within maxAge, left alone
}

// ReconcileSessions cross-references DB session records with live tmux
// sessions. Should be called at startup to recover from crashes. It uses a
// partial failure strategy: individual errors are logged and counted but do not
// stop processing.
func (e *Executor) ReconcileSessions(ctx context.Context) (*ReconcileResult, error) {
	result := &ReconcileResult{}

	// Load all sessions from DB.
	allSessions, err := e.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	// Filter to active/idle.
	var activeSessions []core.Session
	for _, sess := range allSessions {
		if sess.Status == string(core.SessionActive) || sess.Status == string(core.SessionIdle) {
			activeSessions = append(activeSessions, sess)
		}
	}

	// Get live tmux sessions.
	liveSessions, err := e.tmux.ListSessions()
	if err != nil {
		return nil, err
	}

	// Build set for O(1) lookup.
	liveSet := make(map[string]bool, len(liveSessions))
	for _, name := range liveSessions {
		liveSet[name] = true
	}

	// Reconcile each active DB session against live tmux state.
	var firstErr error
	for _, sess := range activeSessions {
		if !liveSet[sess.TmuxSession] {
			// tmux session is dead, mark as crashed.
			if err := e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionCrashed)); err != nil {
				slog.Error("reconcile: failed to mark crashed", "session_id", sess.ID, "error", err)
				result.Errors++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			result.Crashed++
			slog.Info("reconcile: marked crashed", "session_id", sess.ID, "tmux_session", sess.TmuxSession)
		} else {
			// tmux session is alive, touch to update last_activity.
			if err := e.store.TouchSession(ctx, sess.ID); err != nil {
				slog.Error("reconcile: failed to touch session", "session_id", sess.ID, "error", err)
				result.Errors++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			result.Reconciled++
		}
	}

	// Check for orphaned tmux sessions (our prefix, no DB record).
	dbSessionNames := make(map[string]bool, len(activeSessions))
	for _, sess := range activeSessions {
		dbSessionNames[sess.TmuxSession] = true
	}

	knownAgents := e.registry.KnownAgents()
	for _, name := range liveSessions {
		_, _, ok := ParseSessionName(name, knownAgents)
		if !ok {
			continue // not our session
		}
		if !dbSessionNames[name] {
			result.Orphaned++
			slog.Warn("reconcile: orphaned tmux session", "tmux_session", name)
		}
	}

	return result, firstErr
}

// CleanupStaleSessions finds sessions that have been inactive longer than
// maxAge and marks them as expired, killing the tmux session if still alive.
func (e *Executor) CleanupStaleSessions(ctx context.Context, maxAge time.Duration) (*CleanupResult, error) {
	result := &CleanupResult{}

	allSessions, err := e.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-maxAge)

	for _, sess := range allSessions {
		if sess.Status != string(core.SessionActive) && sess.Status != string(core.SessionIdle) {
			continue
		}

		// Determine effective age: use LastActivity if non-zero, otherwise
		// fall back to StartedAt.
		effectiveTime := sess.LastActivity
		if effectiveTime.IsZero() {
			effectiveTime = sess.StartedAt
		}

		if effectiveTime.After(cutoff) {
			result.Skipped++
			continue
		}

		// Session is stale, clean it up.
		if sess.TmuxSession != "" && e.tmux.HasSession(sess.TmuxSession) {
			if err := e.tmux.KillSession(sess.TmuxSession); err != nil {
				slog.Warn("cleanup: failed to kill tmux session", "tmux_session", sess.TmuxSession, "error", err)
			}
		}

		if err := e.store.UpdateSessionStatus(ctx, sess.ID, string(core.SessionExpired)); err != nil {
			return result, err
		}
		result.Cleaned++
		slog.Info("cleanup: expired stale session", "session_id", sess.ID, "effective_age", time.Since(effectiveTime))
	}

	return result, nil
}
