package telegram

import (
	"strings"
	"testing"
)

func TestFindCodeFences(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "no fences",
			input:     "plain text without code blocks",
			wantCount: 0,
		},
		{
			name:      "single code block",
			input:     "before <pre><code>code here</code></pre> after",
			wantCount: 1,
		},
		{
			name:      "multiple code blocks",
			input:     "<pre><code>first</code></pre> text <pre><code>second</code></pre>",
			wantCount: 2,
		},
		{
			name:      "nested tags ignored",
			input:     "<pre><code><b>bold</b></code></pre>",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regions := findCodeFences(tt.input)
			if len(regions) != tt.wantCount {
				t.Errorf("got %d regions, want %d", len(regions), tt.wantCount)
			}
		})
	}
}

func TestFindCodeFencesPositions(t *testing.T) {
	t.Run("ASCII positions", func(t *testing.T) {
		// Intentionally ASCII-only: byte offsets == rune offsets, so strings.Index
		// can be used to compute expected positions.
		input := "before <pre><code>code</code></pre> after"
		regions := findCodeFences(input)

		if len(regions) != 1 {
			t.Fatalf("expected 1 region, got %d", len(regions))
		}

		expectedStart := strings.Index(input, "<pre><code>")
		expectedEnd := strings.Index(input, "</code></pre>") + len("</code></pre>")

		if regions[0].start != expectedStart {
			t.Errorf("start: got %d, want %d", regions[0].start, expectedStart)
		}
		if regions[0].end != expectedEnd {
			t.Errorf("end: got %d, want %d", regions[0].end, expectedEnd)
		}
		if regions[0].openTag != "<pre><code>" {
			t.Errorf("openTag: got %q, want %q", regions[0].openTag, "<pre><code>")
		}
	})

	t.Run("multi-byte positions", func(t *testing.T) {
		// "🎉x" = 2 runes but 5 bytes. Verify positions are rune-based.
		input := "🎉x<pre><code>y</code></pre>"
		regions := findCodeFences(input)

		if len(regions) != 1 {
			t.Fatalf("expected 1 region, got %d", len(regions))
		}

		// "🎉x" = 2 runes, so start is 2.
		if regions[0].start != 2 {
			t.Errorf("start: got %d, want 2 (rune position)", regions[0].start)
		}

		// 2 + 11 (<pre><code>) + 1 (y) + 13 (</code></pre>) = 27
		wantEnd := 2 + len("<pre><code>") + 1 + len("</code></pre>")
		if regions[0].end != wantEnd {
			t.Errorf("end: got %d, want %d (rune position)", regions[0].end, wantEnd)
		}
	})
}

func TestIsInCodeFence(t *testing.T) {
	// Intentionally ASCII-only: byte offsets == rune offsets.
	input := "before <pre><code>code here</code></pre> after"
	regions := findCodeFences(input)

	codeStart := strings.Index(input, "<pre><code>")
	codeEnd := strings.Index(input, "</code></pre>")

	tests := []struct {
		name     string
		pos      int
		expected bool
	}{
		{"before fence", 0, false},
		{"at fence start", codeStart, true},
		{"inside code", codeStart + 10, true},
		{"at fence end", codeEnd, true},
		{"after fence", codeEnd + len("</code></pre>") + 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInCodeFence(tt.pos, regions)
			if result != tt.expected {
				t.Errorf("pos %d: got %v, want %v", tt.pos, result, tt.expected)
			}
		})
	}
}

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		maxLen      int
		wantChunks  int
		checkMaxLen bool
	}{
		{
			name:       "empty string",
			input:      "",
			maxLen:     100,
			wantChunks: 0,
		},
		{
			name:       "short message",
			input:      "short text",
			maxLen:     100,
			wantChunks: 1,
		},
		{
			name:       "exact limit",
			input:      strings.Repeat("a", 100),
			maxLen:     100,
			wantChunks: 1,
		},
		{
			name:        "needs split at paragraph",
			input:       strings.Repeat("a", 50) + "\n\n" + strings.Repeat("b", 60),
			maxLen:      100,
			wantChunks:  2,
			checkMaxLen: true,
		},
		{
			name:        "needs split at line",
			input:       strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 60),
			maxLen:      100,
			wantChunks:  2,
			checkMaxLen: true,
		},
		{
			name:        "needs split at word",
			input:       strings.Repeat("a", 50) + " " + strings.Repeat("b", 60),
			maxLen:      100,
			wantChunks:  2,
			checkMaxLen: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := SplitMessage(tt.input, tt.maxLen)
			if len(chunks) != tt.wantChunks {
				t.Errorf("got %d chunks, want %d", len(chunks), tt.wantChunks)
			}
			if tt.checkMaxLen {
				for i, chunk := range chunks {
					if len(chunk) > tt.maxLen {
						t.Errorf("chunk %d exceeds maxLen: %d > %d", i, len(chunk), tt.maxLen)
					}
				}
			}
		})
	}
}

