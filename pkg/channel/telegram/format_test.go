package telegram

import (
	"strings"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantBlocks       int
		wantPlaceholders int
	}{
		{
			name:             "no code blocks",
			input:            "plain text without code",
			wantBlocks:       0,
			wantPlaceholders: 0,
		},
		{
			name:             "inline code only",
			input:            "use the `print` function",
			wantBlocks:       1,
			wantPlaceholders: 1,
		},
		{
			name:             "fenced code block only",
			input:            "```go\nfmt.Println(\"hello\")\n```",
			wantBlocks:       1,
			wantPlaceholders: 1,
		},
		{
			name:             "both inline and fenced",
			input:            "use `fmt` here:\n```go\nfmt.Println(\"hello\")\n```",
			wantBlocks:       2,
			wantPlaceholders: 2,
		},
		{
			name:             "multiple fenced blocks",
			input:            "```go\ncode1\n```\n```python\ncode2\n```",
			wantBlocks:       2,
			wantPlaceholders: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, blocks := extractCodeBlocks(tt.input)
			if len(blocks) != tt.wantBlocks {
				t.Errorf("got %d blocks, want %d", len(blocks), tt.wantBlocks)
			}
			placeholderCount := strings.Count(result, placeholderPrefix)
			if placeholderCount != tt.wantPlaceholders {
				t.Errorf("got %d placeholders, want %d", placeholderCount, tt.wantPlaceholders)
			}
		})
	}
}

func TestExtractCodeBlocksContent(t *testing.T) {
	input := "```go\nfmt.Println(\"hello\")\n```"
	result, blocks := extractCodeBlocks(input)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].language != "go" {
		t.Errorf("expected language 'go', got %q", blocks[0].language)
	}
	if blocks[0].isInline {
		t.Error("fenced block should not be marked as inline")
	}
	if !strings.Contains(result, placeholderPrefix) {
		t.Error("result should contain placeholder")
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special chars",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "ampersand",
			input:    "a & b",
			expected: "a &amp; b",
		},
		{
			name:     "less than",
			input:    "a < b",
			expected: "a &lt; b",
		},
		{
			name:     "greater than",
			input:    "a > b",
			expected: "a &gt; b",
		},
		{
			name:     "quote",
			input:    "say \"hello\"",
			expected: "say &quot;hello&quot;",
		},
		{
			name:     "all special chars",
			input:    "<a href=\"test\">&</a>",
			expected: "&lt;a href=&quot;test&quot;&gt;&amp;&lt;/a&gt;",
		},
		{
			name:     "already escaped",
			input:    "&amp;",
			expected: "&amp;amp;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeHTML(tt.input)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestConvertMarkdownToHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "bold with asterisks",
			input:    "**bold text**",
			contains: "<b>bold text</b>",
		},
		{
			name:     "bold with underscores",
			input:    "__bold text__",
			contains: "<b>bold text</b>",
		},
		{
			name:     "italic with asterisk",
			input:    "*italic text*",
			contains: "<i>italic text</i>",
		},
		{
			name:     "italic with underscore",
			input:    "_italic text_",
			contains: "<i>italic text</i>",
		},
		{
			name:     "link",
			input:    "[click here](https://example.com)",
			contains: "<a href=\"https://example.com\">click here</a>",
		},
		{
			name:     "heading",
			input:    "# Heading 1",
			contains: "<b>Heading 1</b>",
		},
		{
			name:     "heading level 2",
			input:    "## Heading 2",
			contains: "<b>Heading 2</b>",
		},
		{
			name:     "blockquote",
			input:    "&gt; quoted text",
			contains: "<blockquote>quoted text</blockquote>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMarkdownToHTML(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("result %q does not contain %q", result, tt.contains)
			}
		})
	}
}

