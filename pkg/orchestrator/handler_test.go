package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// e2eTracker tracks requests and returns canned responses based on message content.
type e2eTracker struct {
	mu       sync.Mutex
	requests int
}

func (et *e2eTracker) count() int {
	et.mu.Lock()
	defer et.mu.Unlock()
	return et.requests
}

func setupE2E(t *testing.T, handler http.HandlerFunc) (*Orchestrator, *store.Store) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st := store.New(db)
	router := NewRouter("test-key").WithBaseURL(srv.URL)
	orch := NewOrchestrator(router, st, "test/primary", "test/second")
	return orch, st
}

func respondWithAction(w http.ResponseWriter, action Action) {
	actionJSON, _ := json.Marshal(action)
	_ = json.NewEncoder(w).Encode(ChatResponse{
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{
			{Message: struct {
				Content string `json:"content"`
			}{Content: string(actionJSON)}},
		},
	})
}

func TestE2E_WorkspaceSwitch(t *testing.T) {
	tracker := &e2eTracker{}
	orch, st := setupE2E(t, func(w http.ResponseWriter, r *http.Request) {
		tracker.mu.Lock()
		tracker.requests++
		tracker.mu.Unlock()

		respondWithAction(w, Action{
			Type:       ActionWorkspaceSwitch,
			Workspace:  "laboratory",
			Confidence: 0.95,
			Reasoning:  "clear switch request",
		})
	})

	ctx := context.Background()
	_, _ = st.CreateWorkspace(ctx, "laboratory", "/home/user/lab", "")
	_, _ = st.CreateWorkspace(ctx, "fitness-tracker", "/home/user/fitness", "")

	result, err := orch.HandleMessage(ctx, core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "switch to laboratory",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action.Type != ActionWorkspaceSwitch {
		t.Errorf("action type = %q, want %q", result.Action.Type, ActionWorkspaceSwitch)
	}
	if !strings.Contains(result.Response, "Switched to laboratory") {
		t.Errorf("response = %q, should contain 'Switched to laboratory'", result.Response)
	}

	// Verify user context updated in store
	uc, err := st.GetUserContext(ctx, "user1", "telegram")
	if err != nil {
		t.Fatalf("GetUserContext: %v", err)
	}
	if uc.ActiveWorkspaceID == 0 {
		t.Error("active workspace should be set after switch")
	}
}

func TestE2E_StatusQuery(t *testing.T) {
	orch, st := setupE2E(t, func(w http.ResponseWriter, r *http.Request) {
		respondWithAction(w, Action{
			Type:       ActionStatus,
			Confidence: 0.9,
			Reasoning:  "status query",
		})
	})

	ctx := context.Background()
	ws, _ := st.CreateWorkspace(ctx, "laboratory", "/home/user/lab", "")
	_ = st.SetActiveWorkspace(ctx, "user1", "telegram", ws.ID)

	result, err := orch.HandleMessage(ctx, core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "what's the status",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Response, "laboratory") {
		t.Errorf("response should contain workspace name, got: %s", result.Response)
	}
}

func TestE2E_AgentTask(t *testing.T) {
	orch, _ := setupE2E(t, func(w http.ResponseWriter, r *http.Request) {
		respondWithAction(w, Action{
			Type:       ActionAgentTask,
			Workspace:  "laboratory",
			Content:    "fix the bug in auth.go",
			Confidence: 0.85,
			Reasoning:  "code task",
		})
	})

	result, err := orch.HandleMessage(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "fix the bug in auth.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action.Type != ActionAgentTask {
		t.Errorf("action type = %q, want %q", result.Action.Type, ActionAgentTask)
	}
	if result.Response != "" {
		t.Errorf("response should be empty for agent_task, got %q", result.Response)
	}
	if result.Action.Content != "fix the bug in auth.go" {
		t.Errorf("action content = %q, want 'fix the bug in auth.go'", result.Action.Content)
	}
}

func TestE2E_SecondOpinionConsulted(t *testing.T) {
	tracker := &e2eTracker{}
	orch, _ := setupE2E(t, func(w http.ResponseWriter, r *http.Request) {
		tracker.mu.Lock()
		callNum := tracker.requests
		tracker.requests++
		tracker.mu.Unlock()

		if callNum == 0 {
			// First call: low confidence
			respondWithAction(w, Action{
				Type:       ActionAgentTask,
				Content:    "maybe agent",
				Confidence: 0.4,
				Reasoning:  "unsure",
			})
		} else {
			// Second call: higher confidence
			respondWithAction(w, Action{
				Type:       ActionDirectAnswer,
				Content:    "the answer is clear",
				Confidence: 0.8,
				Reasoning:  "confident answer",
			})
		}
	})

	result, err := orch.HandleMessage(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "ambiguous question",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tracker.count() != 2 {
		t.Errorf("expected 2 requests (primary + second opinion), got %d", tracker.count())
	}
	if result.Action.Type != ActionDirectAnswer {
		t.Errorf("action type = %q, want %q (second opinion should win)", result.Action.Type, ActionDirectAnswer)
	}
	if result.Response != "the answer is clear" {
		t.Errorf("response = %q, want 'the answer is clear'", result.Response)
	}
}

func TestSetup_SeedsDefaults(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	st := store.New(db)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondWithAction(w, Action{
			Type:       ActionDirectAnswer,
			Content:    "ok",
			Confidence: 0.9,
		})
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)

	orch, err := Setup(router, st)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Verify defaults were seeded
	configs, err := st.ListModelConfigs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 default configs, got %d", len(configs))
	}

	// Verify orchestrator is functional
	result, err := orch.HandleMessage(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "hello",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}
