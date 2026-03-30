package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

var _ core.Channel = (*TelegramAdapter)(nil)
var _ core.StreamingChannel = (*TelegramAdapter)(nil)
var _ core.ResponseSender = (*TelegramAdapter)(nil)

type telegramBot interface {
	GetMe(ctx context.Context) (*models.User, error)
	Start(ctx context.Context)
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
	SendChatAction(ctx context.Context, params *bot.SendChatActionParams) (bool, error)
	SetMyCommands(ctx context.Context, params *bot.SetMyCommandsParams) (bool, error)
	DeleteMessage(ctx context.Context, params *bot.DeleteMessageParams) (bool, error)
	EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error)
	SendDocument(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error)
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

func (b *liveTelegramBot) SendDocument(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	return b.inner.SendDocument(ctx, params)
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

const (
	shortThreshold = 8000  // runes - below this, send directly
	longThreshold  = 20000 // runes - above this, auto-attach document
)

type TelegramAdapter struct {
	bot          telegramBot
	allowedUsers map[int64]bool
	router       Router
	store        *store.Store
	summarizer   *Summarizer
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
		callbackHandler: newCallbackHandler(nil, allowed, nil, nil, ""),
	}

	b, err := bot.New(cfg.BotToken,
		bot.WithDefaultHandler(adapter.handleUpdate),
		bot.WithErrorsHandler(adapter.handleBotError),
	)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}
	adapter.bot = &liveTelegramBot{inner: b}
	adapter.callbackHandler.bot = adapter.bot

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

func (t *TelegramAdapter) SetSummarizer(s *Summarizer) {
	t.summarizer = s
}

// SetShutdownFunc sets the function called to trigger full application shutdown
// when a polling conflict is detected at runtime.
func (t *TelegramAdapter) SetShutdownFunc(fn context.CancelFunc) {
	t.shutdownFunc = fn
}

// SetAbortFunc sets the function called to abort an agent session when a user
// presses the "Stop" inline keyboard button.
func (t *TelegramAdapter) SetAbortFunc(fn func(ctx context.Context, agentSessionID string) error) {
	t.callbackHandler.abortFunc = fn
}

// SetResponsesDir sets the directory used to read response files when the user
// presses the "Show full output" inline keyboard button.
func (t *TelegramAdapter) SetResponsesDir(dir string) {
	t.callbackHandler.responsesDir = dir
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
	reqID := core.NewRequestID()
	ctx = core.WithRequestID(ctx, reqID)
	if callbackBot, ok := t.bot.(telegramCallbackBot); ok {
		t.callbackHandler.handleCallback(ctx, callbackBot, update)
	}
}

func (t *TelegramAdapter) handleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		slog.Warn("nil message in update")
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

	reqID := core.NewRequestID()
	ctx = core.WithRequestID(ctx, reqID)

	msg := core.InboundMessage{
		ID:        strconv.FormatInt(int64(update.Message.ID), 10),
		Channel:   "telegram",
		SenderID:  strconv.FormatInt(update.Message.Chat.ID, 10),
		Content:   update.Message.Text,
		Timestamp: time.Unix(int64(update.Message.Date), 0),
		Raw:       update,
	}

	slog.Debug("message received", "req", reqID, "sender", msg.SenderID, "msg_id", msg.ID)

	if t.router != nil {
		slog.Debug("routing message", "req", reqID)
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
		slog.Warn("empty chunks after formatting", "req", core.RequestID(ctx), "sender", msg.RecipientID)
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
			return fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
		}

		if i < len(chunks)-1 {
			time.Sleep(150 * time.Millisecond)
		}
	}
	return nil
}

// SendStreaming consumes a channel of agent events, showing real-time progress
// via a ProgressReporter, and accumulating response text and token/cost data.
// It returns a StreamResult containing the final text and accumulated metrics.
// The caller (forwardResponses) is responsible for delivering the final message
// via SendResponse or Send.
func (t *TelegramAdapter) SendStreaming(ctx context.Context, chatID int64, replyToID int, agentSessionID string, events <-chan core.AgentEvent) (core.StreamResult, error) {
	slog.Debug("streaming started", "req", core.RequestID(ctx), "chat_id", chatID)
	reporter := NewProgressReporter(t.bot, chatID, replyToID, agentSessionID)

	var textBuf strings.Builder
	var result core.StreamResult
	started := false
	done := false
	errored := false

	for event := range events {
		if done {
			continue // drain remaining events after idle/error
		}

		// Check context cancellation before processing each event.
		select {
		case <-ctx.Done():
			reporter.Finish(ctx)
			return core.StreamResult{}, ctx.Err()
		default:
		}

		switch event.Type {
		case core.EventBusy:
			if !started {
				started = true
				if err := reporter.Start(ctx); err != nil {
					slog.Error("progress reporter start failed", "chat_id", chatID, "error", err)
				}
			}

		case core.EventTextDelta:
			textBuf.WriteString(event.Text)
			reporter.Update(ctx, event)

		case core.EventText:
			textBuf.Reset()
			textBuf.WriteString(event.Text)

		case core.EventToolUse:
			reporter.Update(ctx, event)

		case core.EventToolResult:
			reporter.Update(ctx, event)

		case core.EventStepFinish:
			if event.Tokens != nil {
				result.InputTokens += event.Tokens.Input
				result.OutputTokens += event.Tokens.Output
				slog.Debug("step finished",
					"chat_id", chatID,
					"tokens_in", event.Tokens.Input,
					"tokens_out", event.Tokens.Output,
					"cost", event.Cost,
				)
			}
			result.Cost += event.Cost

		case core.EventIdle:
			reporter.Finish(ctx)
			done = true

		case core.EventError:
			slog.Warn("stream error event", "req", core.RequestID(ctx), "err", event.Text)
			reporter.Finish(ctx)
			_ = t.Send(ctx, core.OutboundMessage{
				Channel:     "telegram",
				RecipientID: strconv.FormatInt(chatID, 10),
				Content:     "Error: " + event.Text,
				ReplyToID:   strconv.Itoa(replyToID),
			})
			done = true
			errored = true
		}
	}

	// Ensure reporter is cleaned up even if channel closed without idle/error.
	if started && !done {
		slog.Warn("stream ended abnormally", "req", core.RequestID(ctx))
		reporter.Finish(ctx)
	}

	// Error already communicated to user; return empty result.
	if errored {
		return core.StreamResult{}, nil
	}

	result.Text = textBuf.String()
	return result, nil
}

