package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

var _ core.Channel = (*TelegramAdapter)(nil)

type telegramBot interface {
	GetMe(ctx context.Context) (*models.User, error)
	Start(ctx context.Context)
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
	SendChatAction(ctx context.Context, params *bot.SendChatActionParams) (bool, error)
	SetMyCommands(ctx context.Context, params *bot.SetMyCommandsParams) (bool, error)
	DeleteMessage(ctx context.Context, params *bot.DeleteMessageParams) (bool, error)
	EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error)
}

type telegramCallbackBot interface {
	AnswerCallbackQuery(ctx context.Context, params *bot.AnswerCallbackQueryParams) (bool, error)
	EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error)
}

type liveTelegramBot struct {
	inner *bot.Bot
}

func (b *liveTelegramBot) GetMe(ctx context.Context) (*models.User, error) {
	return b.inner.GetMe(ctx)
}

func (b *liveTelegramBot) Start(ctx context.Context) {
	b.inner.Start(ctx)
}

func (b *liveTelegramBot) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	return b.inner.SendMessage(ctx, params)
}

func (b *liveTelegramBot) SendChatAction(ctx context.Context, params *bot.SendChatActionParams) (bool, error) {
	return b.inner.SendChatAction(ctx, params)
}

func (b *liveTelegramBot) SetMyCommands(ctx context.Context, params *bot.SetMyCommandsParams) (bool, error) {
	return b.inner.SetMyCommands(ctx, params)
}

func (b *liveTelegramBot) DeleteMessage(ctx context.Context, params *bot.DeleteMessageParams) (bool, error) {
	return b.inner.DeleteMessage(ctx, params)
}

func (b *liveTelegramBot) AnswerCallbackQuery(ctx context.Context, params *bot.AnswerCallbackQueryParams) (bool, error) {
	return b.inner.AnswerCallbackQuery(ctx, params)
}

func (b *liveTelegramBot) EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	return b.inner.EditMessageText(ctx, params)
}

type Router interface {
	Route(ctx context.Context, req router.Request) (router.Result, error)
	HandleWorkspaceSelection(ctx context.Context, senderID, channel string, workspaceID int64) (router.Result, error)
	HandleSessionSelection(ctx context.Context, senderID, channel string, workspaceID, sessionID int64) (router.Result, error)
	HandleSecurityDecision(ctx context.Context, senderID, channel, token string, approved bool) (router.Result, error)
}

type TelegramAdapterConfig struct {
	BotToken     string
	AllowedUsers []int64
}

type TelegramAdapter struct {
	bot          telegramBot
	allowedUsers map[int64]bool
	router       Router
	store        *store.Store
	cancel       context.CancelFunc
	shutdownFunc context.CancelFunc
	running      atomic.Bool

	callbackHandler *callbackHandler
}

func NewTelegramAdapter(cfg TelegramAdapterConfig, st *store.Store) (*TelegramAdapter, error) {
	allowed := make(map[int64]bool, len(cfg.AllowedUsers))
	for _, uid := range cfg.AllowedUsers {
		allowed[uid] = true
	}

	adapter := &TelegramAdapter{
		allowedUsers:    allowed,
		store:           st,
		callbackHandler: newCallbackHandler(nil, allowed),
	}

	b, err := bot.New(cfg.BotToken,
		bot.WithDefaultHandler(adapter.handleUpdate),
		bot.WithErrorsHandler(adapter.handleBotError),
	)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}
	adapter.bot = &liveTelegramBot{inner: b}

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		adapter.callbackHandler.handleCallback(ctx, &liveTelegramBot{inner: b}, update)
	})

	return adapter, nil
}

func (t *TelegramAdapter) SetRouter(r Router) {
	t.router = r
	t.callbackHandler.router = r
}

func (t *TelegramAdapter) SetBot(b telegramBot) {
	t.bot = b
}

// SetShutdownFunc sets the function called to trigger full application shutdown
// when a polling conflict is detected at runtime.
func (t *TelegramAdapter) SetShutdownFunc(fn context.CancelFunc) {
	t.shutdownFunc = fn
}

func (t *TelegramAdapter) handleBotError(err error) {
	if strings.Contains(err.Error(), "terminated by other getUpdates request") {
		slog.Error("telegram polling conflict detected: another instance is polling this bot token, shutting down", "error", err)
		if t.shutdownFunc != nil {
			t.shutdownFunc()
		}
		return
	}
	slog.Error("telegram bot error", "error", err)
}

