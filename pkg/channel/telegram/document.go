package telegram

import (
	"context"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// SendDocumentAttachment uploads content as a document file to a Telegram chat.
// It writes the content to a temporary file, uploads it via SendDocument, and
// cleans up the temp file afterward.
func SendDocumentAttachment(ctx context.Context, b telegramBot, chatID int64, content, filename, caption string) error {
	tmp, err := os.CreateTemp("", "ai-chat-*.md")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	reader, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("reopening temp file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	_, err = b.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     reader,
		},
		Caption:   truncateCaption(caption, 1024),
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		return fmt.Errorf("sending document: %w", err)
	}

	return nil
}

// truncateCaption truncates a string to maxLen runes, appending "..." if
// truncation occurred. It is rune-aware, unlike the byte-based truncate() in
// keyboards.go.
func truncateCaption(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen-3]) + "..."
}

// documentFilename returns a filename for a document attachment based on the
// session name and message index.
func documentFilename(sessionName string, msgIdx int) string {
	return fmt.Sprintf("response-%s-%d.md", sessionName, msgIdx)
}
