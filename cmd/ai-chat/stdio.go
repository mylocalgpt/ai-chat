package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
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
		AllowedUsers: cfg.Telegram.AllowedUsers,
	}

	var opts []mcppkg.Option

	// Wire executor for session management.
	tmx := executor.NewTmux()
	registry := executor.NewHarnessRegistry(tmx)
	exec := executor.NewExecutor(st, tmx, registry)
	opts = append(opts, mcppkg.WithExecutor(exec))

	// Wire Telegram adapter if bot token is configured.
	// The adapter is NOT started (no long polling) - only used for API calls
	// (setMyCommands, sendMessage) and connectivity checks.
	if cfg.Telegram.BotToken != "" {
		tg, err := telegram.NewTelegramAdapter(telegram.TelegramAdapterConfig{
			BotToken:     cfg.Telegram.BotToken,
			AllowedUsers: cfg.Telegram.AllowedUsers,
		}, st)
		if err != nil {
			slog.Warn("telegram adapter unavailable for MCP, continuing without it", "error", err)
		} else {
			opts = append(opts, mcppkg.WithNotifier(tg))
			opts = append(opts, mcppkg.WithChannelAdapter(tg))
		}
	}

	srv := mcppkg.NewServer(st, mcpCfg, opts...)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Run(ctx); err != nil {
		slog.Error("mcp server exited with error", "error", err)
		os.Exit(1)
	}
}
