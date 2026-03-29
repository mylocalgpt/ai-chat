package session

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

type Watcher struct {
	dir        string
	responseCh chan<- core.ResponseEvent
	store      sessionStore
	proxy      *executor.SecurityProxy
	lastSeen   map[string]int64
	mu         sync.Mutex
}

func NewWatcher(dir string, ch chan<- core.ResponseEvent, store sessionStore, proxy *executor.SecurityProxy) *Watcher {
	return &Watcher{
		dir:        dir,
		responseCh: ch,
		store:      store,
		proxy:      proxy,
		lastSeen:   make(map[string]int64),
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("fsnotify init failed, using polling fallback", "error", err)
		return w.runPolling(ctx)
	}
	defer func() { _ = fsw.Close() }()

	if err := fsw.Add(w.dir); err != nil {
		slog.Warn("fsnotify add dir failed, using polling fallback", "error", err)
		return w.runPolling(ctx)
	}

	slog.Info("watcher started", "dir", w.dir, "mode", "fsnotify")

	for {
		select {
		case <-ctx.Done():
			slog.Info("watcher stopped")
			return ctx.Err()
		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if strings.HasSuffix(event.Name, ".json") {
					w.processFile(ctx, event.Name)
				}
			}
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			slog.Warn("fsnotify error", "error", err)
		}
	}
}

func (w *Watcher) runPolling(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	slog.Info("watcher started", "dir", w.dir, "mode", "polling")

	for {
		select {
		case <-ctx.Done():
			slog.Info("watcher stopped")
			return ctx.Err()
		case <-ticker.C:
			w.pollFiles(ctx)
		}
	}
}

func (w *Watcher) pollFiles(ctx context.Context) {
	files, err := filepath.Glob(filepath.Join(w.dir, "*.json"))
	if err != nil {
		slog.Warn("failed to glob response files", "error", err)
		return
	}

	for _, f := range files {
		w.processFile(ctx, f)
	}
}

func (w *Watcher) processFile(ctx context.Context, path string) {
	sessionName := strings.TrimSuffix(filepath.Base(path), ".json")

	sess, err := w.store.GetSessionByTmuxSession(ctx, sessionName)
	if err != nil {
		slog.Debug("session not found for response file", "path", path, "error", err)
		return
	}

	rf, err := executor.ReadResponseFile(path)
	if err != nil {
		slog.Debug("failed to read response file", "path", path, "error", err)
		return
	}

	agentMsgCount := int64(0)
	for _, m := range rf.Messages {
		if m.Role == "agent" {
			agentMsgCount++
		}
	}

	w.mu.Lock()
	lastCount, seen := w.lastSeen[path]
	if seen && agentMsgCount <= lastCount {
		w.mu.Unlock()
		return
	}
	w.lastSeen[path] = agentMsgCount
	w.mu.Unlock()

	active, err := w.store.GetActiveWorkspaceSessionBySessionID(ctx, sess.ID)
	if err != nil {
		slog.Debug("active session mapping not found for session", "session_id", sess.ID, "error", err)
		return
	}

	content, err := executor.LatestAgentMessage(path)
	if err != nil {
		slog.Debug("failed to get latest agent message", "path", path, "error", err)
		return
	}
	if content == "" {
		return
	}
	if active.WorkspaceID != sess.WorkspaceID {
		slog.Debug("active mapping workspace mismatch", "session_id", sess.ID, "session_workspace_id", sess.WorkspaceID, "mapping_workspace_id", active.WorkspaceID)
		return
	}
	if w.proxy != nil && len(w.proxy.Scan(content)) > 0 {
		content = "Response blocked because it may contain sensitive content."
	}

	event := core.ResponseEvent{
		SessionName: sessionName,
		SessionID:   sess.ID,
		SenderID:    active.SenderID,
		Channel:     active.Channel,
		Content:     content,
	}

	select {
	case w.responseCh <- event:
		slog.Debug("emitted response event", "session", sessionName, "sender", active.SenderID)
	case <-ctx.Done():
	}
}
