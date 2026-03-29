package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mylocalgpt/ai-chat/pkg/app"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	mcppkg "github.com/mylocalgpt/ai-chat/pkg/mcp"
	"github.com/mylocalgpt/ai-chat/pkg/session"
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
	defer func() { _ = st.Close() }()
	if err := os.MkdirAll(cfg.ResponsesDir, 0o755); err != nil {
		slog.Error("failed to create responses directory", "dir", cfg.ResponsesDir, "error", err)
		os.Exit(1)
	}

	mcpCfg := &mcppkg.MCPConfig{
		AllowedUsers: cfg.Telegram.AllowedUsers,
		ResponsesDir: cfg.ResponsesDir,
	}

	var opts []mcppkg.Option
	var tg *telegram.TelegramAdapter

	tmx := executor.NewTmux()
	registry := executor.NewHarnessRegistry(tmx)
	proxy := executor.NewSecurityProxy()
	manager := session.NewManager(st, registry, proxy, session.ManagerConfig{ResponsesDir: cfg.ResponsesDir})
	sessionMgr := newExecutorSessionManager(manager, st)
	opts = append(opts, mcppkg.WithSessionManager(sessionMgr))

	if cfg.Telegram.BotToken != "" {
		tg, err = telegram.NewTelegramAdapter(telegram.TelegramAdapterConfig{
			BotToken:     cfg.Telegram.BotToken,
			AllowedUsers: cfg.Telegram.AllowedUsers,
		}, st)
		if err != nil {
			slog.Warn("telegram adapter unavailable for MCP, continuing without it", "error", err)
			tg = nil
		} else {
			opts = append(opts, mcppkg.WithNotifier(tg))
			opts = append(opts, mcppkg.WithChannelAdapter(tg))
		}
	}

	srv := mcppkg.NewServer(st, mcpCfg, opts...)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var backgroundWG interface{ Wait() }
	if tg != nil {
		backgroundWG = app.StartBackground(ctx, st, manager, tg)
		if err := tg.Start(ctx); err != nil {
			slog.Error("failed to start telegram adapter", "error", err)
			shutdownStdioBackground(cancel, backgroundWG, nil)
			os.Exit(1)
		}
	} else {
		backgroundWG = app.StartManagerBackground(ctx, manager)
	}

	err = srv.Run(ctx)
	shutdownStdioBackground(cancel, backgroundWG, tg)
	if err != nil {
		slog.Error("mcp server exited with error", "error", err)
		os.Exit(1)
	}
}

func shutdownStdioBackground(cancel context.CancelFunc, backgroundWG interface{ Wait() }, tg interface{ Stop() error }) {
	cancel()
	if tg != nil {
		_ = tg.Stop()
	}
	if backgroundWG != nil {
		backgroundWG.Wait()
	}
}
