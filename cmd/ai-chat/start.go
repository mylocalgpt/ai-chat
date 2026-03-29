package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/app"
	"github.com/mylocalgpt/ai-chat/pkg/audit"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/lockfile"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file (default: ~/.config/ai-chat/config.json, then ./config.json)")
	_ = fs.Parse(args)

	// Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	// Set up structured JSON logging to stderr + daily log file.
	logFile, err := audit.OpenDailyLog(cfg.LogDir)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("failed to open log file", "error", err)
		os.Exit(1)
	}
	defer func() { _ = logFile.Close() }()
	logger := slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stderr, logFile), nil))
	slog.SetDefault(logger)

	slog.Info("config loaded", "config", cfg.String())

	// Initialize audit logger.
	if err := audit.Init(cfg.LogDir, cfg.LogRetainDays); err != nil {
		slog.Error("failed to init audit logger", "error", err)
		os.Exit(1)
	}

	// Ensure data directory exists.
	dbDir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		slog.Error("failed to create data directory", "dir", dbDir, "error", err)
		os.Exit(1)
	}

	// Open and migrate database.
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if err := store.Migrate(db); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Acquire exclusive polling lock to prevent concurrent Telegram pollers.
	lockPath := filepath.Join(filepath.Dir(cfg.DBPath), "polling.lock")
	lockCloser, err := lockfile.Lock(lockPath)
	if err != nil {
		slog.Error("failed to acquire polling lock", "error", err)
		os.Exit(1)
	}
	defer func() { _ = lockCloser.Close() }()

	st := store.New(db)

	// Initialize executor components.
	tmuxRunner := executor.NewTmux()

	// Create response directory.
	if err := os.MkdirAll(cfg.ResponsesDir, 0o755); err != nil {
		slog.Error("failed to create responses directory", "dir", cfg.ResponsesDir, "error", err)
		os.Exit(1)
	}

	runtime := app.NewRuntime(st, tmuxRunner, app.RuntimeConfig{
		ResponsesDir:    cfg.ResponsesDir,
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		ReaperInterval:  5 * time.Minute,
	})

	// Signal handling for graceful shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initialize Telegram adapter.
	tg, err := telegram.NewTelegramAdapter(telegram.TelegramAdapterConfig{
		BotToken:     cfg.Telegram.BotToken,
		AllowedUsers: cfg.Telegram.AllowedUsers,
	}, st)
	if err != nil {
		slog.Error("failed to create telegram adapter", "error", err)
		os.Exit(1)
	}

	// Wire message handlers.
	tg.SetRouter(runtime.Router)
	tg.SetShutdownFunc(cancel)
	tg.SetAbortFunc(runtime.SessionManager.AbortSession)
	tg.SetSummarizer(telegram.NewSummarizer(runtime.ServerManager))
	tg.SetResponsesDir(cfg.ResponsesDir)
	wg := app.StartBackground(ctx, st, runtime.SessionManager, tg)

	// Start Telegram long polling.
	if err := tg.Start(ctx); err != nil {
		slog.Error("failed to start telegram adapter", "error", err)
		os.Exit(1)
	}

	slog.Info("server started", "db", cfg.DBPath, "responses_dir", cfg.ResponsesDir)

	// Block until signal.
	<-ctx.Done()

	slog.Info("shutting down")
	_ = tg.Stop()
	runtime.Shutdown()
	wg.Wait()
	_ = st.Close()
	_ = audit.CloseGlobal()
}
