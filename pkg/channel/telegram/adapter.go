package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

var _ core.Channel = (*TelegramAdapter)(nil)

type Router interface {
	Route(ctx context.Context, msg core.InboundMessage) (string, error)
}

type SessionManager interface {
	ResponseCh() <-chan core.ResponseEvent
}

type TelegramAdapterConfig struct {
	BotToken     string
	AllowedUsers []int64
}

type TelegramAdapter struct {
	bot          *bot.Bot
	allowedUsers map[int64]bool
	router       Router
	sessionMgr   SessionManager
	store        *store.Store
	cancel       context.CancelFunc
	running      atomic.Bool

	callbackHandler *callbackHandler

	sessionToChat      map[int64]string
	sessionToChatMutex sync.RWMutex

	pendingSearch map[string]time.Time
}

func NewTelegramAdapter(cfg TelegramAdapterConfig, st *store.Store) (*TelegramAdapter, error) {
	allowed := make(map[int64]bool, len(cfg.AllowedUsers))
	for _, uid := range cfg.AllowedUsers {
		allowed[uid] = true
	}

	adapter := &TelegramAdapter{
		allowedUsers:    allowed,
		store:           st,
		sessionToChat:   make(map[int64]string),
		pendingSearch:   make(map[string]time.Time),
		callbackHandler: newCallbackHandler(st),
	}

	b, err := bot.New(cfg.BotToken, bot.WithDefaultHandler(adapter.handleUpdate))
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}
	adapter.bot = b

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix, adapter.callbackHandler.handleCallback)

	return adapter, nil
}

func (t *TelegramAdapter) SetRouter(r Router) {
	t.router = r
}

func (t *TelegramAdapter) SetSessionManager(sm SessionManager) {
	t.sessionMgr = sm
}

func (t *TelegramAdapter) SetMessageHandler(fn func(context.Context, core.InboundMessage)) {
	t.router = &messageHandlerRouter{handler: fn}
}

type messageHandlerRouter struct {
	handler func(context.Context, core.InboundMessage)
}

func (r *messageHandlerRouter) Route(ctx context.Context, msg core.InboundMessage) (string, error) {
	if r.handler != nil {
		r.handler(ctx, msg)
	}
	return "", nil
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

	if t.handleSpecialCommands(ctx, msg) {
		return
	}

	if t.router != nil {
		response, err := t.router.Route(ctx, msg)
		if err != nil {
			if strings.Contains(err.Error(), "user context") && strings.Contains(err.Error(), "not found") {
				workspaces, listErr := t.store.ListWorkspaces(ctx)
				if listErr != nil {
					slog.Error("listing workspaces for new user", "error", listErr)
					return
				}
				if len(workspaces) == 0 {
					chatID, _ := strconv.ParseInt(msg.SenderID, 10, 64)
					_, _ = t.bot.SendMessage(ctx, &bot.SendMessageParams{
						ChatID: chatID,
						Text:   "No workspaces configured. Use the MCP tools to register a workspace first.",
					})
					return
				}
				kb := WorkspaceKeyboard(workspaces, 0)
				chatID, _ := strconv.ParseInt(msg.SenderID, 10, 64)
				_, _ = t.bot.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:      chatID,
					Text:        "Select a workspace to get started:",
					ReplyMarkup: kb,
				})
				return
			}
			slog.Error("router failed", "channel", msg.Channel, "sender", msg.SenderID, "error", err)
			return
		}
		if response != "" {
			_ = t.Send(ctx, core.OutboundMessage{
				Channel:     "telegram",
				RecipientID: msg.SenderID,
				Content:     response,
			})
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

func (t *TelegramAdapter) handleSpecialCommands(ctx context.Context, msg core.InboundMessage) bool {
	content := strings.TrimSpace(msg.Content)

	if content == "/workspaces" {
		workspaces, err := t.store.ListWorkspaces(ctx)
		if err != nil {
			slog.Error("listing workspaces", "error", err)
			return true
		}
		kb := WorkspaceKeyboard(workspaces, 0)
		chatID, _ := strconv.ParseInt(msg.SenderID, 10, 64)
		_, _ = t.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "Select a workspace:",
			ReplyMarkup: kb,
		})
		return true
	}

	if content == "/sessions" {
		uc, err := t.store.GetUserContext(ctx, msg.SenderID, msg.Channel)
		if err != nil {
			chatID, _ := strconv.ParseInt(msg.SenderID, 10, 64)
			_, _ = t.bot.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "No active workspace. Use /workspaces to select one.",
			})
			return true
		}

		sessions, err := t.store.ListSessionsForWorkspace(ctx, uc.ActiveWorkspaceID)
		if err != nil {
			slog.Error("listing sessions", "error", err)
			return true
		}

		var previews []SessionPreview
		for _, s := range sessions {
			previews = append(previews, SessionPreview{
				Name:   fmt.Sprintf("ai-chat-%d-%s", uc.ActiveWorkspaceID, s.Slug),
				Status: s.Status,
				Age:    formatAge(s.LastActivity),
			})
		}

		kb := SessionKeyboard(previews)
		chatID, _ := strconv.ParseInt(msg.SenderID, 10, 64)
		_, _ = t.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "Select a session:",
			ReplyMarkup: kb,
		})
		return true
	}

	return false
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

	if t.sessionMgr != nil {
		go t.listenForResponses(childCtx)
	}

	t.callbackHandler.startCleanup(childCtx)

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

func (t *TelegramAdapter) listenForResponses(ctx context.Context) {
	if t.sessionMgr == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case resp := <-t.sessionMgr.ResponseCh():
			t.sessionToChatMutex.RLock()
			chatID, ok := t.sessionToChat[resp.SessionID]
			t.sessionToChatMutex.RUnlock()

			if !ok {
				chatID = resp.SenderID
			}

			if chatID == "" {
				continue
			}

			_ = t.Send(ctx, core.OutboundMessage{
				Channel:     "telegram",
				RecipientID: chatID,
				Content:     resp.Content,
			})
		}
	}
}

func (t *TelegramAdapter) RegisterSessionChat(sessionID int64, chatID string) {
	t.sessionToChatMutex.Lock()
	defer t.sessionToChatMutex.Unlock()
	t.sessionToChat[sessionID] = chatID
}
