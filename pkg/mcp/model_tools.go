package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/store"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Input/Output structs ---

// ConfigSetModelInput is the input for the config_set_model tool.
type ConfigSetModelInput struct {
	Role     string `json:"role" jsonschema:"Model role (orchestrator or worker)"`
	Provider string `json:"provider" jsonschema:"Provider URL (e.g. OpenRouter endpoint)"`
	Model    string `json:"model" jsonschema:"Model name (e.g. google/gemini-2.0-flash-lite-001)"`
}

// ConfigGetModelsInput is empty; config_get_models has no parameters.
type ConfigGetModelsInput struct{}

// ModelConfigEntry is the JSON representation returned by config_get_models.
type ModelConfigEntry struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// --- Registration ---

func (s *Server) registerModelTools() {
	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "config_set_model",
		Description: "Set the AI model for a given role (orchestrator or worker)",
	}, s.handleConfigSetModel)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "config_get_models",
		Description: "List current model assignments per role",
	}, s.handleConfigGetModels)
}

var _ = (*Server)(nil).registerModelTools

// --- Handlers ---

func (s *Server) handleConfigSetModel(ctx context.Context, _ *gomcp.CallToolRequest, input ConfigSetModelInput) (*gomcp.CallToolResult, any, error) {
	if input.Role != "orchestrator" && input.Role != "worker" {
		return nil, nil, fmt.Errorf("invalid role %q: must be orchestrator or worker", input.Role)
	}
	if input.Provider == "" {
		return nil, nil, fmt.Errorf("provider is required")
	}
	if input.Model == "" {
		return nil, nil, fmt.Errorf("model is required")
	}

	if err := s.store.SetModelConfig(ctx, store.ModelConfig{
		Role:     input.Role,
		Provider: input.Provider,
		Model:    input.Model,
	}); err != nil {
		return nil, nil, fmt.Errorf("setting model config: %w", err)
	}

	return textResult(fmt.Sprintf("Set model for role %s: %s via %s", input.Role, input.Model, input.Provider)), nil, nil
}

func (s *Server) handleConfigGetModels(ctx context.Context, _ *gomcp.CallToolRequest, _ ConfigGetModelsInput) (*gomcp.CallToolResult, any, error) {
	configs, err := s.store.ListModelConfigs(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing model configs: %w", err)
	}

	entries := make([]ModelConfigEntry, len(configs))
	for i, cfg := range configs {
		entries[i] = ModelConfigEntry{
			Role:     cfg.Role,
			Provider: cfg.Provider,
			Model:    cfg.Model,
		}
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling model configs: %w", err)
	}

	return textResult(string(data)), nil, nil
}
