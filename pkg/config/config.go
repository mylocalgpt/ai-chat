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
	Telegram      TelegramConfig   `json:"telegram"`
	OpenRouter    OpenRouterConfig `json:"openrouter"`
	DBPath        string           `json:"db_path"`
	LogDir        string           `json:"log_dir"`
	LogRetainDays int              `json:"log_retain_days"`
	ResponsesDir  string           `json:"responses_dir"`
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

// Load reads and parses a JSON config file. If path is empty, it checks
// ~/.config/ai-chat/config.json first, then config.json in the current directory.
func Load(path string) (*Config, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", resolved, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", resolved, err)
	}

	// Apply defaults for empty fields.
	if cfg.DBPath == "" {
		cfg.DBPath = appDir() + "/state.db"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = appDir() + "/logs"
	}
	if cfg.LogRetainDays == 0 {
		cfg.LogRetainDays = 30
	}
	if cfg.ResponsesDir == "" {
		cfg.ResponsesDir = appDir() + "/responses"
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
	cfg.ResponsesDir, err = expandHome(cfg.ResponsesDir)
	if err != nil {
		return nil, fmt.Errorf("expanding responses_dir: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadForMCP loads config without requiring Telegram credentials.
// Used by the MCP stdio entrypoint where Telegram is optional.
func LoadForMCP(path string) (*Config, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", resolved, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", resolved, err)
	}

	// Apply defaults for empty fields.
	if cfg.DBPath == "" {
		cfg.DBPath = appDir() + "/state.db"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = appDir() + "/logs"
	}
	if cfg.LogRetainDays == 0 {
		cfg.LogRetainDays = 30
	}
	if cfg.ResponsesDir == "" {
		cfg.ResponsesDir = appDir() + "/responses"
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
	cfg.ResponsesDir, err = expandHome(cfg.ResponsesDir)
	if err != nil {
		return nil, fmt.Errorf("expanding responses_dir: %w", err)
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

	fmt.Fprintf(&b, "telegram.allowed_users: %d user(s)\n", len(c.Telegram.AllowedUsers))

	b.WriteString("openrouter.api_key: ")
	if c.OpenRouter.APIKey != "" {
		b.WriteString("[set]")
	} else {
		b.WriteString("[not set]")
	}
	b.WriteByte('\n')

	fmt.Fprintf(&b, "db_path: %s\n", c.DBPath)
	fmt.Fprintf(&b, "log_dir: %s\n", c.LogDir)
	fmt.Fprintf(&b, "responses_dir: %s", c.ResponsesDir)

	return b.String()
}

// resolveConfigPath returns the config file path to use. If an explicit path
// is given, it expands tilde and returns it. Otherwise it checks
// ~/.config/ai-chat/config.json first, then config.json in the current directory.
func resolveConfigPath(path string) (string, error) {
	if path != "" {
		return expandHome(path)
	}

	xdgPath := appDir() + "/config.json"
	if _, err := os.Stat(xdgPath); err == nil {
		return xdgPath, nil
	}

	if _, err := os.Stat("config.json"); err == nil {
		return "config.json", nil
	}

	return xdgPath, nil
}

// appDir returns the ai-chat config/data directory (~/.config/ai-chat).
func appDir() string {
	if dir := os.Getenv("AI_CHAT_HOME"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "ai-chat")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ai-chat")
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
