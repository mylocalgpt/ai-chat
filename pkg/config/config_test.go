package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

func validJSON() string {
	return `{
		"telegram": {
			"bot_token": "test-token-123",
			"allowed_users": [111, 222]
		},
		"openrouter": {
			"api_key": "or-key-456"
		},
		"db_path": "/tmp/test.db",
		"log_dir": "/tmp/logs/",
		"http_addr": "127.0.0.1:9090"
	}`
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		check   func(t *testing.T, cfg *Config)
		wantErr string
	}{
		{
			name: "valid config loads all fields",
			json: validJSON(),
			check: func(t *testing.T, cfg *Config) {
				if cfg.Telegram.BotToken != "test-token-123" {
					t.Errorf("bot_token = %q, want %q", cfg.Telegram.BotToken, "test-token-123")
				}
				if len(cfg.Telegram.AllowedUsers) != 2 {
					t.Errorf("allowed_users len = %d, want 2", len(cfg.Telegram.AllowedUsers))
				}
				if cfg.OpenRouter.APIKey != "or-key-456" {
					t.Errorf("api_key = %q, want %q", cfg.OpenRouter.APIKey, "or-key-456")
				}
				if cfg.DBPath != "/tmp/test.db" {
					t.Errorf("db_path = %q, want %q", cfg.DBPath, "/tmp/test.db")
				}
				if cfg.LogDir != "/tmp/logs/" {
					t.Errorf("log_dir = %q, want %q", cfg.LogDir, "/tmp/logs/")
				}
				if cfg.HTTPAddr != "127.0.0.1:9090" {
					t.Errorf("http_addr = %q, want %q", cfg.HTTPAddr, "127.0.0.1:9090")
				}
			},
		},
		{
			name: "missing bot_token",
			json: `{
				"telegram": {
					"allowed_users": [111]
				}
			}`,
			wantErr: "bot_token",
		},
		{
			name: "empty allowed_users",
			json: `{
				"telegram": {
					"bot_token": "tok",
					"allowed_users": []
				}
			}`,
			wantErr: "allowed_users",
		},
		{
			name: "defaults applied when fields empty",
			json: `{
				"telegram": {
					"bot_token": "tok",
					"allowed_users": [1]
				},
				"openrouter": {"api_key": "or-test"}
			}`,
			check: func(t *testing.T, cfg *Config) {
				home, _ := os.UserHomeDir()
				wantDB := filepath.Join(home, ".config", "ai-chat", "state.db")
				if cfg.DBPath != wantDB {
					t.Errorf("db_path = %q, want %q", cfg.DBPath, wantDB)
				}
				wantLog := filepath.Join(home, ".config", "ai-chat", "logs")
				if cfg.LogDir != wantLog {
					t.Errorf("log_dir = %q, want %q", cfg.LogDir, wantLog)
				}
				if cfg.HTTPAddr != "127.0.0.1:8080" {
					t.Errorf("http_addr = %q, want %q", cfg.HTTPAddr, "127.0.0.1:8080")
				}
			},
		},
		{
			name: "tilde expansion in db_path",
			json: `{
				"telegram": {
					"bot_token": "tok",
					"allowed_users": [1]
				},
				"openrouter": {"api_key": "or-test"},
				"db_path": "~/data/state.db"
			}`,
			check: func(t *testing.T, cfg *Config) {
				home, _ := os.UserHomeDir()
				if !strings.HasPrefix(cfg.DBPath, home) {
					t.Errorf("db_path %q should start with home dir %q", cfg.DBPath, home)
				}
				if strings.Contains(cfg.DBPath, "~") {
					t.Errorf("db_path %q should not contain tilde", cfg.DBPath)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestConfig(t, tc.json)
			cfg, err := Load(path)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q should contain %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "/nonexistent/path/config.json") {
		t.Errorf("error %q should mention the file path", err)
	}
}

func TestStringRedactsSecrets(t *testing.T) {
	cfg := &Config{
		Telegram: TelegramConfig{
			BotToken:     "super-secret-token",
			AllowedUsers: []int64{111, 222},
		},
		OpenRouter: OpenRouterConfig{
			APIKey: "secret-api-key",
		},
		DBPath:   "/home/user/.ai-chat/state.db",
		LogDir:   "/home/user/.ai-chat/logs/",
		HTTPAddr: "127.0.0.1:8080",
	}

	s := cfg.String()

	if strings.Contains(s, "super-secret-token") {
		t.Error("String() should not contain the actual bot token")
	}
	if strings.Contains(s, "secret-api-key") {
		t.Error("String() should not contain the actual API key")
	}
	if !strings.Contains(s, "[set]") {
		t.Error("String() should contain [set] for populated secrets")
	}
	if !strings.Contains(s, "2 user(s)") {
		t.Error("String() should show user count")
	}
	if !strings.Contains(s, "/home/user/.ai-chat/state.db") {
		t.Error("String() should show db_path")
	}
}

func TestStringNotSet(t *testing.T) {
	cfg := &Config{}
	s := cfg.String()

	if !strings.Contains(s, "[not set]") {
		t.Error("String() should show [not set] for empty secrets")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "valid",
			cfg: Config{
				Telegram: TelegramConfig{
					BotToken:     "tok",
					AllowedUsers: []int64{1},
				},
				OpenRouter: OpenRouterConfig{
					APIKey: "or-key",
				},
			},
		},
		{
			name: "missing openrouter api_key",
			cfg: Config{
				Telegram: TelegramConfig{
					BotToken:     "tok",
					AllowedUsers: []int64{1},
				},
			},
			wantErr: "openrouter.api_key is required",
		},
		{
			name: "missing bot_token",
			cfg: Config{
				Telegram: TelegramConfig{
					AllowedUsers: []int64{1},
				},
			},
			wantErr: "bot_token is required",
		},
		{
			name: "empty allowed_users",
			cfg: Config{
				Telegram: TelegramConfig{
					BotToken: "tok",
				},
			},
			wantErr: "allowed_users must have at least one entry",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q should contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
