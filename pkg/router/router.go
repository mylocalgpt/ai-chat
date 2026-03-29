package router

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

type SessionManager interface {
	Send(ctx context.Context, senderID, channel, message string) error
	CreateSession(ctx context.Context, workspaceID int64, agent string) (*core.SessionInfo, error)
	ClearSession(ctx context.Context, senderID, channel string) (*core.SessionInfo, error)
	KillSession(ctx context.Context, senderID, channel string) error
	GetStatus(ctx context.Context, senderID, channel string) (*StatusInfo, error)
	SetAgent(ctx context.Context, senderID, channel, agent string) error
}

type StatusInfo struct {
	Workspace     *core.Workspace
	ActiveSession *core.SessionInfo
	Agent         string
	SessionCount  int
}

type Router struct {
	store      *store.Store
	sessionMgr SessionManager
}

func NewRouter(st *store.Store, sm SessionManager) *Router {
	return &Router{
		store:      st,
		sessionMgr: sm,
	}
}

func (r *Router) Route(ctx context.Context, msg core.InboundMessage) (string, error) {
	cmd, ok := Parse(msg.Content)
	if !ok {
		if r.sessionMgr != nil {
			err := r.sessionMgr.Send(ctx, msg.SenderID, msg.Channel, msg.Content)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return "No active workspace. Use /workspaces to select one.", nil
				}
				return "", err
			}
			return "", nil
		}
		return "", nil
	}

	switch cmd.Name {
	case "workspaces":
		return r.handleWorkspaces(ctx, msg)
	case "sessions":
		return r.handleSessions(ctx, msg)
	case "switch":
		return r.handleSwitch(ctx, msg, cmd.Args)
	case "new":
		return r.handleNew(ctx, msg)
	case "clear":
		return r.handleClear(ctx, msg)
	case "kill":
		return r.handleKill(ctx, msg)
	case "status":
		return r.handleStatus(ctx, msg)
	case "agent":
		return r.handleAgent(ctx, msg, cmd.Args)
	default:
		return fmt.Sprintf("Unknown command: /%s. Try /status for help.", cmd.Name), nil
	}
}

func (r *Router) handleWorkspaces(ctx context.Context, msg core.InboundMessage) (string, error) {
	workspaces, err := r.store.ListWorkspaces(ctx)
	if err != nil {
		return "", fmt.Errorf("listing workspaces: %w", err)
	}

	active, err := r.store.GetActiveWorkspace(ctx, msg.SenderID, msg.Channel)
	activeID := int64(0)
	if err == nil {
		activeID = active.WorkspaceID
	}

	if len(workspaces) == 0 {
		return "No workspaces configured.", nil
	}

	var b strings.Builder
	for i, w := range workspaces {
		if i > 0 {
			b.WriteString("\n")
		}
		if w.ID == activeID {
			b.WriteString("→ ")
		} else {
			b.WriteString("  ")
		}
		b.WriteString(w.Name)
		b.WriteString(": ")
		b.WriteString(w.Path)
	}
	return b.String(), nil
}

func (r *Router) handleSessions(ctx context.Context, msg core.InboundMessage) (string, error) {
	active, err := r.store.GetActiveWorkspace(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		return "No active workspace. Use /switch <name> first.", nil
	}

	sessions, err := r.store.ListSessionsForWorkspace(ctx, active.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		return "No sessions in current workspace.", nil
	}

	var b strings.Builder
	for i, s := range sessions {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s.Slug)
		b.WriteString(" [")
		b.WriteString(s.Agent)
		b.WriteString("] ")
		b.WriteString(s.Status)
	}
	return b.String(), nil
}

