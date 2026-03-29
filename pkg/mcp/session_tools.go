package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mylocalgpt/ai-chat/pkg/store"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SessionListInput struct{}

type SessionListEntry struct {
	WorkspaceName string `json:"workspace_name"`
	Agent         string `json:"agent"`
	TmuxSession   string `json:"tmux_session"`
	Status        string `json:"status"`
	LastActivity  string `json:"last_activity,omitempty"`
}

type SessionRestartInput struct {
	WorkspaceName string `json:"workspace_name" jsonschema:"Name of the workspace"`
	Agent         string `json:"agent,omitempty" jsonschema:"Agent to restart (uses workspace default if omitted)"`
}

type SessionKillInput struct {
	WorkspaceName string `json:"workspace_name" jsonschema:"Name of the workspace"`
	Agent         string `json:"agent,omitempty" jsonschema:"Agent to kill (uses workspace default if omitted)"`
}

type AgentSendInput struct {
	WorkspaceName string `json:"workspace_name" jsonschema:"Name of the workspace to send the message to"`
	Agent         string `json:"agent,omitempty" jsonschema:"Agent to send to (uses workspace default, then opencode)"`
	Message       string `json:"message" jsonschema:"Message to send to the agent"`
}

func (s *Server) registerSessionTools() {
	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_list",
		Description: "List all agent sessions with status and workspace info",
	}, s.handleSessionList)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_restart",
		Description: "Kill and respawn an agent session for a workspace",
	}, s.handleSessionRestart)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "session_kill",
		Description: "Kill an agent session without respawning",
	}, s.handleSessionKill)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "agent_send",
		Description: "Send a message to an AI agent in a workspace and return the response",
	}, s.handleAgentSend)
}

func (s *Server) handleSessionList(ctx context.Context, _ *gomcp.CallToolRequest, _ SessionListInput) (*gomcp.CallToolResult, any, error) {
	sessions, err := s.store.ListSessions(ctx)
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

	entries := make([]SessionListEntry, len(sessions))
	for i, sess := range sessions {
		lastActivity := ""
		if !sess.LastActivity.IsZero() {
			lastActivity = sess.LastActivity.Format("2006-01-02T15:04:05Z")
		}
		entries[i] = SessionListEntry{
			WorkspaceName: wsNames[sess.WorkspaceID],
			Agent:         sess.Agent,
			TmuxSession:   sess.TmuxSession,
			Status:        sess.Status,
			LastActivity:  lastActivity,
		}
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling session list: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func (s *Server) handleSessionRestart(ctx context.Context, _ *gomcp.CallToolRequest, input SessionRestartInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	ws, err := s.store.GetWorkspace(ctx, input.WorkspaceName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.WorkspaceName)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}

	agent, err := s.resolveAgent(input.Agent, ws.Metadata)
	if err != nil {
		return nil, nil, err
	}

	sessions, err := s.store.ListSessionsForWorkspace(ctx, ws.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("listing sessions: %w", err)
	}

	for _, sess := range sessions {
		if sess.Agent == agent {
			if err := s.sessionMgr.KillSession(ctx, sess.ID); err != nil {
				return nil, nil, fmt.Errorf("killing session: %w", err)
			}
			break
		}
	}

	if _, err := s.sessionMgr.CreateSession(ctx, *ws, agent); err != nil {
		return nil, nil, fmt.Errorf("creating session: %w", err)
	}

	return textResult(fmt.Sprintf("Restarted session for workspace: %s (agent: %s)", input.WorkspaceName, agent)), nil, nil
}

func (s *Server) handleSessionKill(ctx context.Context, _ *gomcp.CallToolRequest, input SessionKillInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	ws, err := s.store.GetWorkspace(ctx, input.WorkspaceName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.WorkspaceName)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}

	agent, err := s.resolveAgent(input.Agent, ws.Metadata)
	if err != nil {
		return nil, nil, err
	}

	sessions, err := s.store.ListSessionsForWorkspace(ctx, ws.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("listing sessions: %w", err)
	}

	for _, sess := range sessions {
		if sess.Agent == agent {
			if err := s.sessionMgr.KillSession(ctx, sess.ID); err != nil {
				return nil, nil, fmt.Errorf("killing session: %w", err)
			}
			break
		}
	}

	return textResult(fmt.Sprintf("Killed session for workspace: %s", input.WorkspaceName)), nil, nil
}

func (s *Server) handleAgentSend(ctx context.Context, _ *gomcp.CallToolRequest, input AgentSendInput) (*gomcp.CallToolResult, any, error) {
	if s.sessionMgr == nil {
		return nil, nil, fmt.Errorf("session manager not available")
	}

	ws, err := s.store.GetWorkspace(ctx, input.WorkspaceName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.WorkspaceName)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}

	agent, err := s.resolveAgent(input.Agent, ws.Metadata)
	if err != nil {
		agent = "opencode"
	}

	sessions, err := s.store.ListSessionsForWorkspace(ctx, ws.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("listing sessions: %w", err)
	}

	var sessionID int64
	for _, sess := range sessions {
		if sess.Agent == agent {
			sessionID = sess.ID
			break
		}
	}

	if sessionID == 0 {
		return nil, nil, fmt.Errorf("no active session for agent %s in workspace %s", agent, input.WorkspaceName)
	}

	if err := s.sessionMgr.Send(ctx, sessionID, input.Message); err != nil {
		return nil, nil, fmt.Errorf("sending message: %w", err)
	}

	return textResult("Message sent to agent"), nil, nil
}

func (s *Server) resolveAgent(inputAgent string, metadata json.RawMessage) (string, error) {
	if inputAgent != "" {
		return inputAgent, nil
	}

	if metadata != nil {
		var meta map[string]any
		if json.Unmarshal(metadata, &meta) == nil {
			if agent, ok := meta["default_agent"].(string); ok && agent != "" {
				return agent, nil
			}
		}
	}

	return "", fmt.Errorf("no agent specified and workspace has no default_agent")
}
