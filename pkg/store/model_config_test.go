package store

import (
	"context"
	"errors"
	"testing"
)

func TestModelConfig_SetAndGet(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	cfg := ModelConfig{
		Role:     "router",
		Provider: "https://openrouter.ai/api/v1",
		Model:    "google/gemini-flash",
		Metadata: `{"threshold": 0.7}`,
	}

	if err := st.SetModelConfig(ctx, cfg); err != nil {
		t.Fatalf("SetModelConfig: %v", err)
	}

	got, err := st.GetModelConfig(ctx, "router")
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	if got.Role != "router" {
		t.Errorf("role = %q, want 'router'", got.Role)
	}
	if got.Model != "google/gemini-flash" {
		t.Errorf("model = %q, want 'google/gemini-flash'", got.Model)
	}
	if got.Metadata != `{"threshold": 0.7}` {
		t.Errorf("metadata = %q, want threshold json", got.Metadata)
	}
}

func TestModelConfig_GetNotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.GetModelConfig(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error should wrap ErrNotFound, got: %v", err)
	}
}

func TestModelConfig_Upsert(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	cfg := ModelConfig{
		Role:     "router",
		Provider: "https://openrouter.ai/api/v1",
		Model:    "old-model",
		Metadata: "{}",
	}
	if err := st.SetModelConfig(ctx, cfg); err != nil {
		t.Fatalf("first SetModelConfig: %v", err)
	}

	cfg.Model = "new-model"
	if err := st.SetModelConfig(ctx, cfg); err != nil {
		t.Fatalf("second SetModelConfig: %v", err)
	}

	got, err := st.GetModelConfig(ctx, "router")
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	if got.Model != "new-model" {
		t.Errorf("model = %q, want 'new-model'", got.Model)
	}
}

func TestModelConfig_List(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	configs := []ModelConfig{
		{Role: "router", Provider: "https://openrouter.ai/api/v1", Model: "model-a", Metadata: "{}"},
		{Role: "second_opinion", Provider: "https://openrouter.ai/api/v1", Model: "model-b", Metadata: "{}"},
	}
	for _, cfg := range configs {
		if err := st.SetModelConfig(ctx, cfg); err != nil {
			t.Fatalf("SetModelConfig: %v", err)
		}
	}

	got, err := st.ListModelConfigs(ctx)
	if err != nil {
		t.Fatalf("ListModelConfigs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(got))
	}
}

func TestModelConfig_ListEmpty(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	got, err := st.ListModelConfigs(ctx)
	if err != nil {
		t.Fatalf("ListModelConfigs: %v", err)
	}
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 configs, got %d", len(got))
	}
}
