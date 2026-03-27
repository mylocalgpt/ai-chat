package orchestrator

import (
	"context"
	"encoding/json"
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
	primaryModel string // fallback default until cache loads
	secondModel  string // fallback default until cache loads
	threshold    float64
	cache        *modelCache
}

// NewOrchestrator creates an Orchestrator with the given router and store.
// The router should be pre-configured with the OpenRouter API key.
// primaryModel and secondModel are OpenRouter model identifiers used as
// fallback defaults until the first successful cache load from the database.
func NewOrchestrator(router *Router, st *store.Store, primaryModel, secondModel string) *Orchestrator {
	return &Orchestrator{
		router:       router,
		store:        st,
		primaryModel: primaryModel,
		secondModel:  secondModel,
		threshold:    defaultThreshold,
		cache:        newModelCache(defaultCacheTTL),
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
	o.refreshCacheIfStale(ctx)

	primaryModel, secondModel, threshold := o.resolveModels()

	userCtx, err := o.loadUserContext(ctx, msg)
	if err != nil {
		return Action{}, fmt.Errorf("orchestrator: %w", err)
	}

	workspaces, err := o.store.ListWorkspaces(ctx)
	if err != nil {
		return Action{}, fmt.Errorf("orchestrator: listing workspaces: %w", err)
	}

	primary, err := classifyIntent(ctx, o.router, primaryModel, msg, userCtx, workspaces)
	if err != nil {
		return Action{}, fmt.Errorf("orchestrator: primary classification: %w", err)
	}

	if primary.Confidence >= threshold {
		return primary, nil
	}

	second, err := classifyIntent(ctx, o.router, secondModel, msg, userCtx, workspaces)
	if err != nil {
		slog.Warn("second opinion failed", "error", err)
		return primary, nil
	}

	if second.Confidence > primary.Confidence {
		return second, nil
	}
	return primary, nil
}

// refreshCacheIfStale reloads model configs from the store if the cache TTL
// has elapsed. On failure, the stale cache is kept.
func (o *Orchestrator) refreshCacheIfStale(ctx context.Context) {
	if !o.cache.isStale() {
		return
	}
	configs, err := o.store.ListModelConfigs(ctx)
	if err != nil {
		slog.Warn("model cache refresh failed", "error", err)
		return
	}
	o.cache.refresh(configs)
}

// resolveModels returns the primary model, second model, and threshold to use.
// Values come from the cache if available, otherwise from the constructor defaults.
func (o *Orchestrator) resolveModels() (primary, second string, threshold float64) {
	primary = o.primaryModel
	second = o.secondModel
	threshold = o.threshold

	if cfg, ok := o.cache.get("router"); ok {
		primary = cfg.Model
		if t, ok := extractThreshold(cfg.Metadata); ok {
			threshold = t
		}
	}
	if cfg, ok := o.cache.get("second_opinion"); ok {
		second = cfg.Model
	}

	return primary, second, threshold
}

// extractThreshold parses the threshold value from a metadata JSON string.
func extractThreshold(metadata string) (float64, bool) {
	if metadata == "" || metadata == "{}" {
		return 0, false
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return 0, false
	}
	v, ok := m["threshold"]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return f, true
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
