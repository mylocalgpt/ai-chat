package mcp

import (
	"context"
	"encoding/json"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConfigShowRedactsSecrets(t *testing.T) {
	ms := newMockStore()
	cfg := &MCPConfig{
		TelegramToken: "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz",
		OpenRouterKey: "sk-or-1234567890abcdef",
		ResponsesDir:  "/tmp/responses",
		AllowedUsers:  []int64{123, 456},
		BinaryPath:    "/usr/local/bin/ai-chat",
	}
	srv := NewServer(ms, cfg)

	res, _, err := srv.handleConfigShow(context.Background(), &gomcp.CallToolRequest{}, ConfigShowInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output ConfigShowOutput
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if output.TelegramToken == "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz" {
		t.Error("telegram token should be redacted")
	}
	if output.OpenRouterKey == "sk-or-1234567890abcdef" {
		t.Error("openrouter key should be redacted")
	}
	if output.TelegramToken == "[not set]" {
		t.Error("telegram token should show [set] or partial, not [not set]")
	}
}

func TestConfigShowEmptySecrets(t *testing.T) {
	ms := newMockStore()
	cfg := &MCPConfig{}
	srv := NewServer(ms, cfg)

	res, _, err := srv.handleConfigShow(context.Background(), &gomcp.CallToolRequest{}, ConfigShowInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output ConfigShowOutput
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if output.TelegramToken != "[not set]" {
		t.Errorf("expected '[not set]', got %q", output.TelegramToken)
	}
	if output.OpenRouterKey != "[not set]" {
		t.Errorf("expected '[not set]', got %q", output.OpenRouterKey)
	}
}

func TestConfigInstallInstructionsOpenCode(t *testing.T) {
	ms := newMockStore()
	cfg := &MCPConfig{BinaryPath: "/usr/local/bin/ai-chat"}
	srv := NewServer(ms, cfg)

	res, _, err := srv.handleConfigInstallInstructions(context.Background(), &gomcp.CallToolRequest{}, ConfigInstallInput{
		Agent: "opencode",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if _, ok := output["opencode"]; !ok {
		t.Error("expected opencode config")
	}
	if _, ok := output["copilot"]; ok {
		t.Error("should not include copilot when agent is opencode")
	}
}

func TestConfigInstallInstructionsCopilot(t *testing.T) {
	ms := newMockStore()
	cfg := &MCPConfig{BinaryPath: "/usr/local/bin/ai-chat"}
	srv := NewServer(ms, cfg)

	res, _, err := srv.handleConfigInstallInstructions(context.Background(), &gomcp.CallToolRequest{}, ConfigInstallInput{
		Agent: "copilot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if _, ok := output["copilot"]; !ok {
		t.Error("expected copilot config")
	}
	if _, ok := output["opencode"]; ok {
		t.Error("should not include opencode when agent is copilot")
	}
}

func TestConfigInstallInstructionsAll(t *testing.T) {
	ms := newMockStore()
	cfg := &MCPConfig{BinaryPath: "/usr/local/bin/ai-chat"}
	srv := NewServer(ms, cfg)

	res, _, err := srv.handleConfigInstallInstructions(context.Background(), &gomcp.CallToolRequest{}, ConfigInstallInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if _, ok := output["opencode"]; !ok {
		t.Error("expected opencode config")
	}
	if _, ok := output["copilot"]; !ok {
		t.Error("expected copilot config")
	}
}

func TestConfigHealth(t *testing.T) {
	ms := newMockStore()
	cfg := &MCPConfig{ResponsesDir: t.TempDir()}
	srv := NewServer(ms, cfg)

	res, _, err := srv.handleConfigHealth(context.Background(), &gomcp.CallToolRequest{}, ConfigHealthInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := res.Content[0].(*gomcp.TextContent)
	var output ConfigHealthOutput
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if output.Telegram.OK {
		t.Error("telegram should not be OK when not configured")
	}
	if !output.ResponseDir.OK {
		t.Errorf("response dir should be OK: %s", output.ResponseDir.Error)
	}
}

func TestRedactSecret(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "[not set]"},
		{"short", "[set]"},
		{"12345678", "[set]"},
		{"123456789", "1234...6789"},
		{"1234567890123456", "1234...3456"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := redactSecret(tt.input)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