func TestSplitMessageInsideCodeBlock(t *testing.T) {
	codeContent := strings.Repeat("a", 100)
	input := "<pre><code>" + codeContent + "</code></pre>"

	chunks := SplitMessage(input, 50)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if i == 0 {
			if !strings.HasSuffix(chunk, "</code></pre>") {
				t.Errorf("first chunk should end with closing fence, got: %q", chunk)
			}
		}
		if i == len(chunks)-1 {
			if !strings.HasPrefix(chunk, "<pre><code>") {
				t.Errorf("last chunk should start with opening fence, got: %q", chunk)
			}
		}
		if i > 0 && i < len(chunks)-1 {
			if !strings.HasPrefix(chunk, "<pre><code>") || !strings.HasSuffix(chunk, "</code></pre>") {
				t.Errorf("middle chunk should have both fences, got: %q", chunk)
			}
		}
	}
}

func TestSplitMessageMultipleCodeBlocks(t *testing.T) {
	code1 := strings.Repeat("a", 60)
	code2 := strings.Repeat("b", 60)
	input := "<pre><code>" + code1 + "</code></pre> text <pre><code>" + code2 + "</code></pre>"

	chunks := SplitMessage(input, 100)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	for _, chunk := range chunks {
		if len(chunk) > 100 {
			t.Errorf("chunk exceeds maxLen: %d > 100", len(chunk))
		}
	}
}

func TestSplitMessageNoCodeBlocks(t *testing.T) {
	input := strings.Repeat("paragraph one. ", 10) + "\n\n" + strings.Repeat("paragraph two. ", 10)

	chunks := SplitMessage(input, 100)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	for _, chunk := range chunks {
		if strings.Contains(chunk, "<pre><code>") {
			t.Errorf("chunk should not contain code fences: %q", chunk)
		}
	}
}

func TestSplitMessageUTF8(t *testing.T) {
	input := strings.Repeat("日本語", 50) + "\n\n" + strings.Repeat("中文", 50)

	chunks := SplitMessage(input, 100)

	for i, chunk := range chunks {
		runes := []rune(chunk)
		if len(runes) > 100 {
			t.Errorf("chunk %d exceeds maxLen runes: %d > 100", i, len(runes))
		}
	}
}

func TestSplitMessageVeryLongCodeBlock(t *testing.T) {
	codeContent := strings.Repeat("x", 200)
	input := "<pre><code>" + codeContent + "</code></pre>"

	chunks := SplitMessage(input, 50)

	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks for very long code block, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		runes := []rune(chunk)
		if len(runes) > 50 {
			t.Errorf("chunk %d exceeds maxLen: %d runes > 50", i, len(runes))
		}
	}
}

func TestSplitMessagePreservesCodeContent(t *testing.T) {
	codeContent := "line1\nline2\nline3"
	input := "<pre><code>" + codeContent + "</code></pre>"

	chunks := SplitMessage(input, 100)

	combined := strings.Join(chunks, "")
	combined = strings.ReplaceAll(combined, "</code></pre><pre><code>", "")

	if !strings.Contains(combined, codeContent) {
		t.Errorf("code content was lost or modified. Combined: %q", combined)
	}
}

func TestSplitMessageDefaultMaxLen(t *testing.T) {
	input := strings.Repeat("a", FormattedMaxLen+1)

	chunks := SplitMessage(input, 0)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if len(chunk) > FormattedMaxLen {
			t.Errorf("chunk %d exceeds FormattedMaxLen: %d > %d", i, len(chunk), FormattedMaxLen)
		}
	}
}

