package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// ProgressReporter manages a temporary status message in Telegram that shows
// progress while an agent processes a request. It throttles edits to stay
// within Telegram rate limits and deletes the status message when finished.
type ProgressReporter struct {
	bot            telegramBot
	chatID         int64
	replyToID      int
	agentSessionID string
	statusMsg      *models.Message
	lastEdit       time.Time
	throttle       time.Duration
	tools          []string
	textLen        int
	mu             sync.Mutex
}

// NewProgressReporter creates a ProgressReporter that will post status updates
// into the given chat, replying to replyToID. The agentSessionID is embedded
// in the Stop button callback data.
func NewProgressReporter(b telegramBot, chatID int64, replyToID int, agentSessionID string) *ProgressReporter {
	return &ProgressReporter{
		bot:            b,
		chatID:         chatID,
		replyToID:      replyToID,
		agentSessionID: agentSessionID,
		throttle:       1500 * time.Millisecond,
	}
}

// Start sends the initial "Thinking..." status message. It must be called
// before Update or Finish.
func (p *ProgressReporter) Start(ctx context.Context) error {
	msg, err := p.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    p.chatID,
		Text:      "Thinking...",
		ParseMode: models.ParseModeHTML,
		ReplyParameters: &models.ReplyParameters{
			MessageID: p.replyToID,
		},
		ReplyMarkup: stopKeyboard(p.agentSessionID),
	})
	if err != nil {
		return fmt.Errorf("sending progress message: %w", err)
	}
	p.statusMsg = msg
	p.lastEdit = time.Now()
	return nil
}

// Update processes a streaming agent event, updating internal counters and
// editing the status message when the throttle window has elapsed.
func (p *ProgressReporter) Update(ctx context.Context, event core.AgentEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch event.Type {
	case core.EventTextDelta:
		p.textLen += len(event.Text)
	case core.EventToolUse:
		p.tools = append(p.tools, event.ToolName)
	case core.EventToolResult:
		// acknowledged, no counter update
	}

	if time.Since(p.lastEdit) < p.throttle {
		return
	}

	text := p.buildStatusText()
	p.editStatus(ctx, text)
	p.lastEdit = time.Now()
}

// Finish deletes the progress status message. Safe to call multiple times or
// when Start was never called.
func (p *ProgressReporter) Finish(ctx context.Context) {
	if p.statusMsg == nil {
		return
	}
	_, err := p.bot.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    p.chatID,
		MessageID: p.statusMsg.ID,
	})
	if err != nil {
		slog.Warn("failed to delete progress message", "chat_id", p.chatID, "msg_id", p.statusMsg.ID, "error", err)
	}
	p.statusMsg = nil
}

// buildStatusText assembles the current progress text from internal counters.
func (p *ProgressReporter) buildStatusText() string {
	var b strings.Builder
	b.WriteString("Working")

	if len(p.tools) > 0 {
		start := 0
		if len(p.tools) > 3 {
			start = len(p.tools) - 3
		}
		b.WriteString("\n\nTools: ")
		b.WriteString(strings.Join(p.tools[start:], " -> "))
	}

	if p.textLen > 0 {
		fmt.Fprintf(&b, "\n\nResponse: %d chars so far", p.textLen)
	}

	return b.String()
}

// editStatus updates the status message text, silently ignoring "message is
// not modified" errors from Telegram.
func (p *ProgressReporter) editStatus(ctx context.Context, text string) {
	if p.statusMsg == nil {
		return
	}
	_, err := p.bot.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    p.chatID,
		MessageID: p.statusMsg.ID,
		Text:      text,
	})
	if err != nil {
		if strings.Contains(err.Error(), "message is not modified") {
			return
		}
		slog.Warn("failed to edit progress message", "chat_id", p.chatID, "msg_id", p.statusMsg.ID, "error", err)
	}
}

// stopKeyboard returns an inline keyboard with a single Stop button whose
// callback data encodes the agent session ID.
func stopKeyboard(agentSessionID string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text:         "Stop",
					CallbackData: "stop:" + agentSessionID,
				},
			},
		},
	}
}
