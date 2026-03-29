package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ConfigShowInput struct{}

type ConfigShowOutput struct {
	TelegramToken string  `json:"telegram_token"`
	ResponsesDir  string  `json:"responses_dir"`
	AllowedUsers  []int64 `json:"allowed_users"`
	BinaryPath    string  `json:"binary_path"`
}

type ConfigInstallInput struct {
	Agent string `json:"agent,omitempty" jsonschema:"Agent to generate config for (opencode, copilot). Omit for all."`
}

type ConfigHealthInput struct{}

type CheckResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type ConfigHealthOutput struct {
	Telegram    CheckResult            `json:"telegram"`
	Tmux        CheckResult            `json:"tmux"`
	ResponseDir CheckResult            `json:"response_dir"`
	ConfigFile  CheckResult            `json:"config_file"`
	Agents      map[string]CheckResult `json:"agents"`
}

func (s *Server) registerSelfConfigTools() {
	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "config_show",
		Description: "Show current ai-chat config with secrets redacted",
	}, s.handleConfigShow)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "config_install_instructions",
		Description: "Get MCP server config JSON for agent setup",
	}, s.handleConfigInstallInstructions)

	gomcp.AddTool(s.inner, &gomcp.Tool{
		Name:        "config_health",
		Description: "Check ai-chat configuration health",
	}, s.handleConfigHealth)
}

func (s *Server) handleConfigShow(ctx context.Context, _ *gomcp.CallToolRequest, _ ConfigShowInput) (*gomcp.CallToolResult, any, error) {
	output := ConfigShowOutput{
		TelegramToken: redactSecret(s.cfg.TelegramToken),
		ResponsesDir:  s.cfg.ResponsesDir,
		AllowedUsers:  s.cfg.AllowedUsers,
		BinaryPath:    s.cfg.BinaryPath,
	}

	data, err := json.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling config: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func (s *Server) handleConfigInstallInstructions(ctx context.Context, _ *gomcp.CallToolRequest, input ConfigInstallInput) (*gomcp.CallToolResult, any, error) {
	cmd := "ai-chat"
	if s.cfg.BinaryPath != "" {
		cmd = s.cfg.BinaryPath
	}

	instructions := make(map[string]any)

	if input.Agent == "" || input.Agent == "opencode" {
		instructions["opencode"] = map[string]any{
			"path": "~/.config/opencode/config.json",
			"config": map[string]any{
				"mcpServers": map[string]any{
					"ai-chat": map[string]any{
						"command": cmd,
						"args":    []string{"stdio"},
					},
				},
			},
		}
	}

	if input.Agent == "" || input.Agent == "copilot" {
		instructions["copilot"] = map[string]any{
			"path": ".vscode/mcp.json",
			"config": map[string]any{
				"servers": map[string]any{
					"ai-chat": map[string]any{
						"command": cmd,
						"args":    []string{"stdio"},
					},
				},
			},
		}
	}

	data, err := json.Marshal(instructions)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling instructions: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func (s *Server) handleConfigHealth(ctx context.Context, _ *gomcp.CallToolRequest, _ ConfigHealthInput) (*gomcp.CallToolResult, any, error) {
	output := ConfigHealthOutput{
		Agents: make(map[string]CheckResult),
	}

	if s.channel != nil && s.channel.IsConnected() {
		output.Telegram = CheckResult{OK: true}
	} else if s.channel != nil {
		output.Telegram = CheckResult{OK: false, Error: "not connected"}
	} else {
		output.Telegram = CheckResult{OK: false, Error: "not configured"}
	}

	if _, err := exec.LookPath("tmux"); err != nil {
		output.Tmux = CheckResult{OK: false, Error: "tmux not found in PATH"}
	} else {
		output.Tmux = CheckResult{OK: true}
	}

	output.ResponseDir = checkResponseDirWritable(s.cfg.ResponsesDir)

	configPath := configFilePath()
	if _, err := os.Stat(configPath); err != nil {
		output.ConfigFile = CheckResult{OK: false, Error: fmt.Sprintf("config file not found: %s", configPath)}
	} else {
		output.ConfigFile = CheckResult{OK: true, Detail: configPath}
	}

	for _, agent := range []string{"opencode"} {
		if _, err := exec.LookPath(agent); err != nil {
			output.Agents[agent] = CheckResult{OK: false, Error: "not found in PATH"}
		} else {
			output.Agents[agent] = CheckResult{OK: true}
		}
	}

	data, err := json.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling health: %w", err)
	}

	return textResult(string(data)), nil, nil
}

func redactSecret(secret string) string {
	if secret == "" {
		return "[not set]"
	}
	if len(secret) <= 8 {
		return "[set]"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}

func checkResponseDirWritable(dir string) CheckResult {
	if dir == "" {
		return CheckResult{OK: false, Error: "response directory not configured"}
	}
	tmpFile := filepath.Join(dir, fmt.Sprintf(".healthcheck-%d", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		return CheckResult{OK: false, Error: err.Error()}
	}
	os.Remove(tmpFile)
	return CheckResult{OK: true, Detail: dir}
}

func configFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(home, ".config", "ai-chat", "config.json")
}
