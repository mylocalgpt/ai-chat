# Agent Instructions for ai-chat

When responding via ai-chat (Telegram bot), follow these guidelines for optimal output formatting.

## Response Guidelines

- Keep responses concise. Summarize actions, don't show full diffs.
- Use markdown: headers, bold, code blocks, bullet lists.
- For code, use fenced blocks with language tags.
- Max ~2000 chars preferred. If longer, summarize and offer details.

## Formatting

The Telegram adapter applies one formatting boundary before sending:

- raw model text -> `FormatHTML` -> `SplitMessage` -> Telegram HTML send
- Do not pre-convert output to HTML before it reaches the adapter

Supported markdown-to-HTML conversions:

- `**bold**` or `__bold__` - bold text
- `*italic*` - italic text (single asterisk only; underscore italic is not supported to avoid false positives with snake_case)
- `# heading` - bold heading (Telegram has no heading tag)
- `[text](url)` - clickable link
- `> quote` - blockquote
- `` `code` `` - inline code
- fenced code blocks - preformatted code blocks
- `~~text~~` - strikethrough
- `- item` or `* item` - bullet lists (converted to Unicode bullets)
- `1. item` - numbered lists (indented)
- Markdown tables - rendered as monospace `<pre>` blocks

## Code Blocks

Use fenced code blocks with language tags:

````markdown
```go
func main() {
    fmt.Println("Hello")
}
```
````

Long code blocks are automatically split across messages while preserving formatting.

## Validation Notes

- Unmatched HTML tags are auto-repaired: missing close tags are appended, excess close tags are removed. Tags are never stripped wholesale.
- If Telegram rejects the HTML, the adapter retries as plain text with HTML tags stripped and entities unescaped.
- Keep normal replies concise, but multi-chunk formatting must still stay valid when responses are long.
- Security-sensitive content may require confirmation before it is sent to an agent.
- Flagged agent output may be replaced with a safety notice before user delivery.

## Placement

- **Claude Code**: Add to workspace `CLAUDE.md`
- **OpenCode**: Add to system prompt or `~/.config/opencode/AGENTS.md`
- **Copilot CLI**: Add to workspace instructions
