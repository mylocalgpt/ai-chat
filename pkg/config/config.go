package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds the application configuration loaded from a JSON file.
type Config struct {
	Telegram   TelegramConfig   `json:"telegram"`
	OpenRouter OpenRouterConfig `json:"openrouter"`
	DBPath     string           `json:"db_path"`
	LogDir     string           `json:"log_dir"`
	HTTPAddr   string           `json:"http_addr"`
}

// TelegramConfig holds Telegram bot credentials and access control.
type TelegramConfig struct {
	BotToken     string  `json:"bot_token"`
	AllowedUsers []int64 `json:"allowed_users"`
}

// OpenRouterConfig holds OpenRouter API credentials.
type OpenRouterConfig struct {
	APIKey string `json:"api_key"`
}

// Load reads and parses a JSON config file. If path is empty, it defaults
// to ~/.ai-chat/config.json. Tilde is expanded in the path and in path
// fields within the config.
func Load(path string) (*Config, error) {
	if path == "" {
		path = "~/.ai-chat/config.json"
	}

	expanded, err := expandHome(path)
	if err != nil {
		return nil, fmt.Errorf("expanding config path: %w", err)
	}

	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", expanded, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", expanded, err)
	}

	// Apply defaults for empty fields.
	if cfg.DBPath == "" {
		cfg.DBPath = "~/.ai-chat/state.db"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = "~/.ai-chat/logs/"
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = "127.0.0.1:8080"
	}

	// Expand tilde in path fields.
	cfg.DBPath, err = expandHome(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("expanding db_path: %w", err)
	}
	cfg.LogDir, err = expandHome(cfg.LogDir)
	if err != nil {
		return nil, fmt.Errorf("expanding log_dir: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadForMCP loads config without requiring Telegram credentials.
// Used by the MCP stdio entrypoint where Telegram is optional.
func LoadForMCP(path string) (*Config, error) {
	if path == "" {
		path = "~/.ai-chat/config.json"
	}

	expanded, err := expandHome(path)
	if err != nil {
		return nil, fmt.Errorf("expanding config path: %w", err)
	}

	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", expanded, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", expanded, err)
	}

	// Apply defaults for empty fields.
	if cfg.DBPath == "" {
		cfg.DBPath = "~/.ai-chat/state.db"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = "~/.ai-chat/logs/"
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = "127.0.0.1:8080"
	}

	// Expand tilde in path fields.
	cfg.DBPath, err = expandHome(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("expanding db_path: %w", err)
	}
	cfg.LogDir, err = expandHome(cfg.LogDir)
	if err != nil {
		return nil, fmt.Errorf("expanding log_dir: %w", err)
	}

	// Skip Validate() -- Telegram config is optional for MCP.
	return &cfg, nil
}

// Validate checks that required config fields are populated.
// Returns the first validation error found.
func (c *Config) Validate() error {
	if c.Telegram.BotToken == "" {
		return fmt.Errorf("telegram.bot_token is required")
	}
	if len(c.Telegram.AllowedUsers) == 0 {
		return fmt.Errorf("telegram.allowed_users must have at least one entry")
	}
	return nil
}

// String returns a human-readable representation with secrets redacted.
func (c *Config) String() string {
	var b strings.Builder

	b.WriteString("telegram.bot_token: ")
	if c.Telegram.BotToken != "" {
		b.WriteString("[set]")
	} else {
		b.WriteString("[not set]")
	}
	b.WriteByte('\n')

	b.WriteString(fmt.Sprintf("telegram.allowed_users: %d user(s)\n", len(c.Telegram.AllowedUsers)))

	b.WriteString("openrouter.api_key: ")
	if c.OpenRouter.APIKey != "" {
		b.WriteString("[set]")
	} else {
		b.WriteString("[not set]")
	}
	b.WriteByte('\n')

	b.WriteString(fmt.Sprintf("db_path: %s\n", c.DBPath))
	b.WriteString(fmt.Sprintf("log_dir: %s\n", c.LogDir))
	b.WriteString(fmt.Sprintf("http_addr: %s", c.HTTPAddr))

	return b.String()
}

// expandHome replaces a leading ~ in path with the user's home directory.
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}