func (r *Router) handleSwitch(ctx context.Context, msg core.InboundMessage, args []string) (string, error) {
	if len(args) == 0 {
		return "Usage: /switch <workspace>", nil
	}

	name := args[0]

	ws, err := r.store.GetWorkspace(ctx, name)
	if err != nil {
		ws, err = r.store.FindWorkspaceByAlias(ctx, name)
		if err != nil {
			return fmt.Sprintf("Workspace %q not found.", name), nil
		}
	}

	if err := r.store.SetActiveWorkspace(ctx, msg.SenderID, msg.Channel, ws.ID); err != nil {
		return "", fmt.Errorf("setting active workspace: %w", err)
	}

	return fmt.Sprintf("Switched to workspace %s (%s)", ws.Name, ws.Path), nil
}

func (r *Router) handleNew(ctx context.Context, msg core.InboundMessage) (string, error) {
	if r.sessionMgr == nil {
		return "Session manager not configured.", nil
	}

	active, err := r.store.GetActiveWorkspace(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		return "No active workspace. Use /switch <name> first.", nil
	}

	agent := "opencode"
	info, err := r.sessionMgr.CreateSession(ctx, active.WorkspaceID, agent)
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}

	return fmt.Sprintf("Created session %s in %s", info.Name, info.Workspace), nil
}

func (r *Router) handleClear(ctx context.Context, msg core.InboundMessage) (string, error) {
	if r.sessionMgr == nil {
		return "Session manager not configured.", nil
	}

	info, err := r.sessionMgr.ClearSession(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "No active workspace. Use /switch <name> first.", nil
		}
		return "", fmt.Errorf("clearing session: %w", err)
	}

	if info == nil {
		return "No active session to clear.", nil
	}

	return fmt.Sprintf("Cleared session %s", info.Name), nil
}

func (r *Router) handleKill(ctx context.Context, msg core.InboundMessage) (string, error) {
	if r.sessionMgr == nil {
		return "Session manager not configured.", nil
	}

	if err := r.sessionMgr.KillSession(ctx, msg.SenderID, msg.Channel); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "No active workspace. Use /switch <name> first.", nil
		}
		return "", fmt.Errorf("killing session: %w", err)
	}

	return "Session killed.", nil
}

func (r *Router) handleStatus(ctx context.Context, msg core.InboundMessage) (string, error) {
	if r.sessionMgr == nil {
		active, err := r.store.GetActiveWorkspace(ctx, msg.SenderID, msg.Channel)
		if err != nil {
			return "No workspace selected.", nil
		}
		ws, err := r.store.GetWorkspaceByID(ctx, active.WorkspaceID)
		if err != nil {
			return "No workspace selected.", nil
		}
		return fmt.Sprintf("Workspace: %s (%s)\nAgent: not configured", ws.Name, ws.Path), nil
	}

	info, err := r.sessionMgr.GetStatus(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "No workspace selected.", nil
		}
		return "", fmt.Errorf("getting status: %w", err)
	}

	var b strings.Builder
	if info.Workspace != nil {
		b.WriteString("Workspace: ")
		b.WriteString(info.Workspace.Name)
		b.WriteString(" (")
		b.WriteString(info.Workspace.Path)
		b.WriteString(")\n")
	} else {
		b.WriteString("Workspace: none\n")
	}

	b.WriteString("Agent: ")
	if info.Agent != "" {
		b.WriteString(info.Agent)
	} else {
		b.WriteString("none")
	}
	b.WriteString("\n")

	if info.ActiveSession != nil {
		b.WriteString("Session: ")
		b.WriteString(info.ActiveSession.Name)
		b.WriteString("\n")
	}

	b.WriteString("Sessions: ")
	fmt.Fprintf(&b, "%d", info.SessionCount)

	return b.String(), nil
}

func (r *Router) handleAgent(ctx context.Context, msg core.InboundMessage, args []string) (string, error) {
	if len(args) == 0 {
		return "Usage: /agent <name>", nil
	}

	if r.sessionMgr == nil {
		return "Session manager not configured.", nil
	}

	agent := args[0]
	if err := r.sessionMgr.SetAgent(ctx, msg.SenderID, msg.Channel, agent); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "No active workspace. Use /switch <name> first.", nil
		}
		return "", fmt.Errorf("setting agent: %w", err)
	}

	return fmt.Sprintf("Agent set to %s", agent), nil
}
