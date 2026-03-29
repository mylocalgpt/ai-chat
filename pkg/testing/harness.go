package testing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/app"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	mcppkg "github.com/mylocalgpt/ai-chat/pkg/mcp"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/session"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type TestHarness struct {
	Store      *store.Store
	Router     *router.Router
	SessionMgr *session.Manager
	Mock       *MockAdapter
	Outbound   *MockChannel
	TempDir    string
	DB         *sql.DB
	Proxy      *executor.SecurityProxy
	Cancel     context.CancelFunc
	WG         *sync.WaitGroup
	MCP        *executorSessionManager
}

type HarnessConfig struct {
	TempDir string
}

func NewTestHarness(t interface {
	Fatalf(format string, args ...any)
	TempDir() string
}) *TestHarness {
	h, cleanup, err := newHarness(HarnessConfig{TempDir: t.TempDir()})
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatalf("creating harness: %v", err)
	}
	return h
}

func (h *TestHarness) SendMessage(ctx context.Context, senderID, content string) (string, error) {
	before := h.Outbound.Count()
	msg := core.InboundMessage{
		SenderID: senderID,
		Channel:  "test",
		Content:  content,
	}
	result, err := h.route(ctx, msg)
	if err != nil {
		return "", err
	}

	if rendered := renderResultForTest(result); rendered != "" {
		return rendered, nil
	}
	waitCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	return h.Outbound.WaitForContent(waitCtx, before)
}

func renderResultForTest(result router.Result) string {
	switch result.Kind {
	case router.ResultText:
		return result.Text
	case router.ResultWorkspacePicker:
		if result.WorkspacePicker == nil {
			return ""
		}
		parts := []string{result.WorkspacePicker.Prompt}
		for _, ws := range result.WorkspacePicker.Workspaces {
			parts = append(parts, ws.Name)
		}
		return fmt.Sprint(parts)
	case router.ResultSessionPicker:
		if result.SessionPicker == nil {
			return ""
		}
		parts := []string{result.SessionPicker.Prompt}
		for _, sess := range result.SessionPicker.Sessions {
			parts = append(parts, sess.Name)
		}
		return fmt.Sprint(parts)
	case router.ResultSecurityConfirmation:
		if result.SecurityConfirmation != nil {
			return result.SecurityConfirmation.Summary
		}
	}
	return ""
}

func (h *TestHarness) Cleanup() {
	if h.Cancel != nil {
		h.Cancel()
	}
	if h.WG != nil {
		h.WG.Wait()
	}
	if h.DB != nil {
		_ = h.DB.Close()
	}
}

func (h *TestHarness) route(ctx context.Context, msg core.InboundMessage) (router.Result, error) {
	return h.Router.Route(ctx, router.Request{Message: &msg})
}

func (h *TestHarness) ApproveSecurity(ctx context.Context, senderID, token string, approved bool) (string, error) {
	return h.SessionMgr.HandleSecurityDecision(ctx, senderID, "test", token, approved)
}

func (h *TestHarness) ResponseFileForActiveSession(ctx context.Context, senderID, channel string) (string, error) {
	active, err := h.Store.GetActiveWorkspace(ctx, senderID, channel)
	if err != nil {
		return "", err
	}
	activeSession, err := h.Store.GetActiveSessionForWorkspace(ctx, senderID, channel, active.WorkspaceID)
	if err != nil {
		return "", err
	}
	sess, err := h.Store.GetSessionByID(ctx, activeSession.SessionID)
	if err != nil {
		return "", err
	}
	return executor.ResponseFilePath(filepath.Join(h.TempDir, "responses"), sess.TmuxSession), nil
}

