package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
)

type callbackHandler struct {
	router       Router
	allowedUsers map[int64]bool
	abortFunc    func(ctx context.Context, agentSessionID string) error
	bot          telegramBot // full bot interface for SendDocument
	responsesDir string      // directory containing response JSON files
}

func newCallbackHandler(r Router, allowedUsers map[int64]bool, abortFunc func(ctx context.Context, agentSessionID string) error, b telegramBot, responsesDir string) *callbackHandler {
	return &callbackHandler{
		router:       r,
		allowedUsers: allowedUsers,
		abortFunc:    abortFunc,
		bot:          b,
		responsesDir: responsesDir,
	}
}

func (h *callbackHandler) handleCallback(ctx context.Context, b telegramCallbackBot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}

	cb := update.CallbackQuery
	data := cb.Data
	if !h.allowedUsers[cb.From.ID] {
		slog.Warn("unauthorized callback", "user_id", cb.From.ID)
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Not authorized.",
			ShowAlert:       true,
		})
		return
	}

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
	case "stop":
		h.handleStopCallback(ctx, b, chatID, messageID, rest)
	case "full":
		h.handleFullOutputCallback(ctx, b, chatID, messageID, rest)
	default:
		slog.Warn("unknown callback prefix", "prefix", prefix)
	}
}

func (h *callbackHandler) handleWorkspaceCallback(ctx context.Context, b telegramCallbackBot, chatID int64, messageID int, data string) {
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

func (h *callbackHandler) handleSessionCallback(ctx context.Context, b telegramCallbackBot, chatID int64, messageID int, data string) {
	if h.router == nil {
		return
	}
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		slog.Warn("session callback parse failed", "data", data)
		return
	}
	workspaceID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		slog.Warn("session callback parse failed", "data", data, "err", err)
		return
	}
	sessionID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		slog.Warn("session callback parse failed", "data", data, "err", err)
		return
	}
	result, err := h.router.HandleSessionSelection(ctx, strconv.FormatInt(chatID, 10), "telegram", workspaceID, sessionID)
	if err != nil {
		slog.Error("failed to route session callback", "workspace_id", workspaceID, "session_id", sessionID, "error", err)
		return
	}
	h.renderCallbackResult(ctx, b, chatID, messageID, result)
}

func (h *callbackHandler) handleStopCallback(ctx context.Context, b telegramCallbackBot, chatID int64, messageID int, agentSessionID string) {
	if h.abortFunc == nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "Stop not available.",
		})
		return
	}

	if err := h.abortFunc(ctx, agentSessionID); err != nil {
		slog.Error("failed to abort agent session", "agent_session_id", agentSessionID, "error", err)
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "Failed to stop.",
		})
		return
	}

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      "Stopping...",
	})
}

func (h *callbackHandler) handleFullOutputCallback(ctx context.Context, b telegramCallbackBot, chatID int64, messageID int, data string) {
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		slog.Warn("invalid full output callback data", "data", data)
		return
	}
	sessionName := parts[0]
	msgIdx, err := strconv.Atoi(parts[1])
	if err != nil {
		slog.Warn("invalid message index in full output callback", "data", data, "error", err)
		return
	}

	path := executor.ResponseFilePath(h.responsesDir, sessionName)
	rf, err := executor.ReadResponseFile(path)
	if err != nil {
		slog.Warn("failed to read response file for full output", "path", path, "error", err)
		return
	}

	// Find agent message at index.
	agentIdx := 0
	for _, m := range rf.Messages {
		if m.Role == "agent" {
			if agentIdx == msgIdx {
				filename := documentFilename(sessionName, msgIdx)
				caption := truncateCaption(m.Content, 200)

				if err := SendDocumentAttachment(ctx, h.bot, chatID, m.Content, filename, caption); err != nil {
					slog.Warn("failed to send full output document", "error", err)
				}
				return
			}
			agentIdx++
		}
	}

	slog.Warn("message index out of range for full output", "session", sessionName, "idx", msgIdx, "agent_messages", agentIdx)
}

func (h *callbackHandler) handleSecurityCallback(ctx context.Context, b telegramCallbackBot, chatID int64, messageID int, data string) {
	if h.router == nil {
		return
	}
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		slog.Warn("security callback parse failed", "data", data)
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

func (h *callbackHandler) renderCallbackResult(ctx context.Context, b telegramCallbackBot, chatID int64, messageID int, result router.Result) {
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
	params := &bot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: text}
	if formatted := FormatHTML(text); formatted != text {
		params.Text = formatted
		params.ParseMode = models.ParseModeHTML
	}
	if _, err := b.EditMessageText(ctx, params); err != nil {
		slog.Warn("edit message failed", "err", err, "chat_id", chatID, "msg_id", messageID)
	}
}
