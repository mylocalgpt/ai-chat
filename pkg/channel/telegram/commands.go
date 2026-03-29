package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// builtinCommands are always registered and cannot be overwritten by workspace names.
var builtinCommands = map[string]string{
	"status":     "Show active workspace and sessions",
	"new":        "Start a new session",
	"workspaces": "List and switch workspaces",
	"sessions":   "List sessions for current workspace",
	"clear":      "Clear session and start fresh",
	"agent":      "Switch agent (opencode/copilot)",
	"kill":       "Kill current agent session",
}

func (t *TelegramAdapter) SyncCommands(ctx context.Context) error {
	var commands []models.BotCommand

	for cmd, desc := range builtinCommands {
		commands = append(commands, models.BotCommand{
			Command:     cmd,
			Description: desc,
		})
	}

	_, err := t.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: commands,
	})
	if err != nil {
		return fmt.Errorf("setting telegram bot commands: %w", err)
	}

	slog.Info("telegram commands synced", "count", len(commands))
	return nil
}

// OnWorkspacesChanged re-syncs Telegram slash commands after workspace changes.
// Errors are logged but not returned (fire-and-forget).
func (t *TelegramAdapter) OnWorkspacesChanged() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := t.SyncCommands(ctx); err != nil {
		slog.Error("failed to sync telegram commands", "error", err)
	}
}