func (t *TelegramAdapter) ProcessUpdate(ctx context.Context, update *models.Update) {
	t.handleUpdate(ctx, nil, update)
}

func (t *TelegramAdapter) ProcessCallback(ctx context.Context, update *models.Update) {
	if callbackBot, ok := t.bot.(telegramCallbackBot); ok {
		t.callbackHandler.handleCallback(ctx, callbackBot, update)
	}
}

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

	if t.router != nil {
		result, err := t.router.Route(ctx, router.Request{Message: &msg})
		if err != nil {
			slog.Error("router failed", "channel", msg.Channel, "sender", msg.SenderID, "error", err)
			return
		}
		if err := t.renderResult(ctx, msg.SenderID, msg.ID, result); err != nil {
			slog.Error("render result failed", "channel", msg.Channel, "sender", msg.SenderID, "error", err)
		}
		return
	}

	slog.Info("echo", "chat_id", msg.SenderID, "text_len", len(msg.Content))
	_ = t.Send(ctx, core.OutboundMessage{
		Channel:     "telegram",
		RecipientID: msg.SenderID,
		Content:     msg.Content,
	})
}

func (t *TelegramAdapter) Start(ctx context.Context) error {
	me, err := t.bot.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram connectivity check (GetMe): %w", err)
	}

	childCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	go t.bot.Start(childCtx)
	t.running.Store(true)

	slog.Info("telegram bot started", "username", me.Username)

	if err := t.SyncCommands(ctx); err != nil {
		slog.Error("failed to sync telegram commands on startup", "error", err)
	}

	return nil
}

func (t *TelegramAdapter) Stop() error {
	if t.cancel != nil {
		t.cancel()
	}
	t.running.Store(false)
	return nil
}

func (t *TelegramAdapter) IsConnected() bool {
	return t.bot != nil && t.running.Load()
}

func (t *TelegramAdapter) Send(ctx context.Context, msg core.OutboundMessage) error {
	formatted := FormatHTML(msg.Content)
	chunks := SplitMessage(formatted, FormattedMaxLen)
	if len(chunks) == 0 {
		return nil
	}

	chatID, err := strconv.ParseInt(msg.RecipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing recipient ID %q: %w", msg.RecipientID, err)
	}

	for i, chunk := range chunks {
		replyTo := ""
		if i == 0 {
			replyTo = msg.ReplyToID
		}

		if i == 0 {
			_, _ = t.bot.SendChatAction(ctx, &bot.SendChatActionParams{
				ChatID: chatID,
				Action: models.ChatActionTyping,
			})
		}

		if err := SendHTML(ctx, t.bot, chatID, chunk, replyTo); err != nil {
			return err
		}

		if i < len(chunks)-1 {
			time.Sleep(150 * time.Millisecond)
		}
	}
	return nil
}

func (t *TelegramAdapter) renderResult(ctx context.Context, recipientID, replyToID string, result router.Result) error {
	switch result.Kind {
	case router.ResultNoReply:
		return nil
	case router.ResultText:
		if result.Text == "" {
			return nil
		}
		return t.Send(ctx, core.OutboundMessage{Channel: "telegram", RecipientID: recipientID, Content: result.Text, ReplyToID: replyToID})
	case router.ResultWorkspacePicker:
		if result.WorkspacePicker == nil {
			return nil
		}
		chatID, err := strconv.ParseInt(recipientID, 10, 64)
		if err != nil {
			return err
		}
		_, err = t.bot.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: result.WorkspacePicker.Prompt, ReplyMarkup: WorkspacePickerKeyboard(result.WorkspacePicker)})
		return err
	case router.ResultSessionPicker:
		if result.SessionPicker == nil {
			return nil
		}
		chatID, err := strconv.ParseInt(recipientID, 10, 64)
		if err != nil {
			return err
		}
		_, err = t.bot.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: result.SessionPicker.Prompt, ReplyMarkup: SessionPickerKeyboard(result.SessionPicker)})
		return err
	case router.ResultSecurityConfirmation:
		if result.SecurityConfirmation == nil {
			return nil
		}
		chatID, err := strconv.ParseInt(recipientID, 10, 64)
		if err != nil {
			return err
		}
		_, err = t.bot.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: result.SecurityConfirmation.Summary, ReplyMarkup: SecurityWarningKeyboard(result.SecurityConfirmation.Token)})
		return err
	default:
		return nil
	}
}
