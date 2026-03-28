package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// modelTracker records which models were requested and returns per-model responses.
type modelTracker struct {
	mu       sync.Mutex
	called   []string
	respond  map[string]Action // model -> action to return
	failFor  map[string]bool   // model -> return error
}

func newModelTracker() *modelTracker {
	return &modelTracker{
		respond: make(map[string]Action),
		failFor: make(map[string]bool),
	}
}

func (mt *modelTracker) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mt.mu.Lock()
		mt.called = append(mt.called, req.Model)
		mt.mu.Unlock()

		if mt.failFor[req.Model] {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("model error"))
			return
		}

		action, ok := mt.respond[req.Model]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("no response configured for model"))
			return
		}

		actionJSON, _ := json.Marshal(action)
		_, _ = w.Write(chatResponseJSON(string(actionJSON)))
	}
}

func (mt *modelTracker) calledModels() []string {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	result := make([]string, len(mt.called))
	copy(result, mt.called)
	return result
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return store.New(db)
}

func TestClassify_HighConfidence_NoSecondOpinion(t *testing.T) {
	mt := newModelTracker()
	mt.respond["primary/model"] = Action{
		Type:       ActionAgentTask,
		Content:    "do the work",
		Confidence: 0.9,
		Reasoning:  "clear task",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	orch := NewOrchestrator(router, st, "primary/model", "second/model")

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "fix the tests",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Type != ActionAgentTask {
		t.Errorf("type = %q, want %q", action.Type, ActionAgentTask)
	}

	called := mt.calledModels()
	if len(called) != 1 {
		t.Errorf("expected 1 model call, got %d: %v", len(called), called)
	}
	if called[0] != "primary/model" {
		t.Errorf("expected primary model call, got %q", called[0])
	}
}

func TestClassify_LowConfidence_SecondOpinionWins(t *testing.T) {
	mt := newModelTracker()
	mt.respond["primary/model"] = Action{
		Type:       ActionAgentTask,
		Content:    "maybe agent",
		Confidence: 0.5,
		Reasoning:  "unsure",
	}
	mt.respond["second/model"] = Action{
		Type:       ActionDirectAnswer,
		Content:    "this is a direct answer",
		Confidence: 0.8,
		Reasoning:  "confident direct",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	orch := NewOrchestrator(router, st, "primary/model", "second/model")

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "what time is it?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Type != ActionDirectAnswer {
		t.Errorf("type = %q, want %q", action.Type, ActionDirectAnswer)
	}
	if action.Confidence != 0.8 {
		t.Errorf("confidence = %f, want 0.8", action.Confidence)
	}

	called := mt.calledModels()
	if len(called) != 2 {
		t.Fatalf("expected 2 model calls, got %d: %v", len(called), called)
	}
}

func TestClassify_LowConfidence_PrimaryWins(t *testing.T) {
	mt := newModelTracker()
	mt.respond["primary/model"] = Action{
		Type:       ActionAgentTask,
		Content:    "probably agent",
		Confidence: 0.5,
		Reasoning:  "unsure",
	}
	mt.respond["second/model"] = Action{
		Type:       ActionStatus,
		Content:    "hmm status",
		Confidence: 0.3,
		Reasoning:  "even less sure",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	orch := NewOrchestrator(router, st, "primary/model", "second/model")

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "ambiguous message",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Type != ActionAgentTask {
		t.Errorf("type = %q, want %q (primary should win)", action.Type, ActionAgentTask)
	}
}

func TestClassify_PrimaryError(t *testing.T) {
	mt := newModelTracker()
	mt.failFor["primary/model"] = true

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	orch := NewOrchestrator(router, st, "primary/model", "second/model")

	_, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "test",
	})
	if err == nil {
		t.Fatal("expected error from primary model failure")
	}
	if !strings.Contains(err.Error(), "primary classification") {
		t.Errorf("error %q should contain 'primary classification'", err.Error())
	}
}

func TestClassify_SecondOpinionError_FallbackToPrimary(t *testing.T) {
	mt := newModelTracker()
	mt.respond["primary/model"] = Action{
		Type:       ActionAgentTask,
		Content:    "low confidence task",
		Confidence: 0.4,
		Reasoning:  "not sure",
	}
	mt.failFor["second/model"] = true

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	orch := NewOrchestrator(router, st, "primary/model", "second/model")

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "do something",
	})
	if err != nil {
		t.Fatalf("should not return error on second opinion failure: %v", err)
	}
	if action.Type != ActionAgentTask {
		t.Errorf("should fall back to primary, got type %q", action.Type)
	}
}

