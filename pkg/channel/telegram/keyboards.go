package telegram

import (
	"fmt"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/router"
)

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func SecurityWarningKeyboard(msgRef string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Yes, send", CallbackData: "sec:approve:" + msgRef},
				{Text: "Cancel", CallbackData: "sec:reject:" + msgRef},
			},
		},
	}
}

func WorkspacePickerKeyboard(data *router.WorkspacePickerData) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(data.Workspaces))
	for _, ws := range data.Workspaces {
		label := ws.Name
		if ws.ID == data.ActiveWorkspaceID {
			label = "-> " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{{Text: label, CallbackData: fmt.Sprintf("ws:%d", ws.ID)}})
	}
	if len(rows) == 0 {
		rows = append(rows, []models.InlineKeyboardButton{{Text: "No workspaces configured", CallbackData: "ws:none"}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func SessionPickerKeyboard(data *router.SessionPickerData) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(data.Sessions))
	for _, sess := range data.Sessions {
		label := fmt.Sprintf("%s [%s] %s", sess.Slug, sess.Agent, sess.Status)
		if sess.ID == data.ActiveSessionID {
			label = "-> " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{{Text: label, CallbackData: fmt.Sprintf("sess:%d:%d", data.WorkspaceID, sess.ID)}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func formatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ShowFullKeyboard returns an inline keyboard with a "Show full output" button.
// The callback data format is "full:{sessionName}:{msgIdx}".
func ShowFullKeyboard(sessionName string, msgIdx int) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "Show full output", CallbackData: fmt.Sprintf("full:%s:%d", sessionName, msgIdx)}},
		},
	}
}
