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

// MaxCodeBlockLen is the maximum length (in bytes) for a single code block's
// escaped content. Blocks exceeding this limit are truncated. Exported so that
// the message splitter (Part 3) can reference it.
const MaxCodeBlockLen = 15000

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

func convertBlockquotes(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	var quoteLines []string

	flushQuote := func() {
		if len(quoteLines) > 0 {
			result = append(result, "<blockquote>"+strings.Join(quoteLines, "\n")+"</blockquote>")
			quoteLines = nil
		}
	}

	quotePrefix := "&gt; "    // with space
	quotePrefixBare := "&gt;" // bare (line is just ">")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, quotePrefix) {
			quoteLines = append(quoteLines, strings.TrimPrefix(trimmed, quotePrefix))
		} else if trimmed == quotePrefixBare {
			// Empty quote line (just ">"), keep as empty line in blockquote
			quoteLines = append(quoteLines, "")
		} else if strings.HasPrefix(trimmed, quotePrefixBare) {
			// No space after &gt; (e.g., "&gt;text") - still a blockquote line
			quoteLines = append(quoteLines, strings.TrimPrefix(trimmed, quotePrefixBare))
		} else {
			flushQuote()
			result = append(result, line)
		}
	}
	flushQuote()
	return strings.Join(result, "\n")
}

var tableSepRegex = regexp.MustCompile(`^\|?[\s\-:]+(\|[\s\-:]+)+\|?\s*$`)

func isTableSeparator(line string) bool {
	return tableSepRegex.MatchString(strings.TrimSpace(line))
}

func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Contains(trimmed, "|")
}

func parseCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	// Split on pipe
	parts := strings.Split(trimmed, "|")
	// Remove leading empty element if line starts with |
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	// Remove trailing empty element if line ends with |
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	// Trim whitespace from each cell
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

func formatTable(lines []string) string {
	// Parse all rows, skipping the separator (index 1)
	var rows [][]string
	for i, line := range lines {
		if i == 1 {
			continue // skip separator row
		}
		rows = append(rows, parseCells(line))
	}
	if len(rows) == 0 {
		return strings.Join(lines, "\n")
	}

	// Determine the maximum number of columns
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	// Calculate max width per column
	colWidths := make([]int, maxCols)
	for _, row := range rows {
		for col := 0; col < maxCols; col++ {
			cellLen := 0
			if col < len(row) {
				cellLen = len(row[col])
			}
			if cellLen > colWidths[col] {
				colWidths[col] = cellLen
			}
		}
	}

	// Build padded rows
	var sb strings.Builder
	sb.WriteString("<pre>")
	for ri, row := range rows {
		if ri > 0 {
			sb.WriteString("\n")
		}
		for col := 0; col < maxCols; col++ {
			if col > 0 {
				sb.WriteString("  ")
			}
			cell := ""
			if col < len(row) {
				cell = row[col]
			}
			sb.WriteString(cell)
			// Pad with spaces to column width
			for pad := len(cell); pad < colWidths[col]; pad++ {
				sb.WriteString(" ")
			}
		}
	}
	sb.WriteString("</pre>")
	return sb.String()
}

func convertTables(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	i := 0

	for i < len(lines) {
		// Detect table start: a line with | and next line is separator
		if isTableRow(lines[i]) && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
			// Collect all consecutive table lines
			tableStart := i
			tableEnd := i
			for tableEnd < len(lines) && isTableRow(lines[tableEnd]) {
				tableEnd++
			}
			result = append(result, formatTable(lines[tableStart:tableEnd]))
			i = tableEnd
		} else {
			result = append(result, lines[i])
			i++
		}
	}
	return strings.Join(result, "\n")
}

func convertMarkdownToHTML(text string) string {
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkRegex.ReplaceAllString(text, `<a href="$2">$1</a>`)

	text = convertTables(text)

	strikethroughRegex := regexp.MustCompile(`~~(.+?)~~`)
	text = strikethroughRegex.ReplaceAllString(text, "<s>$1</s>")

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

	headingRegex := regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	text = headingRegex.ReplaceAllString(text, "\n<b>$1</b>\n")

	text = convertBlockquotes(text)

	hrRegex := regexp.MustCompile(`(?m)^[\-\*_]{3,}\s*$`)
	text = hrRegex.ReplaceAllString(text, "")

	return text
}

func restoreCodeBlocks(text string, blocks []codeBlock) string {
	for i, block := range blocks {
		placeholder := placeholderPrefix + strconv.Itoa(i) + "\x00"
		escapedContent := escapeHTML(block.content)
		if block.isInline {
			text = strings.Replace(text, placeholder, "<code>"+escapedContent+"</code>", 1)
		} else {
			if len(escapedContent) > MaxCodeBlockLen {
				escapedContent = escapedContent[:MaxCodeBlockLen] + "\n... [truncated]"
			}
			if block.language != "" {
				text = strings.Replace(text, placeholder, `<pre><code class="language-`+block.language+`">`+escapedContent+"</code></pre>", 1)
			} else {
				text = strings.Replace(text, placeholder, "<pre><code>"+escapedContent+"</code></pre>", 1)
			}
		}
	}
	return text
}

func validateHTML(text string) string {
	openTags := []string{"b", "i", "u", "s", "code", "pre", "a", "blockquote", "tg-spoiler"}
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

// wrapThinkingContent replaces thinking markers with tg-spoiler tags.
// Callers (Part 4/5 streaming adapter) inject %%THINKING_START%% / %%THINKING_END%%
// markers around reasoning content before calling FormatHTML().
func wrapThinkingContent(text string) string {
	text = strings.ReplaceAll(text, "%%THINKING_START%%", "<tg-spoiler>")
	text = strings.ReplaceAll(text, "%%THINKING_END%%", "</tg-spoiler>")
	return text
}

func FormatHTML(text string) string {
	if text == "" {
		return ""
	}

	text, blocks := extractCodeBlocks(text)

	text = escapeHTML(text)

	text = wrapThinkingContent(text)

	text = convertMarkdownToHTML(text)

	text = restoreCodeBlocks(text, blocks)

	text = validateHTML(text)

	return text
}

func SendHTML(ctx context.Context, b telegramBot, chatID int64, text string, replyToID string) error {
	params := &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
		LinkPreviewOptions: &models.LinkPreviewOptions{
			IsDisabled: bot.True(),
		},
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
			slog.Warn("HTML parse failed, retrying as plain text", "chat_id", chatID, "html_len", len(text))
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

// formatNumber formats an integer with comma separators for thousands.
func formatNumber(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	s := strconv.Itoa(n)
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// formatTokenFooter returns a markdown italic footer with total token count and cost.
// The asterisk syntax is converted to <i>...</i> by FormatHTML's convertMarkdownToHTML.
// Returns empty string if both input and output tokens are zero.
func formatTokenFooter(inputTokens, outputTokens int, cost float64) string {
	if inputTokens == 0 && outputTokens == 0 {
		return ""
	}
	total := inputTokens + outputTokens
	return fmt.Sprintf("\n\n*%s tokens | $%.4f*", formatNumber(total), cost)
}
