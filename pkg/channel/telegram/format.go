package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type codeBlock struct {
	content  string
	language string
	isInline bool
}

const placeholderPrefix = "\x00CODEBLOCK"

func extractCodeBlocks(text string) (string, []codeBlock) {
	var blocks []codeBlock
	result := text

	fencedRegex := regexp.MustCompile("(?s)```(\\w*)\n?(.*?)```")
	inlineRegex := regexp.MustCompile("`([^`]+)`")

	counter := 0

	result = fencedRegex.ReplaceAllStringFunc(result, func(match string) string {
		submatches := fencedRegex.FindStringSubmatch(match)
		lang := ""
		content := ""
		if len(submatches) >= 3 {
			lang = submatches[1]
			content = submatches[2]
		}
		blocks = append(blocks, codeBlock{
			content:  content,
			language: lang,
			isInline: false,
		})
		placeholder := placeholderPrefix + strconv.Itoa(counter) + "\x00"
		counter++
		return placeholder
	})

	result = inlineRegex.ReplaceAllStringFunc(result, func(match string) string {
		submatches := inlineRegex.FindStringSubmatch(match)
		content := ""
		if len(submatches) >= 2 {
			content = submatches[1]
		}
		blocks = append(blocks, codeBlock{
			content:  content,
			isInline: true,
		})
		placeholder := placeholderPrefix + strconv.Itoa(counter) + "\x00"
		counter++
		return placeholder
	})

	return result, blocks
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	text = strings.ReplaceAll(text, "\"", "&quot;")
	return text
}

func convertMarkdownToHTML(text string) string {
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkRegex.ReplaceAllString(text, `<a href="$2">$1</a>`)

	boldDoubleAsterisk := regexp.MustCompile(`\*\*(.+?)\*\*`)
	for boldDoubleAsterisk.MatchString(text) {
		text = boldDoubleAsterisk.ReplaceAllString(text, "<b>$1</b>")
	}

	boldDoubleUnderscore := regexp.MustCompile("__(.+?)__")
	for boldDoubleUnderscore.MatchString(text) {
		text = boldDoubleUnderscore.ReplaceAllString(text, "<b>$1</b>")
	}

	italicSingleAsterisk := regexp.MustCompile(`\*([^*]+)\*`)
	for italicSingleAsterisk.MatchString(text) {
		text = italicSingleAsterisk.ReplaceAllString(text, "<i>$1</i>")
	}

	italicSingleUnderscore := regexp.MustCompile("_([^_]+)_")
	for italicSingleUnderscore.MatchString(text) {
		text = italicSingleUnderscore.ReplaceAllString(text, "<i>$1</i>")
	}

	headingRegex := regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	text = headingRegex.ReplaceAllString(text, "<b>$1</b>")

	blockquoteRegex := regexp.MustCompile(`(?m)^&gt;\s*(.+)$`)
	text = blockquoteRegex.ReplaceAllString(text, "<blockquote>$1</blockquote>")

	return text
}

func restoreCodeBlocks(text string, blocks []codeBlock) string {
	for i, block := range blocks {
		placeholder := placeholderPrefix + strconv.Itoa(i) + "\x00"
		escapedContent := escapeHTML(block.content)
		if block.isInline {
			text = strings.Replace(text, placeholder, "<code>"+escapedContent+"</code>", 1)
		} else {
			text = strings.Replace(text, placeholder, "<pre><code>"+escapedContent+"</code></pre>", 1)
		}
	}
	return text
}

func validateHTML(text string) string {
	openTags := []string{"b", "i", "u", "s", "code", "pre", "a", "blockquote"}
	for _, tag := range openTags {
		openCount := strings.Count(text, "<"+tag+">") + strings.Count(text, "<"+tag+" ")
		closeCount := strings.Count(text, "</"+tag+">")
		if openCount != closeCount {
			slog.Warn("unmatched HTML tag detected, stripping", "tag", tag, "open", openCount, "close", closeCount)
			openRegex := regexp.MustCompile("<" + tag + "[^>]*>")
			closeRegex := regexp.MustCompile("</" + tag + ">")
			text = openRegex.ReplaceAllString(text, "")
			text = closeRegex.ReplaceAllString(text, "")
		}
	}
	return text
}

func FormatHTML(text string) string {
	if text == "" {
		return ""
	}

	text, blocks := extractCodeBlocks(text)

	text = escapeHTML(text)

	text = convertMarkdownToHTML(text)

	text = restoreCodeBlocks(text, blocks)

	text = validateHTML(text)

	return text
}

func SendHTML(ctx context.Context, b *bot.Bot, chatID int64, text string, replyToID string) error {
	htmlText := FormatHTML(text)

	params := &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      htmlText,
		ParseMode: models.ParseModeHTML,
	}

	if replyToID != "" {
		id, err := strconv.Atoi(replyToID)
		if err == nil {
			params.ReplyParameters = &models.ReplyParameters{
				MessageID: id,
			}
		}
	}

	_, err := b.SendMessage(ctx, params)
	if err != nil {
		if errors.Is(err, bot.ErrorBadRequest) && strings.Contains(err.Error(), "can't parse entities") {
			slog.Warn("HTML parse failed, retrying as plain text", "chat_id", chatID, "html_len", len(htmlText))
			params.ParseMode = ""
			params.Text = text
			_, retryErr := b.SendMessage(ctx, params)
			if retryErr != nil {
				return fmt.Errorf("sending plain text message to %d: %w", chatID, retryErr)
			}
			return nil
		}
		return fmt.Errorf("sending HTML message to %d: %w", chatID, err)
	}

	return nil
}