func TestRestoreCodeBlocks(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		blocks []codeBlock
		want   string
	}{
		{
			name:   "inline code",
			text:   "use " + placeholderPrefix + "0\x00 here",
			blocks: []codeBlock{{content: "print", isInline: true}},
			want:   "use <code>print</code> here",
		},
		{
			name:   "fenced code block",
			text:   placeholderPrefix + "0\x00",
			blocks: []codeBlock{{content: "fmt.Println()", language: "go", isInline: false}},
			want:   `<pre><code class="language-go">fmt.Println()</code></pre>`,
		},
		{
			name:   "code with HTML chars",
			text:   placeholderPrefix + "0\x00",
			blocks: []codeBlock{{content: "<script>", isInline: true}},
			want:   "<code>&lt;script&gt;</code>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := restoreCodeBlocks(tt.text, tt.blocks)
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestFormatHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "empty input",
			input:    "",
			contains: "",
		},
		{
			name:     "plain text",
			input:    "hello world",
			contains: "hello world",
		},
		{
			name:     "bold conversion",
			input:    "**bold**",
			contains: "<b>bold</b>",
		},
		{
			name:     "code block preserved",
			input:    "```go\nfmt.Println()\n```",
			contains: `<pre><code class="language-go">`,
		},
		{
			name:     "inline code preserved",
			input:    "use `code` here",
			contains: "<code>code</code>",
		},
		{
			name:     "markdown in code block not converted",
			input:    "```go\n**not bold**\n```",
			contains: "**not bold**",
		},
		{
			name:     "HTML entities escaped",
			input:    "a < b & c > d",
			contains: "a &lt; b &amp; c &gt; d",
		},
		{
			name:     "link conversion",
			input:    "[link](https://example.com)",
			contains: "<a href=\"https://example.com\">link</a>",
		},
		{
			name:     "heading conversion",
			input:    "# Title",
			contains: "<b>Title</b>",
		},
		{
			name:     "blockquote conversion",
			input:    "> quote",
			contains: "<blockquote>quote</blockquote>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatHTML(tt.input)
			if tt.input == "" && result != "" {
				t.Errorf("empty input should produce empty output, got %q", result)
			}
			if tt.input != "" && !strings.Contains(result, tt.contains) {
				t.Errorf("result %q does not contain %q", result, tt.contains)
			}
		})
	}
}

func TestFormatHTMLFullPipeline(t *testing.T) {
	input := "# Analysis Results\n\nThe function **processData** handles the following:\n\n- Input validation\n- Data transformation\n- Output formatting\n\n```go\nfunc processData(input string) error {\n    if input == \"\" {\n        return errors.New(\"empty input\")\n    }\n    return nil\n}\n```\n\nSee the [documentation](https://example.com) for more info.\n\nUse the `processData` function with care."

	result := FormatHTML(input)

	if !strings.Contains(result, "<b>Analysis Results</b>") {
		t.Error("heading not converted")
	}
	if !strings.Contains(result, "<b>processData</b>") {
		t.Error("bold not converted")
	}
	if !strings.Contains(result, `<pre><code class="language-go">`) {
		t.Error("code block not wrapped")
	}
	if !strings.Contains(result, "<a href=\"https://example.com\">documentation</a>") {
		t.Error("link not converted")
	}
	if !strings.Contains(result, "<code>processData</code>") {
		t.Error("inline code not wrapped")
	}
	if strings.Contains(result, "**") {
		t.Error("markdown bold markers should be removed")
	}
	if strings.Contains(result, "```") {
		t.Error("code fences should be removed")
	}
}

func TestFormatHTMLCodeBlockHTMLEscaping(t *testing.T) {
	input := "```html\n<div class=\"test\">&amp;</div>\n```"
	result := FormatHTML(input)

	if !strings.Contains(result, "&lt;div") {
		t.Error("HTML in code block should be escaped")
	}
	if !strings.Contains(result, "&quot;test&quot;") {
		t.Error("quotes in code block should be escaped")
	}
	if !strings.Contains(result, "&amp;amp;") {
		t.Error("ampersand in code block should be escaped")
	}
}

