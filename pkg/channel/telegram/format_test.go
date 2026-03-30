package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
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
			name:     "snake_case not italicized",
			input:    "my_variable_name",
			contains: "my_variable_name",
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
		{
			name:     "token footer italic via pipeline",
			input:    "Hello world\n\n*150 tokens | $0.0001*",
			contains: "<i>150 tokens | $0.0001</i>",
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

func TestConvertMarkdownToHTMLHeadingNewlines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "heading has leading newline",
			input:    "# Heading",
			contains: "\n<b>Heading</b>\n",
		},
		{
			name:     "h2 heading has newlines",
			input:    "## Sub Heading",
			contains: "\n<b>Sub Heading</b>\n",
		},
		{
			name:     "h3 heading has newlines",
			input:    "### Third Level",
			contains: "\n<b>Third Level</b>\n",
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

func TestConvertBlockquotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single line blockquote",
			input: "&gt; quoted text",
			want:  "<blockquote>quoted text</blockquote>",
		},
		{
			name:  "multi-line blockquote merged",
			input: "&gt; line one\n&gt; line two",
			want:  "<blockquote>line one\nline two</blockquote>",
		},
		{
			name:  "non-consecutive blockquotes produce separate tags",
			input: "&gt; first\nnormal line\n&gt; second",
			want:  "<blockquote>first</blockquote>\nnormal line\n<blockquote>second</blockquote>",
		},
		{
			name:  "no space after &gt; still treated as blockquote",
			input: "&gt;text without space",
			want:  "<blockquote>text without space</blockquote>",
		},
		{
			name:  "bare &gt; as empty quote line",
			input: "&gt; first\n&gt;\n&gt; after empty",
			want:  "<blockquote>first\n\nafter empty</blockquote>",
		},
		{
			name:  "no blockquotes",
			input: "just plain text",
			want:  "just plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertBlockquotes(tt.input)
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestConvertMarkdownToHTMLHeadingInsideBlockquote(t *testing.T) {
	// After HTML escaping, "> # Title" becomes "&gt; # Title".
	// The heading regex won't match because the line starts with "&gt;",
	// so it passes through to blockquote processing as "# Title" content.
	// Per the spec, this is acceptable behavior.
	input := "&gt; # Title"
	result := convertMarkdownToHTML(input)

	if !strings.Contains(result, "<blockquote>") {
		t.Errorf("expected blockquote tag, got %q", result)
	}
	if !strings.Contains(result, "# Title") {
		t.Errorf("heading inside blockquote should retain # prefix, got %q", result)
	}
}

func TestConvertMarkdownToHTMLHorizontalRule(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "three dashes", input: "---"},
		{name: "three asterisks", input: "***"},
		{name: "three underscores", input: "___"},
		{name: "long dashes", input: "----------"},
		{name: "dashes with trailing space", input: "---  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMarkdownToHTML(tt.input)
			trimmed := strings.TrimSpace(result)
			if trimmed != "" {
				t.Errorf("horizontal rule should become empty, got %q", result)
			}
		})
	}
}

func TestFormatHTMLHeadingBlockquotePipeline(t *testing.T) {
	// Full pipeline test: heading, then blockquote, then heading
	input := "# First Heading\n\n> This is quoted\n> Second quote line\n\n## Second Heading"
	result := FormatHTML(input)

	if !strings.Contains(result, "<b>First Heading</b>") {
		t.Errorf("first heading not converted, got %q", result)
	}
	if !strings.Contains(result, "<blockquote>This is quoted\nSecond quote line</blockquote>") {
		t.Errorf("multi-line blockquote not merged correctly, got %q", result)
	}
	if !strings.Contains(result, "<b>Second Heading</b>") {
		t.Errorf("second heading not converted, got %q", result)
	}
}

func TestFormatHTMLHorizontalRulePipeline(t *testing.T) {
	input := "Above\n\n---\n\nBelow"
	result := FormatHTML(input)

	if strings.Contains(result, "---") {
		t.Errorf("horizontal rule should be removed, got %q", result)
	}
	if !strings.Contains(result, "Above") {
		t.Errorf("text above hr should be preserved, got %q", result)
	}
	if !strings.Contains(result, "Below") {
		t.Errorf("text below hr should be preserved, got %q", result)
	}
}

