package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// HandleAction processes a classified action and returns a response string.
// For agent_task actions, the response is empty (the caller routes to the executor).
func (o *Orchestrator) HandleAction(ctx context.Context, msg core.InboundMessage, action Action) (string, error) {
	switch action.Type {
	case ActionWorkspaceSwitch:
		return o.handleWorkspaceSwitch(ctx, msg, action)
	case ActionStatus:
		return o.handleStatus(ctx, msg)
	case ActionDirectAnswer:
		return action.Content, nil
	case ActionAgentTask:
		return "", nil
	case ActionMetaCommand:
		return o.handleMetaCommand(ctx, action)
	default:
		return "", fmt.Errorf("unknown action type %q", action.Type)
	}
}

func (o *Orchestrator) handleWorkspaceSwitch(ctx context.Context, msg core.InboundMessage, action Action) (string, error) {
	ws, err := matchWorkspace(ctx, action.Workspace, o.store)
	if err != nil {
		return err.Error(), nil
	}

	if err := o.store.SetActiveWorkspace(ctx, msg.SenderID, msg.Channel, ws.ID); err != nil {
		return "", fmt.Errorf("setting active workspace: %w", err)
	}

	return fmt.Sprintf("Switched to %s", ws.Name), nil
}

func (o *Orchestrator) handleStatus(ctx context.Context, msg core.InboundMessage) (string, error) {
	uctx, err := o.loadUserContext(ctx, msg)
	if err != nil {
		return "", fmt.Errorf("loading user context: %w", err)
	}

	if uctx.ActiveWorkspace == nil {
		workspaces, err := o.store.ListWorkspaces(ctx)
		if err != nil {
			return "", fmt.Errorf("listing workspaces: %w", err)
		}
		if len(workspaces) == 0 {
			return "No active workspace. No workspaces available.", nil
		}
		names := make([]string, len(workspaces))
		for i, w := range workspaces {
			names[i] = w.Name
		}
		return fmt.Sprintf("No active workspace. Available: %s", strings.Join(names, ", ")), nil
	}

	var sessionLine string
	sess, err := o.store.GetActiveSession(ctx, uctx.ActiveWorkspace.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			sessionLine = "No active session"
		} else {
			return "", fmt.Errorf("checking session: %w", err)
		}
	} else {
		sessionLine = fmt.Sprintf("%s (%s)", sess.Agent, sess.Status)
	}

	return fmt.Sprintf("Active workspace: %s (%s)\nActive session: %s",
		uctx.ActiveWorkspace.Name, uctx.ActiveWorkspace.Path, sessionLine), nil
}

func (o *Orchestrator) handleMetaCommand(ctx context.Context, action Action) (string, error) {
	lower := strings.ToLower(action.Content + " " + action.Reasoning)

	if strings.Contains(lower, "list workspace") || strings.Contains(lower, "help workspace") {
		return o.listWorkspacesResponse(ctx)
	}
	if strings.Contains(lower, "help") {
		return "Available commands: switch to <workspace>, status, list workspaces, help", nil
	}

	return action.Content, nil
}

func (o *Orchestrator) listWorkspacesResponse(ctx context.Context) (string, error) {
	workspaces, err := o.store.ListWorkspaces(ctx)
	if err != nil {
		return "", fmt.Errorf("listing workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return "No workspaces configured.", nil
	}

	var b strings.Builder
	b.WriteString("Workspaces:\n")
	for _, w := range workspaces {
		b.WriteString(fmt.Sprintf("- %s (%s)\n", w.Name, w.Path))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// matchWorkspace finds a workspace by name using multiple matching strategies.
func matchWorkspace(ctx context.Context, name string, st *store.Store) (*core.Workspace, error) {
	workspaces, err := st.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}

	// Exact match (case-insensitive).
	for i := range workspaces {
		if strings.EqualFold(workspaces[i].Name, name) {
			return &workspaces[i], nil
		}
	}

	// Alias match.
	ws, err := st.FindWorkspaceByAlias(ctx, strings.ToLower(name))
	if err == nil {
		return ws, nil
	}

	// Prefix match (case-insensitive).
	lower := strings.ToLower(name)
	var matches []core.Workspace
	for _, w := range workspaces {
		if strings.HasPrefix(strings.ToLower(w.Name), lower) {
			matches = append(matches, w)
		}
	}

	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return nil, fmt.Errorf("Ambiguous workspace %q. Did you mean: %s?", name, strings.Join(names, ", "))
	}

	// No match.
	names := make([]string, len(workspaces))
	for i, w := range workspaces {
		names[i] = w.Name
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("Workspace %q not found. No workspaces available.", name)
	}
	return nil, fmt.Errorf("Workspace %q not found. Available: %s", name, strings.Join(names, ", "))
}
