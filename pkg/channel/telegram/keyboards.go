package telegram

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

const defaultWorkspaceLimit = 5

type SessionPreview struct {
	Name         string
	FirstUserMsg string
	LastAgentMsg string
	Status       string
	Age          string
}

func WorkspaceKeyboard(workspaces []core.Workspace, limit int) *models.InlineKeyboardMarkup {
	if limit <= 0 {
		limit = defaultWorkspaceLimit
	}

	var rows [][]models.InlineKeyboardButton

	count := len(workspaces)
	if count > limit {
		count = limit
	}

	if count == 0 {
		return &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "No workspaces configured", CallbackData: "ws:none"}},
			},
		}
	}

	for i := 0; i < count; i++ {
		ws := workspaces[i]
		encodedName := url.QueryEscape(ws.Name)
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: ws.Name, CallbackData: "ws:" + encodedName},
		})
	}

	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "Search...", CallbackData: "ws:search"},
	})

	return &models.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}

func SessionKeyboard(sessions []SessionPreview) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton

	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "New session", CallbackData: "sess:new"},
	})

	for _, sess := range sessions {
		preview := formatSessionPreview(sess)
		encodedName := url.QueryEscape(sess.Name)
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: preview, CallbackData: "sess:" + encodedName},
		})
	}

	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "Back", CallbackData: "sess:back"},
	})

	return &models.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}

func formatSessionPreview(sess SessionPreview) string {
	userMsg := truncate(sess.FirstUserMsg, 30)
	agentMsg := truncate(sess.LastAgentMsg, 30)

	preview := fmt.Sprintf("%s: \"%s\" -> \"%s\"", sess.Name, userMsg, agentMsg)

	if sess.Status != "" {
		preview += fmt.Sprintf(" (%s", sess.Status)
		if sess.Age != "" {
			preview += fmt.Sprintf(", %s", sess.Age)
		}
		preview += ")"
	} else if sess.Age != "" {
		preview += fmt.Sprintf(" (%s)", sess.Age)
	}

	return truncate(preview, 64)
}

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
				{Text: "Yes, send", CallbackData: "sec:yes:" + msgRef},
				{Text: "Cancel", CallbackData: "sec:no:" + msgRef},
			},
		},
	}
}

func BuildSessionPreviews(ctx context.Context, st *store.Store, workspaceID int64, responseDir string) ([]SessionPreview, error) {
	sessions, err := st.ListSessionsForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	var previews []SessionPreview
	for _, sess := range sessions {
		preview := SessionPreview{
			Name:   fmt.Sprintf("ai-chat-%s-%s", getWorkspaceName(ctx, st, sess.WorkspaceID), sess.Slug),
			Status: string(sess.Status),
			Age:    formatAge(sess.LastActivity),
		}

		responseFile := fmt.Sprintf("%s/ai-chat-%s-%s.json", responseDir, getWorkspaceName(ctx, st, sess.WorkspaceID), sess.Slug)
		firstUser, lastAgent := extractMessagesFromResponseFile(responseFile)
		preview.FirstUserMsg = firstUser
		preview.LastAgentMsg = lastAgent

		previews = append(previews, preview)
	}

	return previews, nil
}

func getWorkspaceName(ctx context.Context, st *store.Store, workspaceID int64) string {
	ws, err := st.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		return "unknown"
	}
	return ws.Name
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

func extractMessagesFromResponseFile(filePath string) (firstUser, lastAgent string) {
	firstUser, lastAgent, err := executor.SessionPreview(filePath)
	if err != nil {
		return "", ""
	}
	return firstUser, lastAgent
}