func TestIsTableSeparator(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"| --- | --- |", true},
		{"|---|---|", true},
		{"| :--- | ---: |", true},
		{"| :---: | :---: |", true},
		{" | --- | --- | ", true},
		{"--- | ---", true},
		{"just text", false},
		{"|", false},
		{"| text | more |", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isTableSeparator(tt.input)
			if got != tt.want {
				t.Errorf("isTableSeparator(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsTableRow(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"| col1 | col2 |", true},
		{"col1 | col2", true},
		{"no pipe here", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isTableRow(tt.input)
			if got != tt.want {
				t.Errorf("isTableRow(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCells(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "with leading and trailing pipes",
			input: "| col1 | col2 | col3 |",
			want:  []string{"col1", "col2", "col3"},
		},
		{
			name:  "without leading and trailing pipes",
			input: "col1 | col2 | col3",
			want:  []string{"col1", "col2", "col3"},
		},
		{
			name:  "extra whitespace",
			input: "|  spaced  |  out  |",
			want:  []string{"spaced", "out"},
		},
		{
			name:  "empty cell in middle",
			input: "| a |  | c |",
			want:  []string{"a", "", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCells(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d cells %v, want %d cells %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cell %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestConvertTablesSimple(t *testing.T) {
	input := "| Name | Age |\n| --- | --- |\n| Alice | 30 |\n| Bob | 25 |"
	result := convertTables(input)

	if !strings.Contains(result, "<pre>") {
		t.Errorf("table should be wrapped in <pre>, got %q", result)
	}
	if !strings.Contains(result, "</pre>") {
		t.Errorf("table should have closing </pre>, got %q", result)
	}
	if !strings.Contains(result, "Name") {
		t.Errorf("header should be present, got %q", result)
	}
	if !strings.Contains(result, "Alice") {
		t.Errorf("data should be present, got %q", result)
	}
	// Separator row should be removed
	if strings.Contains(result, "---") {
		t.Errorf("separator row should be removed, got %q", result)
	}
}

func TestConvertTablesSpecialChars(t *testing.T) {
	// After HTML escaping, &amp; and &lt; are multi-char sequences
	input := "| Key | Value |\n| --- | --- |\n| &amp;foo | &lt;bar&gt; |"
	result := convertTables(input)

	if !strings.Contains(result, "<pre>") {
		t.Errorf("table should be wrapped in <pre>, got %q", result)
	}
	if !strings.Contains(result, "&amp;foo") {
		t.Errorf("escaped content should be preserved, got %q", result)
	}
	if !strings.Contains(result, "&lt;bar&gt;") {
		t.Errorf("escaped content should be preserved, got %q", result)
	}
}

func TestConvertTablesVaryingWidths(t *testing.T) {
	input := "| A | LongColumnName |\n| --- | --- |\n| short | x |"
	result := convertTables(input)

	if !strings.Contains(result, "<pre>") {
		t.Errorf("should contain <pre>, got %q", result)
	}
	// Check that columns are aligned: header "A" should be padded to match "short"
	// and "LongColumnName" should remain as is since it's the widest
	lines := strings.Split(strings.TrimPrefix(strings.TrimSuffix(result, "</pre>"), "<pre>"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 data rows, got %d: %v", len(lines), lines)
	}
	// Both rows should have the same length due to padding
	if len(lines[0]) != len(lines[1]) {
		t.Errorf("rows should have same padded length: %q (%d) vs %q (%d)",
			lines[0], len(lines[0]), lines[1], len(lines[1]))
	}
}

func TestConvertTablesWithoutLeadingTrailingPipes(t *testing.T) {
	input := "Name | Age\n--- | ---\nAlice | 30"
	result := convertTables(input)

	if !strings.Contains(result, "<pre>") {
		t.Errorf("should contain <pre>, got %q", result)
	}
	if !strings.Contains(result, "Alice") {
		t.Errorf("should contain data, got %q", result)
	}
}

func TestConvertTablesEmbeddedInText(t *testing.T) {
	input := "Here is a table:\n| A | B |\n| --- | --- |\n| 1 | 2 |\nAnd some text after."
	result := convertTables(input)

	if !strings.Contains(result, "Here is a table:") {
		t.Errorf("text before table should be preserved, got %q", result)
	}
	if !strings.Contains(result, "<pre>") {
		t.Errorf("table should be converted, got %q", result)
	}
	if !strings.Contains(result, "And some text after.") {
		t.Errorf("text after table should be preserved, got %q", result)
	}
}

func TestConvertTablesNoFalsePositive(t *testing.T) {
	// A single line with | should NOT be treated as a table
	input := "Use the pipe | character for OR operations"
	result := convertTables(input)

	if strings.Contains(result, "<pre>") {
		t.Errorf("single pipe line should not become a table, got %q", result)
	}
	if result != input {
		t.Errorf("input should be unchanged, got %q", result)
	}
}

func TestFormatHTMLTablePipeline(t *testing.T) {
	input := "# Results\n\nHere are the scores:\n\n| Name | Score |\n| --- | --- |\n| Alice | 95 |\n| Bob | 87 |\n\nGreat work!"
	result := FormatHTML(input)

	if !strings.Contains(result, "<b>Results</b>") {
		t.Errorf("heading should be converted, got %q", result)
	}
	if !strings.Contains(result, "<pre>") {
		t.Errorf("table should be in <pre>, got %q", result)
	}
	if !strings.Contains(result, "Alice") {
		t.Errorf("table data should be present, got %q", result)
	}
	if !strings.Contains(result, "Great work!") {
		t.Errorf("trailing text should be preserved, got %q", result)
	}
	// Should NOT contain <pre><code> (tables use <pre> alone)
	if strings.Contains(result, "<pre><code>") {
		t.Errorf("tables should use <pre> not <pre><code>, got %q", result)
	}
	// Separator should be gone
	if strings.Contains(result, "| ---") {
		t.Errorf("separator row should be removed, got %q", result)
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{10000, "10,000"},
		{100000, "100,000"},
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatNumber(tt.input)
			if got != tt.want {
				t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatTokenFooter(t *testing.T) {
	tests := []struct {
		name   string
		input  int
		output int
		cost   float64
		want   string
	}{
		{
			name:   "typical usage",
			input:  1500,
			output: 500,
			cost:   0.0123,
			want:   "\n\n*2,000 tokens | $0.0123*",
		},
		{
			name:   "small numbers",
			input:  50,
			output: 30,
			cost:   0.0001,
			want:   "\n\n*80 tokens | $0.0001*",
		},
		{
			name:   "large numbers",
			input:  75000,
			output: 25000,
			cost:   1.2345,
			want:   "\n\n*100,000 tokens | $1.2345*",
		},
		{
			name:   "zero cost",
			input:  100,
			output: 50,
			cost:   0,
			want:   "\n\n*150 tokens | $0.0000*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTokenFooter(tt.input, tt.output, tt.cost)
			if got != tt.want {
				t.Errorf("formatTokenFooter(%d, %d, %f) = %q, want %q", tt.input, tt.output, tt.cost, got, tt.want)
			}
		})
	}
}

func TestFormatTokenFooterZero(t *testing.T) {
	got := formatTokenFooter(0, 0, 0)
	if got != "" {
		t.Errorf("formatTokenFooter(0, 0, 0) = %q, want empty string", got)
	}

	got = formatTokenFooter(0, 0, 0.05)
	if got != "" {
		t.Errorf("formatTokenFooter(0, 0, 0.05) = %q, want empty string even with non-zero cost", got)
	}
}

// mockSendHTMLBot captures SendMessage params for verifying SendHTML behavior.
type mockSendHTMLBot struct {
	lastParams *bot.SendMessageParams
}

func (b *mockSendHTMLBot) GetMe(context.Context) (*models.User, error) {
	return &models.User{ID: 1, IsBot: true}, nil
}
func (b *mockSendHTMLBot) Start(context.Context) {}
func (b *mockSendHTMLBot) SendMessage(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	b.lastParams = params
	return &models.Message{ID: 1, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}
func (b *mockSendHTMLBot) SendChatAction(context.Context, *bot.SendChatActionParams) (bool, error) {
	return true, nil
}
func (b *mockSendHTMLBot) SetMyCommands(context.Context, *bot.SetMyCommandsParams) (bool, error) {
	return true, nil
}
func (b *mockSendHTMLBot) DeleteMessage(context.Context, *bot.DeleteMessageParams) (bool, error) {
	return true, nil
}
func (b *mockSendHTMLBot) EditMessageText(_ context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	return &models.Message{ID: params.MessageID, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}
func (b *mockSendHTMLBot) SendDocument(_ context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	return &models.Message{ID: 1, Chat: models.Chat{ID: params.ChatID.(int64)}}, nil
}

func TestSendHTMLLinkPreview(t *testing.T) {
	mb := &mockSendHTMLBot{}
	err := SendHTML(context.Background(), mb, 123, "<b>Hello</b>", "")
	if err != nil {
		t.Fatalf("SendHTML() error = %v", err)
	}

	if mb.lastParams == nil {
		t.Fatal("no SendMessage call recorded")
	}

	if mb.lastParams.LinkPreviewOptions == nil {
		t.Fatal("LinkPreviewOptions should be set")
	}
	if mb.lastParams.LinkPreviewOptions.IsDisabled == nil || !*mb.lastParams.LinkPreviewOptions.IsDisabled {
		t.Error("LinkPreviewOptions.IsDisabled should be true")
	}
}

func TestSendHTMLLinkPreviewWithReply(t *testing.T) {
	mb := &mockSendHTMLBot{}
	err := SendHTML(context.Background(), mb, 456, "text with https://example.com link", "42")
	if err != nil {
		t.Fatalf("SendHTML() error = %v", err)
	}

	if mb.lastParams.LinkPreviewOptions == nil || !*mb.lastParams.LinkPreviewOptions.IsDisabled {
		t.Error("link preview should be disabled even with reply")
	}
	if mb.lastParams.ReplyParameters == nil || mb.lastParams.ReplyParameters.MessageID != 42 {
		t.Error("reply parameters should be set")
	}
}
