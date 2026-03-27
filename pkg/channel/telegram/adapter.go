package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

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

// handleUpdate processes incoming Telegram updates. It enforces the allowlist,
// normalizes the update into a core.InboundMessage, and dispatches to the
// registered handler (or echoes back as a default).
func (t *TelegramAdapter) handleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	if update.Message.From == nil {
		slog.Warn("ignoring message with nil sender (channel post or anonymous)")
		return
	}

	userID := update.Message.From.ID
	if !t.allowedUsers[userID] {
		slog.Warn("unauthorized message", "user_id", userID)
		return
	}

	msg := core.InboundMessage{
		ID:        strconv.FormatInt(int64(update.Message.ID), 10),
		Channel:   "telegram",
		SenderID:  strconv.FormatInt(update.Message.Chat.ID, 10),
		Content:   update.Message.Text,
		Timestamp: time.Unix(int64(update.Message.Date), 0),
		Raw:       update,
	}

	if t.msgHandler != nil {
		t.msgHandler(ctx, msg)
		return
	}

	// Default echo behavior for development; replaced when the orchestrator
	// registers its own handler via SetMessageHandler.
	slog.Info("echo", "chat_id", msg.SenderID, "text_len", len(msg.Content))
	t.Send(ctx, core.OutboundMessage{
		Channel:     "telegram",
		RecipientID: msg.SenderID,
		Content:     msg.Content,
	})
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

	if err := t.SyncCommands(ctx); err != nil {
		slog.Error("failed to sync telegram commands on startup", "error", err)
	}

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

// Send delivers an outbound message to Telegram. Long messages are split into
// chunks at natural boundaries before sending.
func (t *TelegramAdapter) Send(ctx context.Context, msg core.OutboundMessage) error {
	chunks := ChunkMessage(msg.Content, TelegramMaxMessageLen)
	if len(chunks) == 0 {
		return nil
	}

	for i, chunk := range chunks {
		replyTo := ""
		if i == 0 {
			replyTo = msg.ReplyToID
		}

		if err := t.sendSingle(ctx, msg.RecipientID, chunk, replyTo, i == 0); err != nil {
			return err
		}

		if i < len(chunks)-1 {
			time.Sleep(150 * time.Millisecond)
		}
	}
	return nil
}

// sendSingle sends a single message to Telegram with optional typing
// indicator, Markdown formatting, and plain-text fallback.
func (t *TelegramAdapter) sendSingle(ctx context.Context, recipientID, text, replyToID string, sendTyping bool) error {
	chatID, err := strconv.ParseInt(recipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing recipient ID %q: %w", recipientID, err)
	}

	if sendTyping {
		_, typingErr := t.bot.SendChatAction(ctx, &bot.SendChatActionParams{
			ChatID: chatID,
			Action: models.ChatActionTyping,
		})
		if typingErr != nil {
			slog.Warn("failed to send typing indicator", "chat_id", chatID, "error", typingErr)
		}
	}

	params := &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeMarkdownV1,
	}

	if replyToID != "" {
		id, err := strconv.Atoi(replyToID)
		if err != nil {
			return fmt.Errorf("parsing reply-to ID %q: %w", replyToID, err)
		}
		params.ReplyParameters = &models.ReplyParameters{
			MessageID: id,
		}
	}

	_, err = t.bot.SendMessage(ctx, params)
	if err != nil {
		if errors.Is(err, bot.ErrorBadRequest) {
			slog.Warn("markdown parse failed, retrying as plain text", "chat_id", chatID)
			params.ParseMode = ""
			_, retryErr := t.bot.SendMessage(ctx, params)
			if retryErr != nil {
				return fmt.Errorf("sending message to %s: %w", recipientID, retryErr)
			}
			return nil
		}
		return fmt.Errorf("sending message to %s: %w", recipientID, err)
	}

	return nil
}
