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
	HandleSecurityDecision(ctx context.Context, senderID, channel, token string, approved bool) (string, error)
	CreateSession(ctx context.Context, workspaceID int64, agent string) (*core.SessionInfo, error)
	CreateSessionForSender(ctx context.Context, senderID, channel, agent string) (*core.SessionInfo, error)
	SwitchActiveSession(ctx context.Context, senderID, channel string, workspaceID, sessionID int64) error
	ClearSession(ctx context.Context, senderID, channel string) (*core.SessionInfo, error)
	KillSession(ctx context.Context, senderID, channel string) error
	GetStatus(ctx context.Context, senderID, channel string) (*StatusInfo, error)
	SetAgent(ctx context.Context, senderID, channel, agent string) (*core.SessionInfo, error)
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
	return &Router{store: st, sessionMgr: sm}
}

func (r *Router) Route(ctx context.Context, req Request) (Result, error) {
	if req.Message == nil {
		return NoReplyResult(), nil
	}

	cmd, ok := Parse(req.Message.Content)
	if !ok {
		if r.sessionMgr != nil {
			err := r.sessionMgr.Send(ctx, req.Message.SenderID, req.Message.Channel, req.Message.Content)
			if err != nil {
				var decisionErr *core.SecurityDecisionError
				if errors.As(err, &decisionErr) {
					switch decisionErr.Decision.Action {
					case core.SecurityActionConfirm:
						return Result{Kind: ResultSecurityConfirmation, SecurityConfirmation: &SecurityConfirmationData{Token: decisionErr.Decision.PendingID, Summary: decisionErr.Decision.Reason}}, nil
					case core.SecurityActionBlock:
						return TextResult(decisionErr.Decision.Reason), nil
					}
				}
				if errors.Is(err, store.ErrNotFound) {
					return r.workspacePickerResult(ctx, req.Message.SenderID, req.Message.Channel, "Select a workspace to get started:")
				}
				return Result{}, err
			}
			return NoReplyResult(), nil
		}
		return NoReplyResult(), nil
	}

	switch cmd.Name {
	case "workspaces":
		return r.handleWorkspaces(ctx, req.Message)
	case "sessions":
		return r.handleSessions(ctx, req.Message)
	case "switch":
		return r.handleSwitch(ctx, req.Message, cmd.Args)
	case "new":
		return r.handleNew(ctx, req.Message)
	case "clear":
		return r.handleClear(ctx, req.Message)
	case "kill":
		return r.handleKill(ctx, req.Message)
	case "status":
		return r.handleStatus(ctx, req.Message)
	case "agent":
		return r.handleAgent(ctx, req.Message, cmd.Args)
	default:
		return TextResult(fmt.Sprintf("Unknown command: /%s. Try /status for help.", cmd.Name)), nil
	}
}

func (r *Router) HandleWorkspaceSelection(ctx context.Context, senderID, channel string, workspaceID int64) (Result, error) {
	ws, err := r.store.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TextResult("Workspace not found."), nil
		}
		return Result{}, fmt.Errorf("getting workspace: %w", err)
	}
	if err := r.store.SetActiveWorkspace(ctx, senderID, channel, ws.ID); err != nil {
		return Result{}, fmt.Errorf("setting active workspace: %w", err)
	}
	return TextResult(fmt.Sprintf("Switched to workspace %s (%s)", ws.Name, ws.Path)), nil
}

func (r *Router) HandleSessionSelection(ctx context.Context, senderID, channel string, workspaceID, sessionID int64) (Result, error) {
	if r.sessionMgr == nil {
		return TextResult("Session manager not configured."), nil
	}
	if err := r.sessionMgr.SwitchActiveSession(ctx, senderID, channel, workspaceID, sessionID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TextResult("That session is no longer available."), nil
		}
		return Result{}, fmt.Errorf("switching session: %w", err)
	}
	sess, err := r.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return Result{}, fmt.Errorf("getting session: %w", err)
	}
	return TextResult(fmt.Sprintf("Switched to session %s", sess.TmuxSession)), nil
}

func (r *Router) HandleSecurityDecision(ctx context.Context, senderID, channel, token string, approved bool) (Result, error) {
	if r.sessionMgr == nil {
		return TextResult("Session manager not configured."), nil
	}
	text, err := r.sessionMgr.HandleSecurityDecision(ctx, senderID, channel, token, approved)
	if err != nil {
		return Result{}, fmt.Errorf("handling security decision: %w", err)
	}
	return TextResult(text), nil
}

func (r *Router) handleWorkspaces(ctx context.Context, msg *core.InboundMessage) (Result, error) {
	return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace:")
}

