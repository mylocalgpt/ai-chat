package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/session"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type TestRunner struct {
	Verbose  bool
	Scenario string
}

func NewTestRunner(verbose bool, scenario string) *TestRunner {
	return &TestRunner{
		Verbose:  verbose,
		Scenario: scenario,
	}
}

func (r *TestRunner) Run() *TestReport {
	report := NewTestReport(r.Verbose)

	scenarios := Scenarios
	if r.Scenario != "" {
		scenarios = nil
		for _, s := range Scenarios {
			if strings.EqualFold(s.Name, r.Scenario) {
				scenarios = []Scenario{s}
				break
			}
		}
		if len(scenarios) == 0 {
			report.Add(r.Scenario, false, 0, fmt.Sprintf("unknown scenario: %s", r.Scenario))
			report.End()
			return report
		}
	}

	for _, s := range scenarios {
		start := time.Now()
		err := r.runScenario(s)
		dur := time.Since(start)

		errStr := ""
		passed := err == nil
		if err != nil {
			errStr = err.Error()
		}

		report.Add(s.Name, passed, dur, errStr)
	}

	report.End()
	return report
}

func (r *TestRunner) runScenario(s Scenario) error {
	harness, cleanup, err := NewStandaloneHarness()
	if err != nil {
		return fmt.Errorf("creating harness: %w", err)
	}
	defer cleanup()

	var scenarioErr error
	t := &scenarioT{verbose: r.Verbose}

	defer func() {
		if rec := recover(); rec != nil {
			if errStr, ok := rec.(string); ok && errStr == "test failed" {
			} else {
				scenarioErr = fmt.Errorf("panic: %v", rec)
			}
		}
	}()

	s.Run(t, harness)
	if t.failed {
		scenarioErr = fmt.Errorf("%s", t.errorMsg)
	}

	return scenarioErr
}

type scenarioT struct {
	failed   bool
	errorMsg string
	verbose  bool
}

func (t *scenarioT) Fatalf(format string, args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprintf(format, args...)
	panic("test failed")
}

func (t *scenarioT) Errorf(format string, args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprintf(format, args...)
}

func (t *scenarioT) Error(args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprint(args...)
}

func (t *scenarioT) Fatal(args ...any) {
	t.failed = true
	t.errorMsg = fmt.Sprint(args...)
	panic("test failed")
}

func (t *scenarioT) Fail() {
	t.failed = true
}

func (t *scenarioT) FailNow() {
	t.failed = true
	panic("test failed")
}

func (t *scenarioT) Failed() bool {
	return t.failed
}

func (t *scenarioT) Log(args ...any) {
	if t.verbose {
		fmt.Println(args...)
	}
}

func (t *scenarioT) Logf(format string, args ...any) {
	if t.verbose {
		fmt.Printf(format+"\n", args...)
	}
}

func (t *scenarioT) TempDir() string {
	dir, err := os.MkdirTemp("", "ai-chat-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	return dir
}

func (t *scenarioT) Run(name string, f func(T)) bool {
	if t.failed {
		return false
	}

	subT := &scenarioT{verbose: t.verbose}
	defer func() {
		if rec := recover(); rec != nil {
			if rec != "test failed" {
				t.failed = true
				t.errorMsg = fmt.Sprintf("panic in subtest %s: %v", name, rec)
			}
		}
	}()

	f(subT)
	if subT.failed {
		t.failed = true
		t.errorMsg = subT.errorMsg
		return false
	}
	return true
}

type T interface {
	Fatalf(format string, args ...any)
	Fatal(args ...any)
	Errorf(format string, args ...any)
	Error(args ...any)
	Fail()
	FailNow()
	Failed() bool
	Log(args ...any)
	Logf(format string, args ...any)
	TempDir() string
	Run(name string, f func(T)) bool
}

func NewStandaloneHarness() (*TestHarness, func(), error) {
	ctx := context.Background()

	tempDir, err := os.MkdirTemp("", "ai-chat-test-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating temp dir: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("opening database: %w", err)
	}

	if err := store.Migrate(db); err != nil {
		_ = db.Close()
		cleanup()
		return nil, nil, fmt.Errorf("running migrations: %w", err)
	}

	st := store.New(db)

	mock := NewMockAdapter("mock")

	registry := &standaloneRegistry{mock: mock}

	proxy := executor.NewSecurityProxy()

	responsesDir := filepath.Join(tempDir, "responses")
	if err := os.MkdirAll(responsesDir, 0o755); err != nil {
		_ = db.Close()
		cleanup()
		return nil, nil, fmt.Errorf("creating responses dir: %w", err)
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
		cleanup()
		return nil, nil, fmt.Errorf("creating workspace: %w", err)
	}

	metadata, _ := json.Marshal(map[string]string{"default_agent": "mock"})
	if err := st.UpdateWorkspaceMetadata(ctx, ws.ID, metadata); err != nil {
		_ = db.Close()
		cleanup()
		return nil, nil, fmt.Errorf("updating workspace metadata: %w", err)
	}

	_, err = db.ExecContext(ctx,
		"INSERT INTO user_context (sender_id, channel, active_workspace_id, updated_at) VALUES (?, ?, ?, ?)",
		"test-sender", "test", ws.ID, time.Now().UTC(),
	)
	if err != nil {
		_ = db.Close()
		cleanup()
		return nil, nil, fmt.Errorf("creating user context: %w", err)
	}

	harness := &TestHarness{
		Store:      st,
		Router:     r,
		SessionMgr: sessMgr,
		Mock:       mock,
		TempDir:    tempDir,
		DB:         db,
		Proxy:      proxy,
	}

	return harness, func() {
		harness.Cleanup()
		cleanup()
	}, nil
}

type standaloneRegistry struct {
	mock *MockAdapter
}

func (r *standaloneRegistry) GetAdapter(agent string) (executor.AgentAdapter, error) {
	if agent == r.mock.Name() {
		return r.mock, nil
	}
	return nil, fmt.Errorf("unknown agent: %q", agent)
}

func (r *standaloneRegistry) KnownAgents() []string {
	return []string{r.mock.Name()}
}

func (h *TestHarness) SendMessageStandalone(ctx context.Context, senderID, content string) (string, error) {
	msg := core.InboundMessage{
		SenderID: senderID,
		Channel:  "test",
		Content:  content,
	}
	resp, err := h.Router.Route(ctx, msg)
	if err != nil {
		return "", err
	}

	if resp != "" {
		return resp, nil
	}

	uc, err := h.Store.GetUserContext(ctx, senderID, "test")
	if err != nil {
		return "", nil
	}
	if uc.ActiveSessionID == nil {
		return "", nil
	}

	sess, err := h.Store.GetSessionByID(ctx, *uc.ActiveSessionID)
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
