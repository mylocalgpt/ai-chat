package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	aichat "github.com/mylocalgpt/ai-chat"
	"github.com/mylocalgpt/ai-chat/pkg/audit"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	webpkg "github.com/mylocalgpt/ai-chat/pkg/channel/web"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/orchestrator"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file (default: ~/.config/ai-chat/config.json, then ./config.json)")
	_ = fs.Parse(args)

	// Set up structured JSON logging to stderr.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	// Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
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

	// Initialize orchestrator.
	router := orchestrator.NewRouter(cfg.OpenRouter.APIKey)
	orch, err := orchestrator.Setup(router, st)
	if err != nil {
		slog.Error("failed to set up orchestrator", "error", err)
		os.Exit(1)
	}

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

			result, err := orch.HandleMessage(ctx, msg)
			if err != nil {
				log.Error("orchestrator failed", "error", err)
				_ = send(ctx, core.OutboundMessage{
					Channel:     msg.Channel,
					RecipientID: msg.SenderID,
					Content:     "Something went wrong processing your message.",
				})
				return
			}

			var wsID *int64
			response := result.Response
			if result.Action.Type == orchestrator.ActionAgentTask {
				ws, err := resolveWorkspace(ctx, st, result.Action.Workspace, msg.SenderID, msg.Channel)
				if err != nil {
					log.Error("no workspace available", "error", err)
					_ = send(ctx, core.OutboundMessage{
						Channel:     msg.Channel,
						RecipientID: msg.SenderID,
						Content:     "No workspace found. Create one first or specify a workspace name.",
					})
					return
				}
				wsID = &ws.ID
				agent := result.Action.Agent
				if agent == "" {
					agent = "claude"
				}
				resp, err := exec.Execute(ctx, *ws, agent, msg.Content)
				if err != nil {
					log.Error("executor failed", "error", err)
					_ = send(ctx, core.OutboundMessage{
						Channel:     msg.Channel,
						RecipientID: msg.SenderID,
						Content:     "Something went wrong with the agent. Please try again.",
					})
					return
				}
				response = resp
			}

			if response != "" {
				// Persist outbound message.
				if err := st.CreateMessage(ctx, &core.Message{
					Channel:     msg.Channel,
					SenderID:    msg.SenderID,
					WorkspaceID: wsID,
					Content:     response,
					Direction:   core.OutboundDirection,
					Status:      core.StatusDone,
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

	// HTTP server with health endpoint.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Start web channel (registers /ws on mux).
	webCh := webpkg.NewWebChannel(st, mux)
	webCh.SetMessageHandler(handleMessage(webCh.Send))
	if err := webCh.Start(ctx); err != nil {
		slog.Error("failed to start web channel", "error", err)
		os.Exit(1)
	}

	if webpkg.DevMode() {
		mux.Handle("/", webpkg.NewDevProxyHandler())
		slog.Info("dev mode: proxying web requests to Vite at :5173")
	} else {
		mux.Handle("/", webpkg.NewFileHandler(aichat.WebDist))
	}

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
		}
	}()

	slog.Info("server started", "addr", cfg.HTTPAddr, "db", cfg.DBPath)

	// Block until signal.
	<-ctx.Done()

	slog.Info("shutting down")
	_ = tg.Stop()
	_ = webCh.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = st.Close()
	_ = audit.CloseGlobal()
}

// resolveWorkspace looks up a workspace by name. If name is empty, it falls
// back to the user's active workspace from their stored context.
func resolveWorkspace(ctx context.Context, st *store.Store, name, senderID, channel string) (*core.Workspace, error) {
	if name != "" {
		return st.GetWorkspace(ctx, name)
	}
	uc, err := st.GetUserContext(ctx, senderID, channel)
	if err != nil {
		return nil, fmt.Errorf("no workspace specified and no active workspace set")
	}
	return st.GetWorkspaceByID(ctx, uc.ActiveWorkspaceID)
}
