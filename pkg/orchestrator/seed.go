package orchestrator

import (
	"context"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// SeedDefaults inserts default model configs if the table is empty.
func SeedDefaults(ctx context.Context, st *store.Store) error {
	existing, err := st.ListModelConfigs(ctx)
	if err != nil {
		return fmt.Errorf("seed: listing configs: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	defaults := []store.ModelConfig{
		{
			Role:     "router",
			Provider: "https://openrouter.ai/api/v1",
			Model:    "google/gemini-3.1-flash-lite-preview",
			Metadata: `{"threshold": 0.7}`,
		},
		{
			Role:     "second_opinion",
			Provider: "https://openrouter.ai/api/v1",
			Model:    "minimax/minimax-m2.5",
			Metadata: "{}",
		},
	}

	for _, cfg := range defaults {
		if err := st.SetModelConfig(ctx, cfg); err != nil {
			return fmt.Errorf("seed: setting %q: %w", cfg.Role, err)
		}
	}
	return nil
}
