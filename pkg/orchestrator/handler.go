package orchestrator

import (
	"context"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

const (
	defaultPrimaryModel = "google/gemini-3.1-flash-lite-preview"
	defaultSecondModel  = "minimax/minimax-m2.5"
)

// Result is the outcome of handling an inbound message.
type Result struct {
	Action   Action
	Response string // empty for agent_task (caller hands off to executor)
}

// HandleMessage is the single entry point called by channel adapters.
// It classifies the message, processes the action, and returns the result.
func (o *Orchestrator) HandleMessage(ctx context.Context, msg core.InboundMessage) (*Result, error) {
	action, err := o.Classify(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("handle message: %w", err)
	}

	response, err := o.HandleAction(ctx, msg, action)
	if err != nil {
		return nil, fmt.Errorf("handle message: %w", err)
	}

	return &Result{
		Action:   action,
		Response: response,
	}, nil
}

// Setup creates a ready-to-use Orchestrator from a pre-configured Router and
// store. It seeds default model configs if the table is empty and loads the
// initial model configuration.
func Setup(router *Router, st *store.Store) (*Orchestrator, error) {
	ctx := context.Background()

	if err := SeedDefaults(ctx, st); err != nil {
		return nil, fmt.Errorf("orchestrator setup: seed defaults: %w", err)
	}

	configs, err := st.ListModelConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("orchestrator setup: loading configs: %w", err)
	}

	primaryModel := defaultPrimaryModel
	secondModel := defaultSecondModel
	for _, cfg := range configs {
		switch cfg.Role {
		case "router":
			primaryModel = cfg.Model
		case "second_opinion":
			secondModel = cfg.Model
		}
	}

	return NewOrchestrator(router, st, primaryModel, secondModel), nil
}
