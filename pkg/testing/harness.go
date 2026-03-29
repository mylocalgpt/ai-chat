package testing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/session"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type TestHarness struct {
	Store      *store.Store
	Router     *router.Router
	SessionMgr *session.Manager
	Mock       *MockAdapter
	TempDir    string
	DB         *sql.DB
	Proxy      *executor.SecurityProxy
}

func NewTestHarness(t interface {
	Fatalf(format string, args ...any)
	TempDir() string
}) *TestHarness {
	ctx := context.Background()

	tempDir := t.TempDir()

	dbPath := tempDir + "/test.db"
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}

	if err := store.Migrate(db); err != nil {
		_ = db.Close()
		t.Fatalf("running migrations: %v", err)
	}

	st := store.New(db)

	mock := NewMockAdapter("mock")
	opencode := NewMockAdapter("opencode")

	registry := &mockRegistry{mock: mock, opencode: opencode}

	proxy := executor.NewSecurityProxy()

	responsesDir := tempDir + "/responses"
	if err := os.MkdirAll(responsesDir, 0o755); err != nil {
		_ = db.Close()
		t.Fatalf("creating responses dir: %v", err)
	}

	sessMgr := session.NewManager(st, registry, proxy, session.ManagerConfig{
		ResponsesDir:    responsesDir,
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		ReaperInterval:  5 * time.Minute,
	})

	r := router.NewRouter(st, sessMgr)

	ws, err := st.CreateWorkspace(ctx, "test-ws", tempDir, "")
	if err != nil {
		_ = db.Close()
		t.Fatalf("creating workspace: %v", err)
	}

	metadata, _ := json.Marshal(map[string]string{"default_agent": "mock"})
	if err := st.UpdateWorkspaceMetadata(ctx, ws.ID, metadata); err != nil {
		_ = db.Close()
		t.Fatalf("updating workspace metadata: %v", err)
	}

	if err := st.SetActiveWorkspace(ctx, "test-sender", "test", ws.ID); err != nil {
		_ = db.Close()
		t.Fatalf("setting active workspace: %v", err)
	}

	return &TestHarness{
		Store:      st,
		Router:     r,
		SessionMgr: sessMgr,
		Mock:       mock,
		TempDir:    tempDir,
		DB:         db,
		Proxy:      proxy,
	}
}

func (h *TestHarness) SendMessage(ctx context.Context, senderID, content string) (string, error) {
	msg := core.InboundMessage{
		SenderID: senderID,
		Channel:  "test",
		Content:  content,
	}
	result, err := h.Router.Route(ctx, router.Request{Message: &msg})
	if err != nil {
		return "", err
	}

	if rendered := renderResultForTest(result); rendered != "" {
		return rendered, nil
	}

	active, err := h.Store.GetActiveWorkspace(ctx, senderID, "test")
	if err != nil {
		return "", nil
	}

	activeSession, err := h.Store.GetActiveSessionForWorkspace(ctx, senderID, "test", active.WorkspaceID)
	if err != nil {
		return "", nil
	}

	sess, err := h.Store.GetSessionByID(ctx, activeSession.SessionID)
	if err != nil {
		return "", nil
	}

	responsesDir := h.TempDir + "/responses"
	responseFile := executor.ResponseFilePath(responsesDir, sess.TmuxSession)

	content, err = executor.LatestAgentMessage(responseFile)
	if err != nil {
		return "", err
	}

	return content, nil
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
	if h.DB != nil {
		_ = h.DB.Close()
	}
}

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