func TestRuneIndex(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		sub   string
		start int
		want  int
	}{
		{
			name:  "ASCII simple",
			text:  "hello world",
			sub:   "world",
			start: 0,
			want:  6,
		},
		{
			name:  "emoji before target",
			text:  "🎉🎉hello",
			sub:   "hello",
			start: 0,
			want:  2, // two emoji runes, then "hello" starts at rune 2
		},
		{
			name:  "not found",
			text:  "hello world",
			sub:   "xyz",
			start: 0,
			want:  -1,
		},
		{
			name:  "search from offset",
			text:  "abcabc",
			sub:   "abc",
			start: 1,
			want:  3,
		},
		{
			name:  "search from offset with multibyte",
			text:  "日本語abc日本語abc",
			sub:   "abc",
			start: 4,
			want:  9, // skip first "abc" at rune 3, runes[4:] = "bc日本語abc", "abc" at rune offset 5 from start 4 = 9
		},
		{
			name:  "start beyond length",
			text:  "short",
			sub:   "s",
			start: 100,
			want:  -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runeIndex(tt.text, tt.sub, tt.start)
			if got != tt.want {
				t.Errorf("runeIndex(%q, %q, %d) = %d, want %d", tt.text, tt.sub, tt.start, got, tt.want)
			}
		})
	}
}

func TestFindCodeFencesUnicode(t *testing.T) {
	t.Run("emoji before code fence", func(t *testing.T) {
		// Each emoji is 1 rune. "🎉🎉🎉" = 3 runes.
		input := "🎉🎉🎉<pre><code>code</code></pre>"
		regions := findCodeFences(input)

		if len(regions) != 1 {
			t.Fatalf("expected 1 region, got %d", len(regions))
		}

		// Opening tag starts at rune 3 (after 3 emoji).
		if regions[0].start != 3 {
			t.Errorf("start: got %d, want 3", regions[0].start)
		}

		// <pre><code> = 11 runes, "code" = 4 runes, </code></pre> = 13 runes
		// end = 3 + 11 + 4 + 13 = 31
		wantEnd := 3 + len("<pre><code>") + len("code") + len("</code></pre>")
		if regions[0].end != wantEnd {
			t.Errorf("end: got %d, want %d", regions[0].end, wantEnd)
		}

		if regions[0].openTag != "<pre><code>" {
			t.Errorf("openTag: got %q, want %q", regions[0].openTag, "<pre><code>")
		}
	})

	t.Run("CJK before and inside fence", func(t *testing.T) {
		// "日本" = 2 runes
		input := `日本<pre><code>中文</code></pre>`
		regions := findCodeFences(input)

		if len(regions) != 1 {
			t.Fatalf("expected 1 region, got %d", len(regions))
		}

		if regions[0].start != 2 {
			t.Errorf("start: got %d, want 2", regions[0].start)
		}

		// 2 + 11 (<pre><code>) + 2 (中文) + 13 (</code></pre>) = 28
		wantEnd := 2 + len("<pre><code>") + 2 + len("</code></pre>")
		if regions[0].end != wantEnd {
			t.Errorf("end: got %d, want %d", regions[0].end, wantEnd)
		}
	})

	t.Run("code fence with class attribute", func(t *testing.T) {
		input := `text <pre><code class="language-go">func main()</code></pre> end`
		regions := findCodeFences(input)

		if len(regions) != 1 {
			t.Fatalf("expected 1 region, got %d", len(regions))
		}

		wantOpenTag := `<pre><code class="language-go">`
		if regions[0].openTag != wantOpenTag {
			t.Errorf("openTag: got %q, want %q", regions[0].openTag, wantOpenTag)
		}

		// start at rune 5 ("text " = 5 runes)
		if regions[0].start != 5 {
			t.Errorf("start: got %d, want 5", regions[0].start)
		}
	})

	t.Run("emoji before fence with class attribute", func(t *testing.T) {
		input := `🎉<pre><code class="language-go">code</code></pre>`
		regions := findCodeFences(input)

		if len(regions) != 1 {
			t.Fatalf("expected 1 region, got %d", len(regions))
		}

		// Emoji is 1 rune, fence starts at rune 1.
		if regions[0].start != 1 {
			t.Errorf("start: got %d, want 1", regions[0].start)
		}

		wantOpenTag := `<pre><code class="language-go">`
		if regions[0].openTag != wantOpenTag {
			t.Errorf("openTag: got %q, want %q", regions[0].openTag, wantOpenTag)
		}
	})
}