func TestClassify_NoUserContext(t *testing.T) {
	mt := newModelTracker()
	mt.respond["primary/model"] = Action{
		Type:       ActionMetaCommand,
		Content:    "list workspaces",
		Confidence: 0.85,
		Reasoning:  "meta command",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	// Don't set up any user context - should work with nil workspace
	orch := NewOrchestrator(router, st, "primary/model", "second/model")

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "newuser",
		Channel:  "telegram",
		Content:  "list workspaces",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Type != ActionMetaCommand {
		t.Errorf("type = %q, want %q", action.Type, ActionMetaCommand)
	}
}

func TestClassify_WithThreshold(t *testing.T) {
	mt := newModelTracker()
	mt.respond["primary/model"] = Action{
		Type:       ActionAgentTask,
		Content:    "task",
		Confidence: 0.6,
		Reasoning:  "moderate confidence",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	orch := NewOrchestrator(router, st, "primary/model", "second/model").WithThreshold(0.5)

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "do work",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Type != ActionAgentTask {
		t.Errorf("type = %q, want %q", action.Type, ActionAgentTask)
	}

	// With threshold 0.5, confidence 0.6 should not trigger second opinion
	called := mt.calledModels()
	if len(called) != 1 {
		t.Errorf("expected 1 model call (no second opinion with threshold 0.5), got %d: %v", len(called), called)
	}
}

func TestClassify_WithActiveWorkspace(t *testing.T) {
	mt := newModelTracker()
	mt.respond["primary/model"] = Action{
		Type:       ActionAgentTask,
		Content:    "fix it",
		Confidence: 0.9,
		Reasoning:  "clear task in workspace",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)

	// Set up a workspace and user context
	ws, err := st.CreateWorkspace(context.Background(), "myproject", "/home/user/myproject", "")
	if err != nil {
		t.Fatal(err)
	}
	err = st.SetActiveWorkspace(context.Background(), "user1", "telegram", ws.ID)
	if err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(router, st, "primary/model", "second/model")

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "fix the bug",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = action
	_ = fmt.Sprintf("action: %+v", action) // verify it doesn't panic
}

func TestClassify_UsesCachedModels(t *testing.T) {
	// Set up model configs in the store so the cache loads them
	mt := newModelTracker()
	mt.respond["cached/primary"] = Action{
		Type:       ActionDirectAnswer,
		Content:    "from cached model",
		Confidence: 0.95,
		Reasoning:  "cached",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)

	// Seed model configs pointing to "cached/primary" and "cached/second"
	if err := st.SetModelConfig(context.Background(), store.ModelConfig{
		Role:     "router",
		Provider: "https://openrouter.ai/api/v1",
		Model:    "cached/primary",
		Metadata: `{"threshold": 0.8}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetModelConfig(context.Background(), store.ModelConfig{
		Role:     "second_opinion",
		Provider: "https://openrouter.ai/api/v1",
		Model:    "cached/second",
		Metadata: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	// Constructor uses different model names, but cache should override
	orch := NewOrchestrator(router, st, "constructor/primary", "constructor/second")

	action, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Content != "from cached model" {
		t.Errorf("expected cached model response, got %q", action.Content)
	}

	// Verify the cached model was called, not the constructor model
	called := mt.calledModels()
	if len(called) != 1 || called[0] != "cached/primary" {
		t.Errorf("expected call to 'cached/primary', got %v", called)
	}
}

func TestSeedDefaults(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := SeedDefaults(ctx, st); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	configs, err := st.ListModelConfigs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 default configs, got %d", len(configs))
	}
}

func TestSeedDefaults_Idempotent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Seed custom config first
	if err := st.SetModelConfig(ctx, store.ModelConfig{
		Role:     "router",
		Provider: "custom",
		Model:    "custom/model",
		Metadata: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	// SeedDefaults should not overwrite
	if err := SeedDefaults(ctx, st); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	cfg, err := st.GetModelConfig(ctx, "router")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "custom/model" {
		t.Errorf("model = %q, want 'custom/model' (seed should not overwrite)", cfg.Model)
	}
}

func TestThresholdFromMetadata(t *testing.T) {
	mt := newModelTracker()
	mt.respond["cached/primary"] = Action{
		Type:       ActionAgentTask,
		Content:    "task",
		Confidence: 0.55,
		Reasoning:  "moderate",
	}
	mt.respond["cached/second"] = Action{
		Type:       ActionAgentTask,
		Content:    "task2",
		Confidence: 0.6,
		Reasoning:  "slightly better",
	}

	srv := httptest.NewServer(mt.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)

	// Set threshold to 0.5 via metadata
	if err := st.SetModelConfig(context.Background(), store.ModelConfig{
		Role:     "router",
		Provider: "https://openrouter.ai/api/v1",
		Model:    "cached/primary",
		Metadata: `{"threshold": 0.5}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetModelConfig(context.Background(), store.ModelConfig{
		Role:     "second_opinion",
		Provider: "https://openrouter.ai/api/v1",
		Model:    "cached/second",
		Metadata: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(router, st, "fallback/primary", "fallback/second")

	_, err := orch.Classify(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "telegram",
		Content:  "do work",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With threshold 0.5 and confidence 0.55, second opinion should NOT be triggered
	called := mt.calledModels()
	if len(called) != 1 {
		t.Errorf("expected 1 call with cached threshold 0.5, got %d: %v", len(called), called)
	}
}

func TestCacheIsStale(t *testing.T) {
	cache := newModelCache(0) // TTL of 0 means always stale
	if !cache.isStale() {
		t.Error("new cache should be stale")
	}

	cache.refresh([]store.ModelConfig{
		{Role: "router", Model: "test"},
	})

	// With TTL of 0, cache is immediately stale again
	if !cache.isStale() {
		t.Error("cache with 0 TTL should always be stale after refresh")
	}
}

func TestCacheRefresh(t *testing.T) {
	cache := newModelCache(defaultCacheTTL)

	cache.refresh([]store.ModelConfig{
		{Role: "router", Model: "model-a"},
		{Role: "second_opinion", Model: "model-b"},
	})

	cfg, ok := cache.get("router")
	if !ok {
		t.Fatal("expected router config in cache")
	}
	if cfg.Model != "model-a" {
		t.Errorf("model = %q, want 'model-a'", cfg.Model)
	}

	cfg, ok = cache.get("second_opinion")
	if !ok {
		t.Fatal("expected second_opinion config in cache")
	}
	if cfg.Model != "model-b" {
		t.Errorf("model = %q, want 'model-b'", cfg.Model)
	}

	_, ok = cache.get("nonexistent")
	if ok {
		t.Error("should not find nonexistent role")
	}
}
