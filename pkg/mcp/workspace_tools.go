package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/mylocalgpt/ai-chat/pkg/store"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type WorkspaceRegisterInput struct {
	Name         string `json:"name" jsonschema:"Short workspace name for commands"`
	Path         string `json:"path" jsonschema:"Filesystem path to the workspace"`
	Host         string `json:"host,omitempty" jsonschema:"Remote host (empty for local)"`
	Description  string `json:"description,omitempty" jsonschema:"What this workspace is for"`
	DefaultAgent string `json:"default_agent,omitempty" jsonschema:"Preferred agent (claude, opencode, or copilot)"`
}

type WorkspaceUnregisterInput struct {
	Name string `json:"name" jsonschema:"Workspace name to remove"`
}

type WorkspaceRenameInput struct {
	OldName string `json:"old_name" jsonschema:"Current workspace name"`
	NewName string `json:"new_name" jsonschema:"New workspace name"`
}

type WorkspaceListInput struct{}

type workspaceInfo struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Host         string   `json:"host,omitempty"`
	Description  string   `json:"description,omitempty"`
	DefaultAgent string   `json:"default_agent,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
}

func (s *Server) registerWorkspaceTools() {
	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "workspace_register",
		Description: "Register a new workspace directory for AI work",
	}, s.handleWorkspaceRegister)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "workspace_unregister",
		Description: "Remove a workspace registration",
	}, s.handleWorkspaceUnregister)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "workspace_rename",
		Description: "Rename an existing workspace",
	}, s.handleWorkspaceRename)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "workspace_list",
		Description: "List all registered workspaces",
	}, s.handleWorkspaceList)
}

func (s *Server) handleWorkspaceRegister(ctx context.Context, _ *gomcp.CallToolRequest, input WorkspaceRegisterInput) (*gomcp.CallToolResult, any, error) {
	if input.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}

	_, err := s.store.GetWorkspace(ctx, input.Name)
	if err == nil {
		return nil, nil, fmt.Errorf("workspace %q already exists", input.Name)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, fmt.Errorf("checking workspace: %w", err)
	}

	if input.Host == "" {
		if input.Path == "" {
			return nil, nil, fmt.Errorf("path is required for local workspaces")
		}
		if _, err := os.Stat(input.Path); err != nil {
			return nil, nil, fmt.Errorf("path %q does not exist", input.Path)
		}
	}

	ws, err := s.store.CreateWorkspace(ctx, input.Name, input.Path, input.Host)
	if err != nil {
		return nil, nil, fmt.Errorf("creating workspace: %w", err)
	}

	if input.Description != "" || input.DefaultAgent != "" {
		meta := map[string]any{}
		if input.Description != "" {
			meta["description"] = input.Description
		}
		if input.DefaultAgent != "" {
			meta["default_agent"] = input.DefaultAgent
		}
		raw, _ := json.Marshal(meta)
		if err := s.store.UpdateWorkspaceMetadata(ctx, ws.ID, raw); err != nil {
			return nil, nil, fmt.Errorf("setting metadata: %w", err)
		}
	}

	s.notifyWorkspacesChanged()
	return textResult(fmt.Sprintf("Registered workspace: %s", input.Name)), nil, nil
}

func (s *Server) handleWorkspaceUnregister(ctx context.Context, _ *gomcp.CallToolRequest, input WorkspaceUnregisterInput) (*gomcp.CallToolResult, any, error) {
	ws, err := s.store.GetWorkspace(ctx, input.Name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.Name)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}

	if err := s.store.DeleteWorkspace(ctx, ws.ID); err != nil {
		return nil, nil, fmt.Errorf("deleting workspace: %w", err)
	}

	s.notifyWorkspacesChanged()
	return textResult(fmt.Sprintf("Unregistered workspace: %s", input.Name)), nil, nil
}

func (s *Server) handleWorkspaceRename(ctx context.Context, _ *gomcp.CallToolRequest, input WorkspaceRenameInput) (*gomcp.CallToolResult, any, error) {
	ws, err := s.store.GetWorkspace(ctx, input.OldName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("workspace %q not found", input.OldName)
		}
		return nil, nil, fmt.Errorf("looking up workspace: %w", err)
	}

	_, err = s.store.GetWorkspace(ctx, input.NewName)
	if err == nil {
		return nil, nil, fmt.Errorf("workspace %q already exists", input.NewName)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, fmt.Errorf("checking new name: %w", err)
	}

	if err := s.store.RenameWorkspace(ctx, ws.ID, input.NewName); err != nil {
		return nil, nil, fmt.Errorf("renaming workspace: %w", err)
	}

	s.notifyWorkspacesChanged()
	return textResult(fmt.Sprintf("Renamed workspace: %s -> %s", input.OldName, input.NewName)), nil, nil
}

func (s *Server) handleWorkspaceList(ctx context.Context, _ *gomcp.CallToolRequest, _ WorkspaceListInput) (*gomcp.CallToolResult, any, error) {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing workspaces: %w", err)
	}

	infos := make([]workspaceInfo, len(workspaces))
	for i, ws := range workspaces {
		info := workspaceInfo{
			Name: ws.Name,
			Path: ws.Path,
			Host: ws.Host,
		}
		if ws.Metadata != nil {
			var meta map[string]any
			if json.Unmarshal(ws.Metadata, &meta) == nil {
				if d, ok := meta["description"].(string); ok {
					info.Description = d
				}
				if a, ok := meta["default_agent"].(string); ok {
					info.DefaultAgent = a
				}
				if aliases, ok := meta["aliases"].([]any); ok {
					for _, alias := range aliases {
						if s, ok := alias.(string); ok {
							info.Aliases = append(info.Aliases, s)
						}
					}
				}
			}
		}
		infos[i] = info
	}

	data, err := json.Marshal(infos)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling workspace list: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func (s *Server) notifyWorkspacesChanged() {
	if s.notifier != nil {
		s.notifier.OnWorkspacesChanged()
	}
}

func textResult(text string) *gomcp.CallToolResult {
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{
			&gomcp.TextContent{Text: text},
		},
	}
}
