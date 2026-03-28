package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mylocalgpt/ai-chat/pkg/audit"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	mcppkg "github.com/mylocalgpt/ai-chat/pkg/mcp"
	"github.com/mylocalgpt/ai-chat/pkg/orchestrator"
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
	exec := executor.NewExecutor(st, tmuxRunner, registry)

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

	// Create MCP server with executor.
	mcpCfg := &mcppkg.ServerConfig{AllowedUsers: cfg.Telegram.AllowedUsers}
	mcpSrv := mcppkg.NewServer(st, mcpCfg, mcppkg.WithExecutor(exec))

	// Connect in-process MCP session.
	mcpSession, err := mcpSrv.ConnectInProcess(context.Background())
	if err != nil {
		slog.Error("failed to connect in-process MCP", "error", err)
		os.Exit(1)
	}
	defer func() { _ = mcpSession.Close() }()

	// Initialize orchestrator (only if OpenRouter key is configured).
	var orch *orchestrator.Orchestrator
	if cfg.OpenRouter.APIKey != "" {
		router := orchestrator.NewRouter(cfg.OpenRouter.APIKey)
		orch = orchestrator.NewOrchestrator(router, mcpSession, "google/gemini-2.5-flash")
		if err := orch.Init(context.Background()); err != nil {
			slog.Error("failed to init orchestrator", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Warn("openrouter.api_key not set, orchestrator disabled")
	}

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

			if orch == nil {
				_ = send(ctx, core.OutboundMessage{
					Channel:     msg.Channel,
					RecipientID: msg.SenderID,
					Content:     "Orchestrator not configured. Set openrouter.api_key in config.",
				})
				return
			}

			// Build user context string.
			userContext := buildUserContext(ctx, st, msg.SenderID, msg.Channel)

			response, err := orch.HandleMessage(ctx, msg, userContext)
			if err != nil {
				log.Error("orchestrator failed", "error", err)
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

// buildUserContext queries the store for the user's active workspace and
// available workspaces, then formats them as a string for the LLM system prompt.
func buildUserContext(ctx context.Context, st *store.Store, senderID, channel string) string {
	var b strings.Builder

	if home, err := os.UserHomeDir(); err == nil {
		b.WriteString("Home directory: ")
		b.WriteString(home)
		b.WriteString("\n")
	}

	b.WriteString("Current workspace: ")
	uc, err := st.GetUserContext(ctx, senderID, channel)
	if err == nil && uc.ActiveWorkspaceID > 0 {
		ws, err := st.GetWorkspaceByID(ctx, uc.ActiveWorkspaceID)
		if err == nil {
			b.WriteString(ws.Name)
			b.WriteString(" (")
			b.WriteString(ws.Path)
			b.WriteString(")")
		} else {
			b.WriteString("none")
		}
	} else {
		b.WriteString("none")
	}

	b.WriteString("\nAvailable workspaces: ")
	workspaces, err := st.ListWorkspaces(ctx)
	if err == nil && len(workspaces) > 0 {
		for i, w := range workspaces {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(w.Name)
			b.WriteString(": ")
			b.WriteString(w.Path)
		}
	} else {
		b.WriteString("none")
	}

	return b.String()
}
