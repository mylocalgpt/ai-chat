package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
)

type callbackHandler struct {
	pendingMessages map[string]pendingEntry
	pendingMutex    sync.RWMutex
	store           callbackStore
}

type pendingEntry struct {
	expiresAt time.Time
}

type callbackStore interface {
	GetWorkspaceByID(ctx context.Context, id int64) (*core.Workspace, error)
	GetWorkspaceByName(ctx context.Context, name string) (*core.Workspace, error)
}

func newCallbackHandler(store callbackStore) *callbackHandler {
	return &callbackHandler{
		pendingMessages: make(map[string]pendingEntry),
		store:           store,
	}
}

func (h *callbackHandler) handleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}

	cb := update.CallbackQuery
	data := cb.Data

	defer func() {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
		})
	}()

	chatID := int64(0)
	messageID := 0
	if cb.Message.Type == models.MaybeInaccessibleMessageTypeMessage && cb.Message.Message != nil {
		chatID = cb.Message.Message.Chat.ID
		messageID = cb.Message.Message.ID
	}

	parts := strings.SplitN(data, ":", 2)
	if len(parts) < 1 {
		slog.Warn("invalid callback data format", "data", data)
		return
	}

	prefix := parts[0]
	var rest string
	if len(parts) > 1 {
		rest = parts[1]
	}

	switch prefix {
	case "ws":
		h.handleWorkspaceCallback(ctx, b, chatID, messageID, rest)
	case "sess":
		h.handleSessionCallback(ctx, b, chatID, messageID, rest)
	case "sec":
		h.handleSecurityCallback(ctx, b, chatID, messageID, rest)
	default:
		slog.Warn("unknown callback prefix", "prefix", prefix)
	}
}

func (h *callbackHandler) handleWorkspaceCallback(ctx context.Context, b *bot.Bot, chatID int64, messageID int, data string) {
	if data == "none" {
		return
	}

	if data == "search" {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "Type workspace name to search:",
		})
		return
	}

	decodedName, err := url.QueryUnescape(data)
	if err != nil {
		slog.Warn("failed to decode workspace name", "data", data, "error", err)
		decodedName = data
	}

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      fmt.Sprintf("Switched to workspace: %s", decodedName),
	})
}

func (h *callbackHandler) handleSessionCallback(ctx context.Context, b *bot.Bot, chatID int64, messageID int, data string) {
	if data == "new" {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "Creating new session...",
		})
		return
	}

	if data == "back" {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "Select a workspace:",
		})
		return
	}

	decodedName, err := url.QueryUnescape(data)
	if err != nil {
		slog.Warn("failed to decode session name", "data", data, "error", err)
		decodedName = data
	}

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      fmt.Sprintf("Switched to session: %s", decodedName),
	})
}

func (h *callbackHandler) handleSecurityCallback(ctx context.Context, b *bot.Bot, chatID int64, messageID int, data string) {
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      "Security warnings are not yet implemented.",
	})
	slog.Warn("security callback received but feature not implemented", "data", data)
}

func (h *callbackHandler) cleanupExpired() {
	h.pendingMutex.Lock()
	defer h.pendingMutex.Unlock()

	now := time.Now()
	for ref, pending := range h.pendingMessages {
		if now.After(pending.expiresAt) {
			delete(h.pendingMessages, ref)
		}
	}
}

func (h *callbackHandler) startCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.cleanupExpired()
			}
		}
	}()
}
