package testing

import (
	"context"
	"database/sql"
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

	registry := &mockRegistry{mock: mock}

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

	_, err = db.ExecContext(ctx,
		"INSERT INTO user_contexts (sender_id, channel, active_workspace_id, updated_at) VALUES (?, ?, ?, ?)",
		"test-sender", "test", ws.ID, time.Now().UTC(),
	)
	if err != nil {
		_ = db.Close()
		t.Fatalf("creating user context: %v", err)
	}

	return &TestHarness{
		Store:      st,
		Router:     r,
		SessionMgr: sessMgr,
		Mock:       mock,
		TempDir:    tempDir,
		DB:         db,
	}
}

func (h *TestHarness) SendMessage(ctx context.Context, senderID, content string) (string, error) {
	msg := core.InboundMessage{
		SenderID: senderID,
		Channel:  "test",
		Content:  content,
	}
	return h.Router.Route(ctx, msg)
}

func (h *TestHarness) Cleanup() {
	if h.DB != nil {
		_ = h.DB.Close()
	}
}

type mockRegistry struct {
	mock *MockAdapter
}

func (r *mockRegistry) GetAdapter(agent string) (executor.AgentAdapter, error) {
	if agent == r.mock.Name() {
		return r.mock, nil
	}
	return nil, fmt.Errorf("unknown agent: %q", agent)
}

func (r *mockRegistry) KnownAgents() []string {
	return []string{r.mock.Name()}
}
