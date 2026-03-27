package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	aichat "github.com/mylocalgpt/ai-chat"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	webpkg "github.com/mylocalgpt/ai-chat/pkg/channel/web"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file (default: ~/.ai-chat/config.json)")
	fs.Parse(args)

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

	if err := store.Migrate(db); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	st := store.New(db)

	// Initialize Telegram adapter.
	tg, err := telegram.NewTelegramAdapter(telegram.TelegramAdapterConfig{
		BotToken:     cfg.Telegram.BotToken,
		AllowedUsers: cfg.Telegram.AllowedUsers,
	}, st)
	if err != nil {
		slog.Error("failed to create telegram adapter", "error", err)
		os.Exit(1)
	}

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
		w.Write([]byte(`{"status":"ok"}`))
	})

	if webpkg.DevMode() {
		mux.Handle("/", webpkg.NewDevProxyHandler())
		slog.Info("dev mode: proxying web requests to Vite at :5173")
	} else {
		mux.Handle("/", webpkg.NewFileHandler(aichat.WebDist))
	}

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
		}
	}()

	slog.Info("server started", "addr", cfg.HTTPAddr, "db", cfg.DBPath)

	// Block until signal.
	<-ctx.Done()

	slog.Info("shutting down")
	tg.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	st.Close()
}
