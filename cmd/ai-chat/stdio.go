package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mylocalgpt/ai-chat/pkg/config"
	mcppkg "github.com/mylocalgpt/ai-chat/pkg/mcp"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

func runStdio() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.LoadForMCP("")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Ensure data directory exists.
	dbDir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		slog.Error("failed to create data directory", "dir", dbDir, "error", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}

	if err := store.Migrate(db); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	st := store.New(db)
	defer st.Close()

	mcpCfg := &mcppkg.ServerConfig{
		AllowedUsers: cfg.Telegram.AllowedUsers, // may be empty if Telegram not configured
	}
	srv := mcppkg.NewServer(st, mcpCfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Run(ctx); err != nil {
		slog.Error("mcp server exited with error", "error", err)
		os.Exit(1)
	}
}
