package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SessionListInput struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"Filter by workspace name"`
}

type SessionListEntry struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Workspace string `json:"workspace"`
	Agent     string `json:"agent"`
	Status    string `json:"status"`
	Age       string `json:"age"`
}

type SessionCreateInput struct {
	Workspace string `json:"workspace" jsonschema:"Workspace name"`
	Agent     string `json:"agent" jsonschema:"Agent type (opencode, copilot)"`
}

type SessionClearInput struct {
	Workspace   string `json:"workspace" jsonschema:"Workspace name"`
	SessionName string `json:"session_name" jsonschema:"Session name or slug to clear"`
}

type SessionKillInput struct {
	SessionName string `json:"session_name" jsonschema:"Session name or slug to kill"`
}

type AgentSendInput struct {
	SessionName string `json:"session_name" jsonschema:"Session name or slug to send message to"`
	Message     string `json:"message" jsonschema:"Message to send to the agent"`
}

type AgentSendApprovalInput struct {
	PendingID string `json:"pending_id" jsonschema:"Pending approval token returned by agent_send"`
	Approve   bool   `json:"approve" jsonschema:"Whether to approve the pending send"`
}

type SessionSwitchInput struct {
	Workspace string `json:"workspace" jsonschema:"Workspace name"`
	Session   string `json:"session" jsonschema:"Session name or slug to switch to"`
}

func (s *Server) registerSessionTools() {
	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_list",
		Description: "List agent sessions, optionally filtered by workspace",
	}, s.handleSessionList)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_create",
		Description: "Create a new session in a workspace",
	}, s.handleSessionCreate)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_clear",
		Description: "Clear the active session for a workspace (adapter-aware)",
	}, s.handleSessionClear)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_kill",
		Description: "Kill a session by name or slug",
	}, s.handleSessionKill)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "agent_send",
		Description: "Send a message to an agent session",
	}, s.handleAgentSend)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "agent_send_approve",
		Description: "Approve or reject a pending agent_send confirmation",
	}, s.handleAgentSendApproval)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_switch",
		Description: "Switch the active MCP session for a workspace",
	}, s.handleSessionSwitch)
}

func (s *Server) handleSessionList(ctx context.Context, _ *gomcp.CallToolRequest, input SessionListInput) (*gomcp.CallToolResult, any, error) {
	var sessions []core.Session
	var err error

	if input.Workspace != "" {
		var ws *core.Workspace
		ws, err = s.store.GetWorkspace(ctx, input.Workspace)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil, fmt.Errorf("workspace %q not found", input.Workspace)
			}
			return nil, nil, fmt.Errorf("looking up workspace: %w", err)
		}
		sessions, err = s.store.ListSessionsForWorkspace(ctx, ws.ID)
	} else {
		sessions, err = s.store.ListSessions(ctx)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("listing sessions: %w", err)
	}

	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing workspaces: %w", err)
	}
	wsNames := make(map[int64]string, len(workspaces))
	for _, ws := range workspaces {
		wsNames[ws.ID] = ws.Name
	}

	entries := make([]SessionListEntry, 0, len(sessions))
	for _, sess := range sessions {
		wsName := wsNames[sess.WorkspaceID]

		entry := SessionListEntry{
			Name:      sess.TmuxSession,
			Slug:      sess.Slug,
			Workspace: wsName,
			Agent:     sess.Agent,
			Status:    sess.Status,
			Age:       humanAge(sess.LastActivity),
		}
		entries = append(entries, entry)
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling session list: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func (s *Server) handleSessionCreate(ctx context.Context, _ *gomcp.CallToolRequest, input SessionCreateInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	ws, err := s.store.GetWorkspace(ctx, input.Workspace)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.Workspace)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}

	sess, err := s.sessionMgr.CreateSession(ctx, *ws, input.Agent)
	if err != nil {
		return nil, nil, fmt.Errorf("creating session: %w", err)
	}

	name := sess.TmuxSession
	return textResult(fmt.Sprintf("Created session: %s (slug: %s)", name, sess.Slug)), nil, nil
}

func (s *Server) handleSessionClear(ctx context.Context, _ *gomcp.CallToolRequest, input SessionClearInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	if input.Workspace == "" {
		return nil, nil, fmt.Errorf("workspace is required")
	}

	ws, err := s.store.GetWorkspace(ctx, input.Workspace)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.Workspace)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}

	var sess *core.Session
	if input.SessionName != "" {
		sess, err = s.store.GetSessionByReferenceInWorkspace(ctx, ws.ID, input.SessionName)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil, fmt.Errorf("session %q not found in workspace %q", input.SessionName, input.Workspace)
			}
			return nil, nil, fmt.Errorf("looking up session: %w", err)
		}
	} else {
		activeWorkspace, err := s.store.GetActiveWorkspace(ctx, "mcp", "system")
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil, fmt.Errorf("no active MCP workspace set; provide session_name or switch to workspace %q first", input.Workspace)
			}
			return nil, nil, fmt.Errorf("looking up active workspace: %w", err)
		}
		if activeWorkspace.WorkspaceID != ws.ID {
			return nil, nil, fmt.Errorf("workspace %q is not the active MCP workspace; provide session_name or switch to it first", input.Workspace)
		}
		activeSession, err := s.store.GetActiveSessionForWorkspace(ctx, "mcp", "system", ws.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil, fmt.Errorf("no active MCP session in workspace %q; provide session_name or switch to a session first", input.Workspace)
			}
			return nil, nil, fmt.Errorf("looking up active session: %w", err)
		}
		sess, err = s.store.GetSessionByID(ctx, activeSession.SessionID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil, fmt.Errorf("active MCP session for workspace %q not found", input.Workspace)
			}
			return nil, nil, fmt.Errorf("looking up active session record: %w", err)
		}
		if sess.WorkspaceID != ws.ID {
			return nil, nil, fmt.Errorf("active MCP session does not belong to workspace %q", input.Workspace)
		}
	}

	newSess, err := s.sessionMgr.ClearSession(ctx, sess.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("clearing session: %w", err)
	}

	name := newSess.TmuxSession
	return textResult(fmt.Sprintf("Cleared session. New session: %s", name)), nil, nil
}

func (s *Server) handleSessionKill(ctx context.Context, _ *gomcp.CallToolRequest, input SessionKillInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	sess, err := s.store.GetSessionByReference(ctx, input.SessionName)
	if err != nil {
		return nil, nil, fmt.Errorf("looking up session: %w", err)
	}

	if err := s.sessionMgr.KillSession(ctx, sess.ID); err != nil {
		return nil, nil, fmt.Errorf("killing session: %w", err)
	}

	return textResult(fmt.Sprintf("Killed session: %s", input.SessionName)), nil, nil
}

func (s *Server) handleAgentSend(ctx context.Context, _ *gomcp.CallToolRequest, input AgentSendInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	sess, err := s.store.GetSessionByReference(ctx, input.SessionName)
	if err != nil {
		return nil, nil, fmt.Errorf("looking up session: %w", err)
	}

	if err := s.sessionMgr.Send(ctx, sess.ID, input.Message); err != nil {
		var decisionErr *core.SecurityDecisionError
		if errors.As(err, &decisionErr) {
			switch decisionErr.Decision.Action {
			case core.SecurityActionConfirm:
				payload := map[string]any{
					"status":     "confirmation_required",
					"pending_id": decisionErr.Decision.PendingID,
					"reason":     decisionErr.Decision.Reason,
				}
				data, marshalErr := json.Marshal(payload)
				if marshalErr != nil {
					return nil, nil, fmt.Errorf("marshaling confirmation result: %w", marshalErr)
				}
				return &gomcp.CallToolResult{
					Content:           []gomcp.Content{&gomcp.TextContent{Text: string(data)}},
					StructuredContent: payload,
					IsError:           true,
				}, nil, nil
			case core.SecurityActionBlock:
				return &gomcp.CallToolResult{
					Content: []gomcp.Content{&gomcp.TextContent{Text: decisionErr.Decision.Reason}},
					StructuredContent: map[string]any{
						"status": "blocked",
						"reason": decisionErr.Decision.Reason,
					},
					IsError: true,
				}, nil, nil
			}
		}
		return nil, nil, fmt.Errorf("sending message: %w", err)
	}

	return textResult("Message sent to agent"), nil, nil
}

func (s *Server) handleAgentSendApproval(ctx context.Context, _ *gomcp.CallToolRequest, input AgentSendApprovalInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}
	if input.PendingID == "" {
		return nil, nil, fmt.Errorf("pending_id is required")
	}
	text, err := s.sessionMgr.ApproveSend(ctx, input.PendingID, input.Approve)
	if err != nil {
		return nil, nil, fmt.Errorf("handling approval: %w", err)
	}
	payload := map[string]any{"status": "completed", "message": text}
	return &gomcp.CallToolResult{
		Content:           []gomcp.Content{&gomcp.TextContent{Text: text}},
		StructuredContent: payload,
	}, nil, nil
}

func (s *Server) handleSessionSwitch(ctx context.Context, _ *gomcp.CallToolRequest, input SessionSwitchInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}
	if input.Workspace == "" || input.Session == "" {
		return nil, nil, fmt.Errorf("workspace and session are required")
	}
	ws, err := s.store.GetWorkspace(ctx, input.Workspace)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.Workspace)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}
	sess, err := s.store.GetSessionByReferenceInWorkspace(ctx, ws.ID, input.Session)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("session %q not found in workspace %q", input.Session, input.Workspace)
		}
		return nil, nil, fmt.Errorf("looking up session: %w", err)
	}
	sess, err = s.sessionMgr.SwitchSession(ctx, ws.ID, sess.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("switching session: %w", err)
	}
	data, err := json.Marshal(map[string]any{"workspace": ws.Name, "session": sess.TmuxSession, "session_id": sess.ID})
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling switch result: %w", err)
	}
	return textResult(string(data)), nil, nil
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func parseSessionName(input string) (workspace, slug string, isFullName bool) {
	workspace, slug, isFullName = store.ParseSessionReference(input)
	if isFullName {
		return workspace, slug, true
	}
	return "", input, false
}
