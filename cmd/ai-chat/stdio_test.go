package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/app"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/session"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type stdioTestChannel struct {
	msgCh chan core.OutboundMessage
}

func (c *stdioTestChannel) Start(context.Context) error { return nil }

func (c *stdioTestChannel) Stop() error { return nil }

func (c *stdioTestChannel) Send(_ context.Context, msg core.OutboundMessage) error {
	select {
	case c.msgCh <- msg:
	default:
	}
	return nil
}

func TestStdioBackgroundForwardsSessionResponses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}

	st := store.New(db)
	responsesDir := filepath.Join(t.TempDir(), "responses")
	if err := os.MkdirAll(responsesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll responses: %v", err)
	}
	manager := session.NewManager(st, executor.NewHarnessRegistry(executor.NewTmux()), executor.NewSecurityProxy(), session.ManagerConfig{
		ResponsesDir: responsesDir,
	})
	channel := &stdioTestChannel{msgCh: make(chan core.OutboundMessage, 1)}
	wg := app.StartBackground(ctx, st, manager, channel)
	defer func() {
		cancel()
		wg.Wait()
	}()
	time.Sleep(200 * time.Millisecond)

	ws, err := st.CreateWorkspace(ctx, "lab", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	sess, err := st.CreateSession(ctx, ws.ID, "opencode", "abcd", "ai-chat-lab-abcd")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.SetActiveSessionForWorkspace(ctx, mcpSenderID, mcpChannel, ws.ID, sess.ID); err != nil {
		t.Fatalf("SetActiveSessionForWorkspace: %v", err)
	}

	path, err := executor.NewResponseFile(responsesDir, core.SessionInfo{
		Name:          sess.TmuxSession,
		Slug:          sess.Slug,
		Workspace:     ws.Name,
		WorkspacePath: ws.Path,
		Agent:         sess.Agent,
		ResponseFile:  executor.ResponseFilePath(responsesDir, sess.TmuxSession),
	})
	if err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}
	if err := executor.AppendMessage(path, executor.ResponseMessage{
		Role:      "agent",
		Content:   "stdio delivered this",
		Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	select {
	case msg := <-channel.msgCh:
		if msg.Channel != mcpChannel {
			t.Fatalf("channel = %q, want %q", msg.Channel, mcpChannel)
		}
		if msg.RecipientID != mcpSenderID {
			t.Fatalf("recipient = %q, want %q", msg.RecipientID, mcpSenderID)
		}
		if msg.Content != "stdio delivered this" {
			t.Fatalf("content = %q", msg.Content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watcher-driven response delivery")
	}
}

func TestShutdownStdioBackgroundCancelsBeforeWait(t *testing.T) {
	cancelled := make(chan struct{})
	shutdownStdioBackground(func() {
		close(cancelled)
	}, waitRecorder{
		wait: func() {
			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("wait started before cancel")
			}
		},
	})
}

type waitRecorder struct {
	wait func()
}

func (w waitRecorder) Wait() {
	w.wait()
}
