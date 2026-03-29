package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const sessionNamePrefix = "ai-chat-"

type SessionListInput struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"Filter by workspace name"`
}

type SessionListEntry struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Workspace    string `json:"workspace"`
	Agent        string `json:"agent"`
	Status       string `json:"status"`
	Age          string `json:"age"`
	PreviewUser  string `json:"preview_user"`
	PreviewAgent string `json:"preview_agent"`
}

type SessionSwitchInput struct {
	SessionName string `json:"session_name" jsonschema:"Session name or slug to switch to"`
}

type SessionCreateInput struct {
	Workspace string `json:"workspace" jsonschema:"Workspace name"`
	Agent     string `json:"agent" jsonschema:"Agent type (opencode, copilot)"`
}

type SessionClearInput struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"Workspace to clear"`
}

type SessionKillInput struct {
	SessionName string `json:"session_name" jsonschema:"Session name or slug to kill"`
}

type AgentSendInput struct {
	SessionName string `json:"session_name" jsonschema:"Session name or slug to send message to"`
	Message     string `json:"message" jsonschema:"Message to send to the agent"`
}

func (s *Server) registerSessionTools() {
	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_list",
		Description: "List agent sessions with previews, optionally filtered by workspace",
	}, s.handleSessionList)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_switch",
		Description: "Switch active session by name or slug",
	}, s.handleSessionSwitch)

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
}

func (s *Server) handleSessionList(ctx context.Context, _ *gomcp.CallToolRequest, input SessionListInput) (*gomcp.CallToolResult, any, error) {
	var sessions []core.Session
	var err error

	if input.Workspace != "" {
		ws, err := s.store.GetWorkspace(ctx, input.Workspace)
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
		previewUser, previewAgent, _ := s.store.GetSessionPreview(ctx, sess.ID)

		entry := SessionListEntry{
			Name:         sessionNamePrefix + wsName + "-" + sess.Slug,
			Slug:         sess.Slug,
			Workspace:    wsName,
			Agent:        sess.Agent,
			Status:       sess.Status,
			Age:          humanAge(sess.LastActivity),
			PreviewUser:  truncate(previewUser, 80),
			PreviewAgent: truncate(previewAgent, 120),
		}
		entries = append(entries, entry)
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling session list: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func (s *Server) handleSessionSwitch(ctx context.Context, _ *gomcp.CallToolRequest, input SessionSwitchInput) (*gomcp.CallToolResult, any, error) {
	workspace, slug, isFullName := parseSessionName(input.SessionName)

	var sess *core.Session
	var err error

	if isFullName {
		ws, lookupErr := s.store.GetWorkspace(ctx, workspace)
		if lookupErr != nil {
			if errors.Is(lookupErr, store.ErrNotFound) {
				return nil, nil, fmt.Errorf("workspace %q not found", workspace)
			}
			return nil, nil, fmt.Errorf("looking up workspace: %w", lookupErr)
		}
		sess, err = s.store.GetSessionByName(ctx, sessionNamePrefix+workspace+"-"+slug)
		if err != nil {
			return nil, nil, fmt.Errorf("looking up session: %w", err)
		}
		_ = ws
	} else {
		sessions, err := s.store.ListSessions(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("listing sessions: %w", err)
		}

		var matches []core.Session
		for _, s := range sessions {
			if s.Slug == slug {
				matches = append(matches, s)
			}
		}

		if len(matches) == 0 {
			return nil, nil, fmt.Errorf("session with slug %q not found", slug)
		}
		if len(matches) > 1 {
			var workspaces []string
			wsMap := make(map[int64]string)
			for _, w := range matches {
				if _, ok := wsMap[w.WorkspaceID]; !ok {
					ws, _ := s.store.GetWorkspace(ctx, "")
					if ws != nil {
						wsMap[w.WorkspaceID] = ws.Name
					}
				}
				workspaces = append(workspaces, wsMap[w.WorkspaceID])
			}
			return nil, nil, fmt.Errorf("ambiguous slug %q exists in multiple workspaces: %v", slug, workspaces)
		}
		sess = &matches[0]
	}

	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	if err := s.sessionMgr.SetActiveSession(ctx, sess.WorkspaceID, sess.ID); err != nil {
		return nil, nil, fmt.Errorf("setting active session: %w", err)
	}

	return textResult(fmt.Sprintf("Switched to session: %s", sessionNamePrefix+sess.Slug)), nil, nil
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

	name := sessionNamePrefix + ws.Name + "-" + sess.Slug
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

	sess, err := s.store.GetActiveSession(ctx, ws.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("no active session for workspace %q", input.Workspace)
		}
		return nil, nil, fmt.Errorf("looking up active session: %w", err)
	}

	newSess, err := s.sessionMgr.ClearSession(ctx, *sess)
	if err != nil {
		return nil, nil, fmt.Errorf("clearing session: %w", err)
	}

	name := sessionNamePrefix + ws.Name + "-" + newSess.Slug
	return textResult(fmt.Sprintf("Cleared session. New session: %s", name)), nil, nil
}

func (s *Server) handleSessionKill(ctx context.Context, _ *gomcp.CallToolRequest, input SessionKillInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	_, slug, isFullName := parseSessionName(input.SessionName)

	var sess *core.Session
	var err error

	if isFullName {
		sess, err = s.store.GetSessionByName(ctx, input.SessionName)
	} else {
		sessions, err := s.store.ListSessions(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("listing sessions: %w", err)
		}

		var matches []core.Session
		for _, s := range sessions {
			if s.Slug == slug {
				matches = append(matches, s)
			}
		}

		if len(matches) == 0 {
			return nil, nil, fmt.Errorf("session with slug %q not found", slug)
		}
		if len(matches) > 1 {
			return nil, nil, fmt.Errorf("ambiguous slug %q exists in multiple workspaces", slug)
		}
		sess = &matches[0]
	}

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

	_, slug, isFullName := parseSessionName(input.SessionName)

	var sess *core.Session
	var err error

	if isFullName {
		sess, err = s.store.GetSessionByName(ctx, input.SessionName)
	} else {
		sessions, err := s.store.ListSessions(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("listing sessions: %w", err)
		}

		var matches []core.Session
		for _, s := range sessions {
			if s.Slug == slug {
				matches = append(matches, s)
			}
		}

		if len(matches) == 0 {
			return nil, nil, fmt.Errorf("session with slug %q not found", slug)
		}
		if len(matches) > 1 {
			return nil, nil, fmt.Errorf("ambiguous slug %q exists in multiple workspaces", slug)
		}
		sess = &matches[0]
	}

	if err != nil {
		return nil, nil, fmt.Errorf("looking up session: %w", err)
	}

	if err := s.sessionMgr.Send(ctx, sess.ID, input.Message); err != nil {
		return nil, nil, fmt.Errorf("sending message: %w", err)
	}

	return textResult("Message sent to agent"), nil, nil
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
	if strings.HasPrefix(input, sessionNamePrefix) {
		parts := strings.TrimPrefix(input, sessionNamePrefix)
		idx := strings.LastIndex(parts, "-")
		if idx > 0 {
			return parts[:idx], parts[idx+1:], true
		}
	}
	return "", input, false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
