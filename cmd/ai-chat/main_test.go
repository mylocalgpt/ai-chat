package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

func TestStartIntegration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	responsesDir := filepath.Join(dir, "responses")

	// Write a valid config file (no openrouter required).
	cfgJSON := `{
		"telegram": {
			"bot_token": "test-token",
			"allowed_users": [123]
		},
		"db_path": "` + dbPath + `",
		"log_dir": "` + dir + `",
		"responses_dir": "` + responsesDir + `"
	}`
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Load config.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	if cfg.ResponsesDir != responsesDir {
		t.Errorf("responses_dir = %q, want %q", cfg.ResponsesDir, responsesDir)
	}

	// OpenRouter should be empty but not cause an error.
	if cfg.OpenRouter.APIKey != "" {
		t.Errorf("openrouter.api_key = %q, want empty", cfg.OpenRouter.APIKey)
	}

	// Open and migrate DB.
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := store.Migrate(db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}

	// Create responses directory.
	if err := os.MkdirAll(cfg.ResponsesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll responses: %v", err)
	}

	// Verify directory was created.
	info, err := os.Stat(cfg.ResponsesDir)
	if err != nil {
		t.Fatalf("responses dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("responses_dir is not a directory")
	}
}
