package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

const defaultThreshold = 0.7

// Orchestrator classifies inbound messages into actions using LLM-based intent
// classification with a second-opinion pattern for low-confidence results.
type Orchestrator struct {
	router       *Router
	store        *store.Store
	primaryModel string
	secondModel  string
	threshold    float64
}

// NewOrchestrator creates an Orchestrator with the given router and store.
// The router should be pre-configured with the OpenRouter API key.
// primaryModel and secondModel are OpenRouter model identifiers.
func NewOrchestrator(router *Router, st *store.Store, primaryModel, secondModel string) *Orchestrator {
	return &Orchestrator{
		router:       router,
		store:        st,
		primaryModel: primaryModel,
		secondModel:  secondModel,
		threshold:    defaultThreshold,
	}
}

// WithThreshold sets the confidence threshold below which a second opinion is
// consulted. Default is 0.7.
func (o *Orchestrator) WithThreshold(f float64) *Orchestrator {
	o.threshold = f
	return o
}

// Classify determines what action to take for an inbound message.
func (o *Orchestrator) Classify(ctx context.Context, msg core.InboundMessage) (Action, error) {
	userCtx, err := o.loadUserContext(ctx, msg)
	if err != nil {
		return Action{}, fmt.Errorf("orchestrator: %w", err)
	}

	workspaces, err := o.store.ListWorkspaces(ctx)
	if err != nil {
		return Action{}, fmt.Errorf("orchestrator: listing workspaces: %w", err)
	}

	primary, err := classifyIntent(ctx, o.router, o.primaryModel, msg, userCtx, workspaces)
	if err != nil {
		return Action{}, fmt.Errorf("orchestrator: primary classification: %w", err)
	}

	if primary.Confidence >= o.threshold {
		return primary, nil
	}

	second, err := classifyIntent(ctx, o.router, o.secondModel, msg, userCtx, workspaces)
	if err != nil {
		slog.Warn("second opinion failed", "error", err)
		return primary, nil
	}

	if second.Confidence > primary.Confidence {
		return second, nil
	}
	return primary, nil
}

// loadUserContext resolves the user's active workspace from the store into a
// full UserContext for classification prompts.
func (o *Orchestrator) loadUserContext(ctx context.Context, msg core.InboundMessage) (UserContext, error) {
	uctx := UserContext{
		SenderID: msg.SenderID,
		Channel:  msg.Channel,
	}

	storeCtx, err := o.store.GetUserContext(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return uctx, nil
		}
		return uctx, fmt.Errorf("loading user context: %w", err)
	}

	if storeCtx.ActiveWorkspaceID > 0 {
		ws, err := o.store.GetWorkspaceByID(ctx, storeCtx.ActiveWorkspaceID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return uctx, nil
			}
			return uctx, fmt.Errorf("loading workspace: %w", err)
		}
		uctx.ActiveWorkspace = ws
	}

	return uctx, nil
}