func TestFormatHTMLNestedFormatting(t *testing.T) {
	input := "**bold with *italic* inside**"
	result := FormatHTML(input)

	if !strings.Contains(result, "<b>bold with") {
		t.Error("outer bold should be converted")
	}
	if !strings.Contains(result, "<i>italic</i>") {
		t.Error("inner italic should be converted")
	}
}

func TestFormatHTMLMultipleCodeBlocks(t *testing.T) {
	input := "First block:\n```go\ncode1\n```\n\nSecond block:\n```python\ncode2\n```"
	result := FormatHTML(input)

	count := strings.Count(result, `<pre><code class="language-`)
	if count != 2 {
		t.Errorf("expected 2 code blocks, got %d", count)
	}
}

func TestFormatHTMLStrikethrough(t *testing.T) {
	input := "this is ~~deleted~~ text"
	result := FormatHTML(input)

	if !strings.Contains(result, "<s>deleted</s>") {
		t.Errorf("strikethrough not converted, got %q", result)
	}
}

func TestRestoreCodeBlocksLanguageAttribute(t *testing.T) {
	t.Run("with language", func(t *testing.T) {
		text := placeholderPrefix + "0\x00"
		blocks := []codeBlock{{content: "x = 1", language: "python", isInline: false}}
		result := restoreCodeBlocks(text, blocks)
		want := `<pre><code class="language-python">x = 1</code></pre>`
		if result != want {
			t.Errorf("got %q, want %q", result, want)
		}
	})

	t.Run("without language", func(t *testing.T) {
		text := placeholderPrefix + "0\x00"
		blocks := []codeBlock{{content: "plain code", language: "", isInline: false}}
		result := restoreCodeBlocks(text, blocks)
		want := "<pre><code>plain code</code></pre>"
		if result != want {
			t.Errorf("got %q, want %q", result, want)
		}
		if strings.Contains(result, "class=") {
			t.Error("code block without language should not have class attribute")
		}
	})
}

func TestRestoreCodeBlocksTruncation(t *testing.T) {
	longContent := strings.Repeat("a", MaxCodeBlockLen+100)
	text := placeholderPrefix + "0\x00"
	blocks := []codeBlock{{content: longContent, language: "", isInline: false}}
	result := restoreCodeBlocks(text, blocks)

	if !strings.Contains(result, "... [truncated]") {
		t.Error("long code block should contain truncation marker")
	}
	// The escaped content inside <pre><code>...</code></pre> should be at most
	// MaxCodeBlockLen + len("\n... [truncated]")
	if strings.Contains(result, strings.Repeat("a", MaxCodeBlockLen+1)) {
		t.Error("code block content should be truncated")
	}
}

func TestFormatHTMLThinkingMarkers(t *testing.T) {
	input := "Before %%THINKING_START%%hidden reasoning%%THINKING_END%% after"
	result := FormatHTML(input)

	if !strings.Contains(result, "<tg-spoiler>") {
		t.Errorf("thinking start marker not converted, got %q", result)
	}
	if !strings.Contains(result, "</tg-spoiler>") {
		t.Errorf("thinking end marker not converted, got %q", result)
	}
	if strings.Contains(result, "%%THINKING_START%%") {
		t.Error("raw thinking start marker should be removed")
	}
	if strings.Contains(result, "%%THINKING_END%%") {
		t.Error("raw thinking end marker should be removed")
	}
}

func TestWrapThinkingContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no markers",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "both markers",
			input: "%%THINKING_START%%thought%%THINKING_END%%",
			want:  "<tg-spoiler>thought</tg-spoiler>",
		},
		{
			name:  "multiple pairs",
			input: "%%THINKING_START%%a%%THINKING_END%% then %%THINKING_START%%b%%THINKING_END%%",
			want:  "<tg-spoiler>a</tg-spoiler> then <tg-spoiler>b</tg-spoiler>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapThinkingContent(tt.input)
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}
