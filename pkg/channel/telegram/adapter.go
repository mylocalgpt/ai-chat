package telegram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// Compile-time interface check.
var _ core.Channel = (*TelegramAdapter)(nil)

// TelegramAdapterConfig holds the configuration for the Telegram adapter.
type TelegramAdapterConfig struct {
	BotToken     string
	AllowedUsers []int64
}

// TelegramAdapter implements core.Channel for Telegram using long polling.
type TelegramAdapter struct {
	bot          *bot.Bot
	allowedUsers map[int64]bool
	msgHandler   func(context.Context, core.InboundMessage)
	store        *store.Store
	cancel       context.CancelFunc
}

// NewTelegramAdapter creates a new Telegram adapter. The bot token is used
// once to create the underlying bot instance and is not stored.
func NewTelegramAdapter(cfg TelegramAdapterConfig, st *store.Store) (*TelegramAdapter, error) {
	allowed := make(map[int64]bool, len(cfg.AllowedUsers))
	for _, uid := range cfg.AllowedUsers {
		allowed[uid] = true
	}

	adapter := &TelegramAdapter{
		allowedUsers: allowed,
		store:        st,
	}

	b, err := bot.New(cfg.BotToken, bot.WithDefaultHandler(adapter.handleUpdate))
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}
	adapter.bot = b

	return adapter, nil
}

// handleUpdate processes incoming Telegram updates. This is a stub that will
// be filled in by Phase 2.
func (t *TelegramAdapter) handleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	slog.Info("received update", "update_id", update.ID)
}

// Start connects to Telegram and begins long polling. It probes the connection
// with GetMe before launching the polling goroutine.
func (t *TelegramAdapter) Start(ctx context.Context) error {
	me, err := t.bot.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram connectivity check (GetMe): %w", err)
	}

	childCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	go t.bot.Start(childCtx)

	slog.Info("telegram bot started", "username", me.Username)
	return nil
}

// Stop cancels the long polling context and shuts down the bot.
func (t *TelegramAdapter) Stop() error {
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

// SetMessageHandler registers the callback invoked for each normalized
// inbound message. Called by the orchestrator before Start.
func (t *TelegramAdapter) SetMessageHandler(fn func(context.Context, core.InboundMessage)) {
	t.msgHandler = fn
}

// Send delivers an outbound message to Telegram. Stub for now, implemented
// in Phase 3.
func (t *TelegramAdapter) Send(ctx context.Context, msg core.OutboundMessage) error {
	return nil
}