// SendResponse implements core.ResponseSender. It routes responses through
// short, medium, or long paths based on content length in runes.
//
// Short (<=8,000 runes): formats as HTML and sends directly via Send().
// Medium (8,001-20,000 runes): summarizes via AI, sends summary with
//
//	"Show full output" inline keyboard button.
//
// Long (>20,000 runes): same as medium, plus auto-attaches the full
//
//	content as a document file.
func (t *TelegramAdapter) SendResponse(ctx context.Context, params core.ResponseParams) error {
	contentLen := utf8.RuneCountInString(params.Content)

	// Short path: send directly via existing Send().
	if contentLen <= shortThreshold {
		slog.Debug("sending response", "req", core.RequestID(ctx), "content_len", contentLen, "path", "direct")
		content := params.Content
		if footer := formatTokenFooter(params.InputTokens, params.OutputTokens, params.Cost); footer != "" {
			content += footer
		}
		return t.Send(ctx, core.OutboundMessage{
			Channel:     "telegram",
			RecipientID: strconv.FormatInt(params.ChatID, 10),
			Content:     content,
			ReplyToID:   params.ReplyToID,
		})
	}

	// Long path: typing indicator + summarization.
	path := "summarized"
	if contentLen > longThreshold {
		path = "document"
	}
	slog.Debug("sending response", "req", core.RequestID(ctx), "content_len", contentLen, "path", path)

	typingCtx, typingCancel := context.WithCancel(ctx)
	defer typingCancel()

	// Send one immediately so user sees feedback before summarization begins.
	_, _ = t.bot.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: params.ChatID,
		Action: models.ChatActionTyping,
	})

	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				_, _ = t.bot.SendChatAction(typingCtx, &bot.SendChatActionParams{
					ChatID: params.ChatID,
					Action: models.ChatActionTyping,
				})
			}
		}
	}()

	// Summarize content.
	var summary string
	if t.summarizer != nil {
		var err error
		summary, err = t.summarizer.Summarize(ctx, params.Content, params.Workspace)
		if err != nil {
			slog.Warn("summarization failed, using fallback", "error", err)
			summary = fallbackSummary(params.Content, 2000)
		}
	} else {
		summary = fallbackSummary(params.Content, 2000)
	}

	// Very long content: auto-attach document.
	if contentLen > longThreshold {
		filename := documentFilename(params.SessionName, params.MsgIdx)
		caption := truncateCaption(summary, 200)
		if err := SendDocumentAttachment(ctx, t.bot, params.ChatID, params.Content, filename, caption); err != nil {
			slog.Error("failed to send document attachment", "chat_id", params.ChatID, "error", err)
			// Continue to send the summary message even if doc upload fails.
		}
	}

	// Append token footer to summary before sending.
	if footer := formatTokenFooter(params.InputTokens, params.OutputTokens, params.Cost); footer != "" {
		summary += footer
	}

	// Format and send summary with "Show full output" button.
	formatted := FormatHTML(summary)
	keyboard := ShowFullKeyboard(params.SessionName, params.MsgIdx)

	sendParams := &bot.SendMessageParams{
		ChatID:    params.ChatID,
		Text:      formatted,
		ParseMode: models.ParseModeHTML,
		LinkPreviewOptions: &models.LinkPreviewOptions{
			IsDisabled: bot.True(),
		},
		ReplyMarkup: keyboard,
	}

	if params.ReplyToID != "" {
		id, err := strconv.Atoi(params.ReplyToID)
		if err == nil {
			sendParams.ReplyParameters = &models.ReplyParameters{
				MessageID: id,
			}
		}
	}

	typingCancel() // Stop typing indicator before sending.

	_, err := t.bot.SendMessage(ctx, sendParams)
	if err != nil {
		return fmt.Errorf("sending summary message to %d: %w", params.ChatID, err)
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
		slog.Warn("unknown result kind", "kind", result.Kind)
		return nil
	}
}
