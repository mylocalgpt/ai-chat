package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// builtinCommands are always registered and cannot be overwritten by workspace names.
var builtinCommands = map[string]string{
	"status": "Show active workspace and sessions",
	"new":    "Start a new session",
}

// nonAlphanumeric matches any character that is not a lowercase letter, digit,
// or underscore.
var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9_]+`)

// multiUnderscore collapses consecutive underscores.
var multiUnderscore = regexp.MustCompile(`_+`)

// sanitizeCommandName converts a workspace name into a valid Telegram bot
// command (1-32 lowercase letters, digits, underscores). Returns empty string
// if the name cannot be converted.
func sanitizeCommandName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumeric.ReplaceAllString(s, "_")
	s = multiUnderscore.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 32 {
		s = s[:32]
		s = strings.TrimRight(s, "_")
	}
	return s
}

// SyncCommands registers Telegram bot commands from the workspace list.
// Built-in commands are always included. Workspace names that collide with
// built-in commands or produce invalid command names are skipped.
func (t *TelegramAdapter) SyncCommands(ctx context.Context) error {
	var commands []models.BotCommand

	// Built-in commands first.
	for cmd, desc := range builtinCommands {
		commands = append(commands, models.BotCommand{
			Command:     cmd,
			Description: desc,
		})
	}

	workspaces, err := t.store.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("listing workspaces for command sync: %w", err)
	}

	for _, ws := range workspaces {
		cmd := sanitizeCommandName(ws.Name)
		if cmd == "" {
			continue
		}
		if _, builtin := builtinCommands[cmd]; builtin {
			slog.Warn("skipping workspace command that conflicts with built-in", "workspace", ws.Name, "command", cmd)
			continue
		}

		desc := "Switch to " + ws.Name + " workspace"
		if ws.Metadata != nil {
			var meta map[string]any
			if json.Unmarshal(ws.Metadata, &meta) == nil {
				if d, ok := meta["description"].(string); ok && d != "" {
					desc = d
				}
			}
		}

		commands = append(commands, models.BotCommand{
			Command:     cmd,
			Description: desc,
		})
	}

	_, err = t.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{
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
