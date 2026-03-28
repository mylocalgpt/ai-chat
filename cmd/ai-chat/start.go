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

	"github.com/mylocalgpt/ai-chat/pkg/audit"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
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

	st := store.New(db)

	// Initialize executor.
	tmuxRunner := executor.NewTmux()
	registry := executor.NewHarnessRegistry(tmuxRunner)
	exec := executor.NewExecutor(st, tmuxRunner, registry, cfg.ResponsesDir)

	// Clean up stale sessions from previous runs.
	if res, err := exec.ReconcileSessions(context.Background()); err != nil {
		slog.Warn("session reconciliation failed", "error", err)
	} else if res.Crashed > 0 || res.Orphaned > 0 {
		slog.Info("reconciled sessions", "crashed", res.Crashed, "orphaned", res.Orphaned)
	}

	// Create response directory.
	if err := os.MkdirAll(cfg.ResponsesDir, 0o755); err != nil {
		slog.Error("failed to create responses directory", "dir", cfg.ResponsesDir, "error", err)
		os.Exit(1)
	}

	// Initialize slash command router.
	cmdRouter := router.NewRouter(st, nil) // sessionMgr wired in Phase 5

	// Initialize Telegram adapter.
	tg, err := telegram.NewTelegramAdapter(telegram.TelegramAdapterConfig{
		BotToken:     cfg.Telegram.BotToken,
		AllowedUsers: cfg.Telegram.AllowedUsers,
	}, st)
	if err != nil {
		slog.Error("failed to create telegram adapter", "error", err)
		os.Exit(1)
	}

	// Build message handler shared by all channels.
	handleMessage := func(send func(context.Context, core.OutboundMessage) error) func(context.Context, core.InboundMessage) {
		return func(ctx context.Context, msg core.InboundMessage) {
			log := slog.With("channel", msg.Channel, "sender", msg.SenderID)

			// Persist inbound message.
			if err := st.CreateMessage(ctx, &core.Message{
				Channel:   msg.Channel,
				SenderID:  msg.SenderID,
				Content:   msg.Content,
				Direction: core.InboundDirection,
				Status:    core.StatusDone,
			}); err != nil {
				log.Warn("failed to persist inbound message", "error", err)
			}

			response, err := cmdRouter.Route(ctx, msg)
			if err != nil {
				log.Error("router failed", "error", err)
				_ = send(ctx, core.OutboundMessage{
					Channel:     msg.Channel,
					RecipientID: msg.SenderID,
					Content:     "Something went wrong processing your message.",
				})
				return
			}

			if response != "" {
				// Persist outbound message.
				if err := st.CreateMessage(ctx, &core.Message{
					Channel:   msg.Channel,
					SenderID:  msg.SenderID,
					Content:   response,
					Direction: core.OutboundDirection,
					Status:    core.StatusDone,
				}); err != nil {
					log.Warn("failed to persist outbound message", "error", err)
				}
				_ = send(ctx, core.OutboundMessage{
					Channel:     msg.Channel,
					RecipientID: msg.SenderID,
					Content:     response,
					ReplyToID:   msg.ID,
				})
			}
		}
	}

	// Wire message handlers.
	tg.SetMessageHandler(handleMessage(tg.Send))

	// Signal handling for graceful shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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
	_ = st.Close()
	_ = audit.CloseGlobal()
}
