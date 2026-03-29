package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/router"
)

type callbackHandler struct {
	router Router
}

func newCallbackHandler(r Router) *callbackHandler {
	return &callbackHandler{
		router: r,
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
	if data == "none" || h.router == nil {
		return
	}
	workspaceID, err := strconv.ParseInt(data, 10, 64)
	if err != nil {
		slog.Warn("failed to parse workspace callback", "data", data, "error", err)
		return
	}
	result, err := h.router.HandleWorkspaceSelection(ctx, strconv.FormatInt(chatID, 10), "telegram", workspaceID)
	if err != nil {
		slog.Error("failed to route workspace callback", "workspace_id", workspaceID, "error", err)
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "Failed to switch workspace.",
		})
		return
	}
	h.renderCallbackResult(ctx, b, chatID, messageID, result)
}

func (h *callbackHandler) handleSessionCallback(ctx context.Context, b *bot.Bot, chatID int64, messageID int, data string) {
	if h.router == nil {
		return
	}
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		return
	}
	workspaceID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return
	}
	sessionID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return
	}
	result, err := h.router.HandleSessionSelection(ctx, strconv.FormatInt(chatID, 10), "telegram", workspaceID, sessionID)
	if err != nil {
		slog.Error("failed to route session callback", "workspace_id", workspaceID, "session_id", sessionID, "error", err)
		return
	}
	h.renderCallbackResult(ctx, b, chatID, messageID, result)
}

func (h *callbackHandler) handleSecurityCallback(ctx context.Context, b *bot.Bot, chatID int64, messageID int, data string) {
	if h.router == nil {
		return
	}
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		return
	}
	approved := parts[0] == "approve"
	result, err := h.router.HandleSecurityDecision(ctx, strconv.FormatInt(chatID, 10), "telegram", parts[1], approved)
	if err != nil {
		slog.Error("failed to route security callback", "error", err)
		return
	}
	h.renderCallbackResult(ctx, b, chatID, messageID, result)
}

func (h *callbackHandler) renderCallbackResult(ctx context.Context, b *bot.Bot, chatID int64, messageID int, result router.Result) {
	text := result.Text
	if result.Kind == router.ResultWorkspacePicker && result.WorkspacePicker != nil {
		text = result.WorkspacePicker.Prompt
	}
	if result.Kind == router.ResultSessionPicker && result.SessionPicker != nil {
		text = result.SessionPicker.Prompt
	}
	if text == "" {
		return
	}
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: text})
}
