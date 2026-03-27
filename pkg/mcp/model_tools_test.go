package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/store"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConfigSetModelOrchestrator(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	res, _, err := srv.handleConfigSetModel(context.Background(), &gomcp.CallToolRequest{}, ConfigSetModelInput{
		Role:     "orchestrator",
		Provider: "openrouter",
		Model:    "google/gemini-2.0-flash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected result")
	}
	if len(ms.modelConfigs) != 1 {
		t.Fatalf("expected 1 model config, got %d", len(ms.modelConfigs))
	}
	if ms.modelConfigs[0].Role != "orchestrator" {
		t.Errorf("expected role 'orchestrator', got %q", ms.modelConfigs[0].Role)
	}
}

func TestConfigSetModelWorker(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleConfigSetModel(context.Background(), &gomcp.CallToolRequest{}, ConfigSetModelInput{
		Role:     "worker",
		Provider: "openrouter",
		Model:    "anthropic/claude-3-haiku",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ms.modelConfigs) != 1 || ms.modelConfigs[0].Role != "worker" {
		t.Error("expected worker model config to be stored")
	}
}

func TestConfigSetModelInvalidRole(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleConfigSetModel(context.Background(), &gomcp.CallToolRequest{}, ConfigSetModelInput{
		Role:     "admin",
		Provider: "openrouter",
		Model:    "some-model",
	})
	if err == nil {
		t.Error("expected error for invalid role")
	}
}

func TestConfigSetModelEmptyProvider(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleConfigSetModel(context.Background(), &gomcp.CallToolRequest{}, ConfigSetModelInput{
		Role:  "orchestrator",
		Model: "some-model",
	})
	if err == nil {
		t.Error("expected error for empty provider")
	}
}

func TestConfigSetModelEmptyModel(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	_, _, err := srv.handleConfigSetModel(context.Background(), &gomcp.CallToolRequest{}, ConfigSetModelInput{
		Role:     "orchestrator",
		Provider: "openrouter",
	})
	if err == nil {
		t.Error("expected error for empty model")
	}
}

func TestConfigGetModels(t *testing.T) {
	ms := newMockStore()
	ms.modelConfigs = []store.ModelConfig{
		{Role: "orchestrator", Provider: "openrouter", Model: "gemini-flash"},
		{Role: "worker", Provider: "openrouter", Model: "claude-haiku"},
	}
	srv := newTestServer(ms, nil)

	res, _, err := srv.handleConfigGetModels(context.Background(), &gomcp.CallToolRequest{}, ConfigGetModelsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var entries []ModelConfigEntry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Role != "orchestrator" {
		t.Errorf("expected first entry role 'orchestrator', got %q", entries[0].Role)
	}
}

func TestConfigGetModelsEmpty(t *testing.T) {
	ms := newMockStore()
	srv := newTestServer(ms, nil)

	res, _, err := srv.handleConfigGetModels(context.Background(), &gomcp.CallToolRequest{}, ConfigGetModelsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var entries []ModelConfigEntry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}
}