func (r *Router) handleSessions(ctx context.Context, msg *core.InboundMessage) (Result, error) {
	active, err := r.store.GetActiveWorkspace(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
	}
	ws, err := r.store.GetWorkspaceByID(ctx, active.WorkspaceID)
	if err != nil {
		return Result{}, fmt.Errorf("getting workspace: %w", err)
	}
	sessions, err := r.store.ListSessionsForWorkspace(ctx, active.WorkspaceID)
	if err != nil {
		return Result{}, fmt.Errorf("listing sessions: %w", err)
	}
	activeSessionID := int64(0)
	activeSession, err := r.store.GetActiveSessionForWorkspace(ctx, msg.SenderID, msg.Channel, active.WorkspaceID)
	if err == nil && activeSession != nil {
		activeSessionID = activeSession.SessionID
	}
	options := make([]SessionOption, 0, len(sessions))
	for _, sess := range sessions {
		options = append(options, SessionOption{
			ID:       sess.ID,
			Name:     sess.TmuxSession,
			Slug:     sess.Slug,
			Agent:    sess.Agent,
			Status:   sess.Status,
			IsActive: sess.ID == activeSessionID,
		})
	}
	return Result{
		Kind: ResultSessionPicker,
		SessionPicker: &SessionPickerData{
			WorkspaceID:     ws.ID,
			WorkspaceName:   ws.Name,
			Sessions:        options,
			ActiveSessionID: activeSessionID,
			Prompt:          "Select a session:",
		},
	}, nil
}

func (r *Router) handleSwitch(ctx context.Context, msg *core.InboundMessage, args []string) (Result, error) {
	if len(args) == 0 {
		return TextResult("Usage: /switch <workspace>"), nil
	}
	name := args[0]
	ws, err := r.store.GetWorkspace(ctx, name)
	if err != nil {
		ws, err = r.store.FindWorkspaceByAlias(ctx, name)
		if err != nil {
			return TextResult(fmt.Sprintf("Workspace %q not found.", name)), nil
		}
	}
	return r.HandleWorkspaceSelection(ctx, msg.SenderID, msg.Channel, ws.ID)
}

func (r *Router) handleNew(ctx context.Context, msg *core.InboundMessage) (Result, error) {
	if r.sessionMgr == nil {
		return TextResult("Session manager not configured."), nil
	}
	info, err := r.sessionMgr.CreateSessionForSender(ctx, msg.SenderID, msg.Channel, "")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
		}
		return Result{}, fmt.Errorf("creating session: %w", err)
	}
	return TextResult(fmt.Sprintf("Created session %s in %s", info.Name, info.Workspace)), nil
}

func (r *Router) handleClear(ctx context.Context, msg *core.InboundMessage) (Result, error) {
	if r.sessionMgr == nil {
		return TextResult("Session manager not configured."), nil
	}
	info, err := r.sessionMgr.ClearSession(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
		}
		return Result{}, fmt.Errorf("clearing session: %w", err)
	}
	if info == nil {
		return TextResult("No active session to clear."), nil
	}
	return TextResult(fmt.Sprintf("Cleared session %s", info.Name)), nil
}

func (r *Router) handleKill(ctx context.Context, msg *core.InboundMessage) (Result, error) {
	if r.sessionMgr == nil {
		return TextResult("Session manager not configured."), nil
	}
	if err := r.sessionMgr.KillSession(ctx, msg.SenderID, msg.Channel); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
		}
		return Result{}, fmt.Errorf("killing session: %w", err)
	}
	return TextResult("Session killed."), nil
}

func (r *Router) handleStatus(ctx context.Context, msg *core.InboundMessage) (Result, error) {
	if r.sessionMgr == nil {
		active, err := r.store.GetActiveWorkspace(ctx, msg.SenderID, msg.Channel)
		if err != nil {
			return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
		}
		ws, err := r.store.GetWorkspaceByID(ctx, active.WorkspaceID)
		if err != nil {
			return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
		}
		return TextResult(fmt.Sprintf("Workspace: %s (%s)\nAgent: not configured", ws.Name, ws.Path)), nil
	}
	info, err := r.sessionMgr.GetStatus(ctx, msg.SenderID, msg.Channel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
		}
		return Result{}, fmt.Errorf("getting status: %w", err)
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
	return TextResult(b.String()), nil
}

func (r *Router) handleAgent(ctx context.Context, msg *core.InboundMessage, args []string) (Result, error) {
	if len(args) == 0 {
		return TextResult("Usage: /agent <name>"), nil
	}
	if r.sessionMgr == nil {
		return TextResult("Session manager not configured."), nil
	}
	agent := args[0]
	if _, err := r.sessionMgr.SetAgent(ctx, msg.SenderID, msg.Channel, agent); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return r.workspacePickerResult(ctx, msg.SenderID, msg.Channel, "Select a workspace to get started:")
		}
		return Result{}, fmt.Errorf("setting agent: %w", err)
	}
	return TextResult(fmt.Sprintf("Agent set to %s", agent)), nil
}

func (r *Router) workspacePickerResult(ctx context.Context, senderID, channel, prompt string) (Result, error) {
	workspaces, err := r.store.ListWorkspaces(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("listing workspaces: %w", err)
	}
	activeID := int64(0)
	active, err := r.store.GetActiveWorkspace(ctx, senderID, channel)
	if err == nil && active != nil {
		activeID = active.WorkspaceID
	}
	options := make([]WorkspaceOption, 0, len(workspaces))
	for _, ws := range workspaces {
		options = append(options, WorkspaceOption{ID: ws.ID, Name: ws.Name, Path: ws.Path})
	}
	if prompt == "" {
		prompt = "Select a workspace:"
	}
	return Result{
		Kind: ResultWorkspacePicker,
		WorkspacePicker: &WorkspacePickerData{
			Workspaces:        options,
			ActiveWorkspaceID: activeID,
			Prompt:            prompt,
		},
	}, nil
}
