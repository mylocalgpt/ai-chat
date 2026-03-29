package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/mylocalgpt/ai-chat/pkg/core"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type HealthCheckInput struct{}

type PlatformStatus struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

type HealthCheckOutput struct {
	Status          string           `json:"status"`
	Platforms       []PlatformStatus `json:"platforms"`
	TmuxAvailable   bool             `json:"tmux_available"`
	ResponseDirOk   bool             `json:"response_dir_ok"`
	ActiveSessions  int              `json:"active_sessions"`
	TotalWorkspaces int              `json:"total_workspaces"`
}

type SendTestMessageInput struct {
	Platform string `json:"platform" jsonschema:"Platform to send test message to (telegram)"`
	Message  string `json:"message" jsonschema:"Test message text"`
}

func (s *Server) registerHealthTools() {
	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "health_check",
		Description: "Check system health: database, platforms, tmux, response dir, active sessions",
	}, s.handleHealthCheck)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "send_test_message",
		Description: "Send a test message to verify platform connectivity",
	}, s.handleSendTestMessage)
}

func (s *Server) handleHealthCheck(ctx context.Context, _ *gomcp.CallToolRequest, _ HealthCheckInput) (*gomcp.CallToolResult, any, error) {
	output := HealthCheckOutput{
		Status: "healthy",
	}

	if err := s.store.Ping(ctx); err != nil {
		output.Platforms = append(output.Platforms, PlatformStatus{
			Name:      "database",
			Connected: false,
			Error:     "database unreachable",
		})
		output.Status = "unhealthy"
	} else {
		output.Platforms = append(output.Platforms, PlatformStatus{
			Name:      "database",
			Connected: true,
		})
	}

	if s.channel == nil {
		output.Platforms = append(output.Platforms, PlatformStatus{
			Name:      "telegram",
			Connected: false,
			Error:     "adapter not configured",
		})
		if output.Status == "healthy" {
			output.Status = "degraded"
		}
	} else if !s.channel.IsConnected() {
		output.Platforms = append(output.Platforms, PlatformStatus{
			Name:      "telegram",
			Connected: false,
		})
		if output.Status == "healthy" {
			output.Status = "degraded"
		}
	} else {
		output.Platforms = append(output.Platforms, PlatformStatus{
			Name:      "telegram",
			Connected: true,
		})
	}

	if _, err := exec.LookPath("tmux"); err != nil {
		output.TmuxAvailable = false
		if output.Status == "healthy" {
			output.Status = "degraded"
		}
	} else {
		output.TmuxAvailable = true
	}

	result := checkResponseDirWritable(s.cfg.ResponsesDir)
	output.ResponseDirOk = result.OK

	sessions, err := s.store.ListSessions(ctx)
	if err == nil {
		for _, sess := range sessions {
			if sess.Status == string(core.SessionActive) {
				output.ActiveSessions++
			}
		}
	}

	workspaces, err := s.store.ListWorkspaces(ctx)
	if err == nil {
		output.TotalWorkspaces = len(workspaces)
	}

	data, err := json.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling health output: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func (s *Server) handleSendTestMessage(ctx context.Context, _ *gomcp.CallToolRequest, input SendTestMessageInput) (*gomcp.CallToolResult, any, error) {
	if input.Platform != "telegram" {
		return nil, nil, fmt.Errorf("unsupported platform: %q (only telegram is supported)", input.Platform)
	}

	if s.channel == nil {
		return nil, nil, fmt.Errorf("telegram adapter not configured")
	}

	if len(s.cfg.AllowedUsers) == 0 {
		return nil, nil, fmt.Errorf("no allowed users configured for test messages")
	}

	msg := core.OutboundMessage{
		Channel:     "telegram",
		RecipientID: strconv.FormatInt(s.cfg.AllowedUsers[0], 10),
		Content:     input.Message,
	}

	if err := s.channel.Send(ctx, msg); err != nil {
		return nil, nil, fmt.Errorf("sending test message: %w", err)
	}

	return textResult("Test message sent to telegram"), nil, nil
}