func (h *TestHarness) ActiveSession(ctx context.Context, senderID, channel string) (*core.Session, error) {
	active, err := h.Store.GetActiveWorkspace(ctx, senderID, channel)
	if err != nil {
		return nil, err
	}
	activeSession, err := h.Store.GetActiveSessionForWorkspace(ctx, senderID, channel, active.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return h.Store.GetSessionByID(ctx, activeSession.SessionID)
}

func (h *TestHarness) CreateWorkspace(ctx context.Context, name string) (*core.Workspace, error) {
	ws, err := h.Store.CreateWorkspace(ctx, name, filepath.Join(h.TempDir, name), "")
	if err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(map[string]string{"default_agent": "mock"})
	if err := h.Store.UpdateWorkspaceMetadata(ctx, ws.ID, metadata); err != nil {
		return nil, err
	}
	return ws, nil
}

func newHarness(cfg HarnessConfig) (*TestHarness, func(), error) {
	ctx := context.Background()
	tempDir := cfg.TempDir
	if tempDir == "" {
		var err error
		tempDir, err = os.MkdirTemp("", "ai-chat-test-*")
		if err != nil {
			return nil, nil, fmt.Errorf("creating temp dir: %w", err)
		}
	}
	ownedTempDir := cfg.TempDir == ""

	cleanup := func() {
		if ownedTempDir {
			_ = os.RemoveAll(tempDir)
		}
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("opening database: %w", err)
	}

	if err := store.Migrate(db); err != nil {
		_ = db.Close()
		cleanup()
		return nil, cleanup, fmt.Errorf("running migrations: %w", err)
	}

	st := store.New(db)
	mock := NewMockAdapter("mock")
	opencode := NewMockAdapter("opencode")
	registry := &mockRegistry{mock: mock, opencode: opencode}
	responsesDir := filepath.Join(tempDir, "responses")
	if err := os.MkdirAll(responsesDir, 0o755); err != nil {
		_ = db.Close()
		cleanup()
		return nil, cleanup, fmt.Errorf("creating responses dir: %w", err)
	}

	runtime := app.NewRuntimeWithRegistry(st, registry, app.RuntimeConfig{
		ResponsesDir:    responsesDir,
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		ReaperInterval:  5 * time.Minute,
	})
	outbound := NewMockChannel("test")
	runCtx, cancel := context.WithCancel(context.Background())
	wg := app.StartBackground(runCtx, st, runtime.SessionManager, outbound)
	time.Sleep(50 * time.Millisecond)

	ws, err := st.CreateWorkspace(ctx, "test-ws", tempDir, "")
	if err != nil {
		cancel()
		wg.Wait()
		_ = db.Close()
		cleanup()
		return nil, cleanup, fmt.Errorf("creating workspace: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"default_agent": "mock"})
	if err := st.UpdateWorkspaceMetadata(ctx, ws.ID, metadata); err != nil {
		cancel()
		wg.Wait()
		_ = db.Close()
		cleanup()
		return nil, cleanup, fmt.Errorf("updating workspace metadata: %w", err)
	}
	if err := st.SetActiveWorkspace(ctx, "test-sender", "test", ws.ID); err != nil {
		cancel()
		wg.Wait()
		_ = db.Close()
		cleanup()
		return nil, cleanup, fmt.Errorf("setting active workspace: %w", err)
	}

	h := &TestHarness{
		Store:      st,
		Router:     runtime.Router,
		SessionMgr: runtime.SessionManager,
		Mock:       mock,
		Outbound:   outbound,
		TempDir:    tempDir,
		DB:         db,
		Proxy:      runtime.SecurityProxy,
		Cancel:     cancel,
		WG:         wg,
		MCP:        newExecutorSessionManager(runtime.SessionManager, st),
	}

	return h, func() {
		h.Cleanup()
		cleanup()
	}, nil
}

type executorSessionManager struct {
	manager *session.Manager
	store   *store.Store
}

func newExecutorSessionManager(manager *session.Manager, st *store.Store) *executorSessionManager {
	return &executorSessionManager{manager: manager, store: st}
}

func (e *executorSessionManager) CreateSession(ctx context.Context, ws core.Workspace, agent string) (*core.Session, error) {
	info, err := e.manager.CreateSession(ctx, ws.ID, agent)
	if err != nil {
		return nil, err
	}
	return e.store.GetSessionByTmuxSession(ctx, info.Name)
}

func (e *executorSessionManager) ClearSession(ctx context.Context, sessionID int64) (*core.Session, error) {
	info, err := e.manager.ClearSessionByID(ctx, "mcp", "system", sessionID)
	if err != nil {
		return nil, err
	}
	return e.store.GetSessionByTmuxSession(ctx, info.Name)
}

func (e *executorSessionManager) KillSession(ctx context.Context, sessionID int64) error {
	return e.manager.KillSessionByID(ctx, "mcp", "system", sessionID)
}

func (e *executorSessionManager) Send(ctx context.Context, sessionID int64, message string) error {
	return e.manager.SendToSession(ctx, "mcp", "system", sessionID, message)
}

func (e *executorSessionManager) ApproveSend(ctx context.Context, pendingID string, approved bool) (string, error) {
	return e.manager.HandleSecurityDecision(ctx, "mcp", "system", pendingID, approved)
}

func (e *executorSessionManager) SwitchSession(ctx context.Context, workspaceID, sessionID int64) (*core.Session, error) {
	if err := e.store.SetActiveWorkspace(ctx, "mcp", "system", workspaceID); err != nil {
		return nil, err
	}
	if err := e.manager.SwitchActiveSession(ctx, "mcp", "system", workspaceID, sessionID); err != nil {
		return nil, err
	}
	return e.store.GetSessionByID(ctx, sessionID)
}

var _ mcppkg.SessionManager = (*executorSessionManager)(nil)

type mockRegistry struct {
	mock     *MockAdapter
	opencode *MockAdapter
}

func (r *mockRegistry) GetAdapter(agent string) (executor.AgentAdapter, error) {
	if agent == r.mock.Name() {
		return r.mock, nil
	}
	if agent == "opencode" && r.opencode != nil {
		return r.opencode, nil
	}
	return nil, fmt.Errorf("unknown agent: %q", agent)
}

func (r *mockRegistry) KnownAgents() []string {
	agents := []string{r.mock.Name()}
	if r.opencode != nil {
		agents = append(agents, "opencode")
	}
	return agents
}
