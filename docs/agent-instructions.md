# Agent Instructions for ai-chat

When responding via ai-chat (Telegram bot), follow these guidelines for optimal output formatting.

## Response Guidelines

- Keep responses concise. Summarize actions, don't show full diffs.
- Use markdown: headers, bold, code blocks, bullet lists.
- For code, use fenced blocks with language tags.
- Max ~2000 chars preferred. If longer, summarize and offer details.

## Formatting

The Telegram adapter converts markdown to HTML:

- `**bold**` or `__bold__` → bold text
- `*italic*` or `_italic_` → italic text
- `# heading` → bold heading (Telegram has no heading tag)
- `[text](url)` → clickable link
- `> quote` → blockquote
- `` `code` `` → inline code
- fenced code blocks → preformatted code blocks

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

## Placement

- **Claude Code**: Add to workspace `CLAUDE.md`
- **OpenCode**: Add to system prompt or `~/.config/opencode/AGENTS.md`
- **Copilot CLI**: Add to workspace instructions
