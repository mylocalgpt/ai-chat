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
					if len([]rune(chunk)) > tt.maxLen {
						t.Errorf("chunk %d exceeds maxLen: %d runes > %d", i, len([]rune(chunk)), tt.maxLen)
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
		if len([]rune(chunk)) > 100 {
			t.Errorf("chunk exceeds maxLen: %d runes > 100", len([]rune(chunk)))
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
		if len([]rune(chunk)) > FormattedMaxLen {
			t.Errorf("chunk %d exceeds FormattedMaxLen: %d runes > %d", i, len([]rune(chunk)), FormattedMaxLen)
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

func TestFenceAt(t *testing.T) {
	regions := []fenceRegion{
		{start: 10, end: 50, openTag: `<pre><code class="language-go">`},
		{start: 60, end: 100, openTag: `<pre><code>`},
	}

	tests := []struct {
		name    string
		pos     int
		wantNil bool
		wantTag string
	}{
		{"before all regions", 5, true, ""},
		{"at first region start", 10, false, `<pre><code class="language-go">`},
		{"inside first region", 30, false, `<pre><code class="language-go">`},
		{"at first region end (exclusive)", 50, true, ""},
		{"between regions", 55, true, ""},
		{"at second region start", 60, false, `<pre><code>`},
		{"inside second region", 80, false, `<pre><code>`},
		{"at second region end (exclusive)", 100, true, ""},
		{"after all regions", 120, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fenceAt(tt.pos, regions)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got region with openTag %q", got.openTag)
				}
			} else {
				if got == nil {
					t.Fatal("expected non-nil region, got nil")
				}
				if got.openTag != tt.wantTag {
					t.Errorf("openTag: got %q, want %q", got.openTag, tt.wantTag)
				}
			}
		})
	}
}

func TestSplitMessageCodeFenceLanguagePreserved(t *testing.T) {
	openTag := `<pre><code class="language-go">`
	closeTag := `</code></pre>`
	codeContent := strings.Repeat("x", 200)
	input := openTag + codeContent + closeTag

	chunks := SplitMessage(input, 80)

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		// Every chunk except the first should start with the language-aware open tag.
		if i > 0 {
			if !strings.HasPrefix(chunk, openTag) {
				t.Errorf("chunk %d should start with %q, got prefix: %q", i, openTag, chunk[:min(len(chunk), 40)])
			}
		}
		// Every chunk except the last should end with the close tag.
		if i < len(chunks)-1 {
			if !strings.HasSuffix(chunk, closeTag) {
				t.Errorf("chunk %d should end with %q, got suffix: %q", i, closeTag, chunk[max(0, len(chunk)-20):])
			}
		}
	}
}

func TestSplitMessageMultipleLanguages(t *testing.T) {
	goOpen := `<pre><code class="language-go">`
	pyOpen := `<pre><code class="language-python">`
	closeTag := `</code></pre>`

	goCode := strings.Repeat("g", 150)
	pyCode := strings.Repeat("p", 150)

	input := goOpen + goCode + closeTag + "\n\nSome text between blocks.\n\n" + pyOpen + pyCode + closeTag

	chunks := SplitMessage(input, 100)

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// Track which language tags we see in reopened chunks.
	sawGoReopen := false
	sawPyReopen := false

	for i, chunk := range chunks {
		if i == 0 {
			continue // first chunk starts at beginning, skip
		}
		if strings.HasPrefix(chunk, goOpen) {
			sawGoReopen = true
			// Verify it doesn't accidentally use the python tag.
			if strings.HasPrefix(chunk, pyOpen) {
				t.Errorf("chunk %d starts with python tag but should be go", i)
			}
		}
		if strings.HasPrefix(chunk, pyOpen) {
			sawPyReopen = true
		}
	}

	if !sawGoReopen {
		t.Error("expected at least one chunk to reopen with Go language tag")
	}
	if !sawPyReopen {
		t.Error("expected at least one chunk to reopen with Python language tag")
	}
}

func TestTagStack(t *testing.T) {
	t.Run("push and len", func(t *testing.T) {
		var ts tagStack
		if ts.len() != 0 {
			t.Errorf("empty stack len: got %d, want 0", ts.len())
		}
		ts.push("b")
		ts.push("i")
		if ts.len() != 2 {
			t.Errorf("after 2 pushes: got %d, want 2", ts.len())
		}
	})

	t.Run("pop removes last matching", func(t *testing.T) {
		var ts tagStack
		ts.push("b")
		ts.push("i")
		ts.push("b")
		ts.pop("b")
		// Should remove the last "b", leaving ["b", "i"]
		if ts.len() != 2 {
			t.Fatalf("after pop: got len %d, want 2", ts.len())
		}
		if ts.tags[0] != "b" || ts.tags[1] != "i" {
			t.Errorf("after pop: got %v, want [b i]", ts.tags)
		}
	})

	t.Run("pop nonexistent is no-op", func(t *testing.T) {
		var ts tagStack
		ts.push("b")
		ts.pop("i")
		if ts.len() != 1 {
			t.Errorf("pop nonexistent: got len %d, want 1", ts.len())
		}
	})

	t.Run("closeAll reverse order", func(t *testing.T) {
		var ts tagStack
		ts.push("b")
		ts.push("i")
		got := ts.closeAll()
		want := "</i></b>"
		if got != want {
			t.Errorf("closeAll: got %q, want %q", got, want)
		}
	})

	t.Run("closeAll empty", func(t *testing.T) {
		var ts tagStack
		if got := ts.closeAll(); got != "" {
			t.Errorf("closeAll empty: got %q, want empty", got)
		}
	})

	t.Run("reopenAll original order", func(t *testing.T) {
		var ts tagStack
		ts.push("b")
		ts.push("i")
		got := ts.reopenAll()
		want := "<b><i>"
		if got != want {
			t.Errorf("reopenAll: got %q, want %q", got, want)
		}
	})

	t.Run("reopenAll empty", func(t *testing.T) {
		var ts tagStack
		if got := ts.reopenAll(); got != "" {
			t.Errorf("reopenAll empty: got %q, want empty", got)
		}
	})

	t.Run("clone is independent", func(t *testing.T) {
		var ts tagStack
		ts.push("b")
		ts.push("i")
		cloned := ts.clone()
		cloned.push("u")
		if ts.len() != 2 {
			t.Errorf("original modified after clone push: got %d, want 2", ts.len())
		}
		if cloned.len() != 3 {
			t.Errorf("clone should have 3: got %d", cloned.len())
		}
	})
}

func TestScanTags(t *testing.T) {
	t.Run("simple closed tag", func(t *testing.T) {
		ts := scanTags("<b>text</b>", nil)
		if ts.len() != 0 {
			t.Errorf("closed tag: got len %d, want 0", ts.len())
		}
	})

	t.Run("open only", func(t *testing.T) {
		ts := scanTags("<b>text", nil)
		if ts.len() != 1 {
			t.Fatalf("open only: got len %d, want 1", ts.len())
		}
		if ts.tags[0] != "b" {
			t.Errorf("open only: got %q, want 'b'", ts.tags[0])
		}
	})

	t.Run("nested", func(t *testing.T) {
		ts := scanTags("<b><i>text", nil)
		if ts.len() != 2 {
			t.Fatalf("nested: got len %d, want 2", ts.len())
		}
		if ts.tags[0] != "b" || ts.tags[1] != "i" {
			t.Errorf("nested: got %v, want [b i]", ts.tags)
		}
	})

	t.Run("close then open", func(t *testing.T) {
		ts := scanTags("<b>text</b><i>more", nil)
		if ts.len() != 1 {
			t.Fatalf("close then open: got len %d, want 1", ts.len())
		}
		if ts.tags[0] != "i" {
			t.Errorf("close then open: got %q, want 'i'", ts.tags[0])
		}
	})

	t.Run("with fence region skipped", func(t *testing.T) {
		// <b>text<pre><code>code</code></pre>more
		// The <code> and </code> inside the fence should not affect tag tracking.
		text := "<b>text<pre><code>code</code></pre>more"
		regions := findCodeFences(text)
		ts := scanTags(text, regions)
		if ts.len() != 1 {
			t.Fatalf("fence skip: got len %d, want 1", ts.len())
		}
		if ts.tags[0] != "b" {
			t.Errorf("fence skip: got %q, want 'b'", ts.tags[0])
		}
	})

	t.Run("anchor tag with href", func(t *testing.T) {
		ts := scanTags(`<a href="http://example.com">link`, nil)
		if ts.len() != 1 {
			t.Fatalf("anchor: got len %d, want 1", ts.len())
		}
		if ts.tags[0] != "a" {
			t.Errorf("anchor: got %q, want 'a'", ts.tags[0])
		}
	})

	t.Run("blockquote", func(t *testing.T) {
		ts := scanTags("<blockquote>quote text", nil)
		if ts.len() != 1 {
			t.Fatalf("blockquote: got len %d, want 1", ts.len())
		}
		if ts.tags[0] != "blockquote" {
			t.Errorf("blockquote: got %q, want 'blockquote'", ts.tags[0])
		}
	})

	t.Run("untracked tags ignored", func(t *testing.T) {
		ts := scanTags("<div>text<span>more</span></div>", nil)
		if ts.len() != 0 {
			t.Errorf("untracked: got len %d, want 0", ts.len())
		}
	})
}

func TestRuneSliceMatch(t *testing.T) {
	tests := []struct {
		name    string
		runes   []rune
		pos     int
		pattern []rune
		want    bool
	}{
		{"match at start", []rune("hello"), 0, []rune("hel"), true},
		{"match in middle", []rune("hello"), 2, []rune("llo"), true},
		{"no match", []rune("hello"), 0, []rune("xyz"), false},
		{"pos out of bounds", []rune("hi"), 5, []rune("h"), false},
		{"negative pos", []rune("hi"), -1, []rune("h"), false},
		{"pattern too long", []rune("hi"), 1, []rune("ijk"), false},
		{"empty pattern", []rune("hi"), 0, []rune(""), true},
		{"unicode match", []rune("日本語abc"), 3, []rune("abc"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runeSliceMatch(tt.runes, tt.pos, tt.pattern)
			if got != tt.want {
				t.Errorf("runeSliceMatch(%q, %d, %q) = %v, want %v",
					string(tt.runes), tt.pos, string(tt.pattern), got, tt.want)
			}
		})
	}
}

func TestFindBestSplitPointSemanticPriority(t *testing.T) {
	// Build input where both a heading boundary and a paragraph break
	// exist in the search range (back half of maxLen). The heading boundary
	// should win because it has higher priority.
	//
	// Layout (maxLen=100, search range 50..100):
	//   40 chars of filler
	//   \n\n (paragraph break at rune 40-41)  -- will be in range if search includes it
	//   more filler to push heading into range
	//   \n\n<b>Heading  (heading boundary)
	//   ... rest
	//
	// We want the heading boundary at ~rune 60, and a paragraph break at ~rune 55.
	// Both in the 50..100 search range.

	filler1 := strings.Repeat("a", 53)
	paragraphBreak := "\n\n"
	filler2 := strings.Repeat("b", 5)
	headingBoundary := "\n\n<b>Section Title</b>"
	filler3 := strings.Repeat("c", 50)

	input := filler1 + paragraphBreak + filler2 + headingBoundary + filler3
	runes := []rune(input)
	maxLen := 100

	splitPoint := findBestSplitPoint(runes, maxLen, nil)

	// The heading pattern \n\n<b> starts at position 53+2+5 = 60.
	// findBestSplitPoint should return 60+1 = 61 (after the first \n).
	headingPos := len([]rune(filler1 + paragraphBreak + filler2))
	expectedSplit := headingPos + 1 // after the first \n of \n\n<b>

	if splitPoint != expectedSplit {
		t.Errorf("expected split at heading boundary (pos %d), got %d", expectedSplit, splitPoint)
	}

	// Verify the paragraph break is also in range (would have been chosen without heading priority).
	paragraphPos := len([]rune(filler1))
	if paragraphPos < maxLen/2 || paragraphPos > maxLen {
		t.Errorf("test setup: paragraph break at %d should be in search range [%d, %d]",
			paragraphPos, maxLen/2, maxLen)
	}
}

func TestSplitMessageAtBlockquoteEnd(t *testing.T) {
	// Build a message with a long blockquote followed by more content.
	// The split should happen after </blockquote>\n when possible.
	quote := strings.Repeat("q", 60)
	afterQuote := strings.Repeat("x", 60)
	input := "<blockquote>" + quote + "</blockquote>\n" + afterQuote

	maxLen := 100
	chunks := SplitMessage(input, maxLen)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// First chunk should end with the blockquote (possibly with close tags).
	// The split should have happened after </blockquote>\n
	if !strings.Contains(chunks[0], "</blockquote>") {
		t.Errorf("first chunk should contain </blockquote>, got: %q", chunks[0])
	}

	// Second chunk should not start with content from inside the blockquote.
	// After TrimLeft, it should start with the afterQuote content (x's) or a reopened tag.
	if strings.Contains(chunks[1], "</blockquote>") {
		t.Errorf("second chunk should not contain </blockquote>, got: %q", chunks[1])
	}
}

func TestSplitMessageAtCodeBlockEnd(t *testing.T) {
	// Build a message where a code block ends near the split zone,
	// followed by more text. The split should happen after </code></pre>\n
	// rather than mid-paragraph.
	code := strings.Repeat("c", 50)
	afterCode := strings.Repeat("x", 60)
	input := "<pre><code>" + code + "</code></pre>\nSome paragraph. " + afterCode

	maxLen := 100
	chunks := SplitMessage(input, maxLen)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// First chunk should contain the complete code block.
	if !strings.Contains(chunks[0], "</code></pre>") {
		t.Errorf("first chunk should contain </code></pre>, got: %q", chunks[0])
	}

	// The second chunk should start with the content after the code block.
	if strings.HasPrefix(chunks[1], "<pre><code>") {
		t.Errorf("second chunk should NOT reopen a code fence (split should be outside fence), got: %q", chunks[1][:min(len(chunks[1]), 40)])
	}
}

func TestSplitMessageHTMLTagTracking(t *testing.T) {
	// <b> + 200 chars + </b>, maxLen 80
	content := strings.Repeat("x", 200)
	input := "<b>" + content + "</b>"

	chunks := SplitMessage(input, 80)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		runeLen := len([]rune(chunk))
		if runeLen > 80 {
			t.Errorf("chunk %d exceeds maxLen: %d runes > 80", i, runeLen)
		}
	}

	// First chunk should end with </b>
	if !strings.HasSuffix(chunks[0], "</b>") {
		t.Errorf("first chunk should end with </b>, got suffix: %q", chunks[0][max(0, len(chunks[0])-20):])
	}

	// Middle chunks and last chunk that need continuation should start with <b>
	for i := 1; i < len(chunks); i++ {
		if !strings.HasPrefix(chunks[i], "<b>") {
			t.Errorf("chunk %d should start with <b>, got prefix: %q", i, chunks[i][:min(len(chunks[i]), 20)])
		}
	}

	// Last chunk should end with original </b>
	lastChunk := chunks[len(chunks)-1]
	if !strings.HasSuffix(lastChunk, "</b>") {
		t.Errorf("last chunk should end with </b>, got suffix: %q", lastChunk[max(0, len(lastChunk)-20):])
	}
}

func TestSplitMessageNestedTags(t *testing.T) {
	// <b><i> + 200 chars + </i></b>, maxLen 80
	content := strings.Repeat("y", 200)
	input := "<b><i>" + content + "</i></b>"

	chunks := SplitMessage(input, 80)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		runeLen := len([]rune(chunk))
		if runeLen > 80 {
			t.Errorf("chunk %d exceeds maxLen: %d runes > 80", i, runeLen)
		}
	}

	// First chunk should close in reverse order: </i></b>
	if !strings.HasSuffix(chunks[0], "</i></b>") {
		t.Errorf("first chunk should end with </i></b>, got suffix: %q", chunks[0][max(0, len(chunks[0])-20):])
	}

	// Subsequent chunks should reopen in original order: <b><i>
	for i := 1; i < len(chunks); i++ {
		if !strings.HasPrefix(chunks[i], "<b><i>") {
			t.Errorf("chunk %d should start with <b><i>, got prefix: %q", i, chunks[i][:min(len(chunks[i]), 20)])
		}
	}
}

func TestSplitMessageTagsAndCodeFence(t *testing.T) {
	// <b>text<pre><code class="language-go">...code...</code></pre>more</b>
	codeContent := strings.Repeat("z", 150)
	input := `<b>text ` + `<pre><code class="language-go">` + codeContent + `</code></pre>` + ` more text here</b>`

	chunks := SplitMessage(input, 100)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		runeLen := len([]rune(chunk))
		if runeLen > 100 {
			t.Errorf("chunk %d exceeds maxLen: %d runes > 100", i, runeLen)
		}
	}

	// Verify that <b> is tracked: the last chunk (after code fence) should still have <b> context.
	// Look for a chunk that contains "more text" - it should be wrapped in <b>
	for _, chunk := range chunks {
		if strings.Contains(chunk, "more text") {
			if !strings.Contains(chunk, "<b>") {
				t.Errorf("chunk with 'more text' should have <b> reopened, got: %q", chunk)
			}
		}
	}
}

// hasBalancedHTMLTags checks that every tracked inline HTML tag has a matching close tag.
// Code fence tags (pre, code) are checked separately via open/close count.
func hasBalancedHTMLTags(s string) bool {
	var stack []string
	runes := []rune(s)
	n := len(runes)
	i := 0
	for i < n {
		if runes[i] != '<' {
			i++
			continue
		}
		if i+1 >= n {
			break
		}
		isClose := runes[i+1] == '/'
		end := -1
		for j := i + 1; j < n; j++ {
			if runes[j] == '>' {
				end = j
				break
			}
		}
		if end == -1 {
			break
		}
		var tagName string
		if isClose {
			tagName = strings.ToLower(strings.TrimSpace(string(runes[i+2 : end])))
		} else {
			nameEnd := end
			for j := i + 1; j < end; j++ {
				if runes[j] == ' ' {
					nameEnd = j
					break
				}
			}
			tagName = strings.ToLower(strings.TrimSpace(string(runes[i+1 : nameEnd])))
		}
		// Only check tracked inline tags; fence tags (pre, code) are handled by the splitter
		// at a higher level and individual chunks may legitimately have unmatched fence pairs.
		if trackedTags[tagName] {
			if isClose {
				// Pop from stack (handle misnesting gracefully).
				found := false
				for k := len(stack) - 1; k >= 0; k-- {
					if stack[k] == tagName {
						stack = append(stack[:k], stack[k+1:]...)
						found = true
						break
					}
				}
				if !found {
					return false
				}
			} else {
				stack = append(stack, tagName)
			}
		}
		i = end + 1
	}
	return len(stack) == 0
}

func TestSplitMessageEndToEnd(t *testing.T) {
	// Realistic long message with multiple features.
	input := `<b>Introduction</b>

This is a <b>bold</b> and <i>italic</i> paragraph with emoji 🎉 and CJK 日本語.

<b>Code Examples</b>

<pre><code class="language-go">func main() {
	fmt.Println("Hello, World!")
	for i := 0; i < 100; i++ {
		fmt.Printf("iteration %d: processing data for the long running task\n", i)
	}
	// This is a long comment that adds more content to the code block
	// to ensure it is large enough to require splitting across chunks
	result := computeSomething(42, "parameter")
	if result != nil {
		handleResult(result)
	}
}</code></pre>

Some text between code blocks with <b>formatting</b>.

<pre><code class="language-python">def process_data(items):
    """Process a list of items and return results."""
    results = []
    for item in items:
        transformed = transform(item)
        results.append(transformed)
    return results

class DataProcessor:
    def __init__(self, config):
        self.config = config
    
    def run(self):
        data = self.load()
        return self.process(data)
</code></pre>

<blockquote>This is a blockquote with some important information that the user should pay attention to.</blockquote>

<b>Conclusion</b>

Final paragraph with more emoji: 🚀🎨✨ and mixed content 中文テスト.`

	maxLen := 500
	chunks := SplitMessage(input, maxLen)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for long multi-feature message, got %d", len(chunks))
	}

	// (a) All chunks under maxLen in rune count.
	for i, chunk := range chunks {
		runeLen := len([]rune(chunk))
		if runeLen > maxLen {
			t.Errorf("chunk %d exceeds maxLen: %d runes > %d", i, runeLen, maxLen)
		}
	}

	// (b) No chunk has unbalanced HTML tags.
	for i, chunk := range chunks {
		if !hasBalancedHTMLTags(chunk) {
			t.Errorf("chunk %d has unbalanced HTML tags: %q", i, chunk)
		}
	}

	// (c) Code fences properly closed/reopened with language attribute.
	// When splitting inside a fence, the splitter injects close tags at chunk end
	// and reopens with the language-aware open tag at the next chunk start.
	// Verify each chunk has balanced fence tags (every open has a close within the chunk).
	for i, chunk := range chunks {
		openCount := strings.Count(chunk, "<pre><code")
		closeCount := strings.Count(chunk, "</code></pre>")
		if openCount != closeCount {
			t.Errorf("chunk %d has mismatched fence tags: %d opens, %d closes", i, openCount, closeCount)
		}
		// If a chunk reopens a Go fence, verify the language attribute is preserved.
		if strings.Contains(chunk, `<pre><code class="language-go">`) {
			// Good - language attribute preserved.
		} else if strings.Contains(chunk, `<pre><code class="language-python">`) {
			// Good - language attribute preserved.
		} else if openCount > 0 && !strings.Contains(chunk, `<pre><code>`) {
			// Has a fence open that is neither language-go, language-python, nor bare - unexpected.
			t.Errorf("chunk %d has unexpected fence open tag", i)
		}
	}

	// (d) Concatenating chunks and stripping all formatting tags recovers original text content.
	combined := strings.Join(chunks, "")
	// Strip HTML tags (only real tags starting with a letter or /) and compare raw text.
	stripAllTags := func(s string) string {
		var result strings.Builder
		runes := []rune(s)
		i := 0
		for i < len(runes) {
			if runes[i] == '<' && i+1 < len(runes) {
				next := runes[i+1]
				isTag := (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || next == '/'
				if isTag {
					// Skip to closing '>'.
					j := i + 1
					for j < len(runes) && runes[j] != '>' {
						j++
					}
					if j < len(runes) {
						i = j + 1
					} else {
						i = j
					}
					continue
				}
			}
			result.WriteRune(runes[i])
			i++
		}
		return strings.Join(strings.Fields(result.String()), " ")
	}

	originalNorm := stripAllTags(input)
	combinedNorm := stripAllTags(combined)
	if originalNorm != combinedNorm {
		maxShow := 200
		if len(originalNorm) < maxShow {
			maxShow = len(originalNorm)
		}
		t.Errorf("content not preserved after recombination.\nOriginal (normalized):  %q\nCombined (normalized): %q",
			originalNorm[:maxShow], combinedNorm[:min(maxShow, len(combinedNorm))])
	}
}

func TestSplitMessageAllCode(t *testing.T) {
	// Single huge code block, maxLen 500.
	openTag := `<pre><code class="language-go">`
	closeTag := `</code></pre>`
	codeContent := strings.Repeat("x", 2000)
	input := openTag + codeContent + closeTag

	maxLen := 500
	chunks := SplitMessage(input, maxLen)

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks for 2000-char code block at maxLen 500, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		runeLen := len([]rune(chunk))
		if runeLen > maxLen {
			t.Errorf("chunk %d exceeds maxLen: %d runes > %d", i, runeLen, maxLen)
		}

		// Every chunk should have properly paired fence tags.
		if !strings.Contains(chunk, "<pre><code") {
			t.Errorf("chunk %d missing opening fence tag", i)
		}
		if !strings.Contains(chunk, closeTag) {
			t.Errorf("chunk %d missing closing fence tag", i)
		}

		// Verify language attribute is preserved in reopened fences.
		if i > 0 {
			if !strings.HasPrefix(chunk, openTag) {
				t.Errorf("chunk %d should start with language-aware open tag %q, got prefix: %q",
					i, openTag, chunk[:min(len(chunk), 50)])
			}
		}
	}
}

func TestSplitMessageUnicodeWithFences(t *testing.T) {
	// Text with CJK and emoji both inside and outside code fences.
	cjkOutside := strings.Repeat("日本語", 50)  // 150 runes
	emojiOutside := strings.Repeat("🚀🎨", 30) // 60 runes
	cjkInside := strings.Repeat("中文代码", 40)  // 160 runes
	emojiInside := strings.Repeat("✨💻", 30)  // 60 runes

	input := cjkOutside + "\n\n" +
		`<pre><code class="language-go">` + cjkInside + emojiInside + `</code></pre>` +
		"\n\n" + emojiOutside

	maxLen := 200
	chunks := SplitMessage(input, maxLen)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		runeLen := len([]rune(chunk))
		if runeLen > maxLen {
			t.Errorf("chunk %d exceeds maxLen: %d runes > %d", i, runeLen, maxLen)
		}
	}

	// Verify fence tags are balanced across all chunks combined.
	totalOpen := 0
	totalClose := 0
	for _, chunk := range chunks {
		totalOpen += strings.Count(chunk, "<pre><code")
		totalClose += strings.Count(chunk, "</code></pre>")
	}
	if totalOpen != totalClose {
		t.Errorf("total fence tags unbalanced across chunks: %d opens, %d closes", totalOpen, totalClose)
	}

	// Verify content is preserved: all CJK/emoji runes appear somewhere.
	combined := strings.Join(chunks, "")
	for _, r := range cjkOutside {
		if !strings.ContainsRune(combined, r) {
			t.Errorf("CJK rune %q from outside text not found in output", string(r))
			break
		}
	}
	for _, r := range emojiOutside {
		if !strings.ContainsRune(combined, r) {
			t.Errorf("emoji rune %q from outside text not found in output", string(r))
			break
		}
	}
}

func TestSplitMessageEmptyAndEdgeCases(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		chunks := SplitMessage("", 100)
		if chunks != nil {
			t.Errorf("expected nil for empty string, got %v", chunks)
		}
	})

	t.Run("single character returns single chunk", func(t *testing.T) {
		chunks := SplitMessage("a", 100)
		if len(chunks) != 1 || chunks[0] != "a" {
			t.Errorf("expected [\"a\"], got %v", chunks)
		}
	})

	t.Run("exactly at maxLen returns single chunk", func(t *testing.T) {
		input := strings.Repeat("x", 100)
		chunks := SplitMessage(input, 100)
		if len(chunks) != 1 {
			t.Errorf("expected 1 chunk at exact maxLen, got %d", len(chunks))
		}
	})

	t.Run("one character over maxLen splits", func(t *testing.T) {
		input := strings.Repeat("x", 101)
		chunks := SplitMessage(input, 100)
		if len(chunks) < 2 {
			t.Errorf("expected at least 2 chunks for 101 chars at maxLen 100, got %d", len(chunks))
		}
	})

	t.Run("all whitespace text", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panicked on whitespace input: %v", r)
			}
		}()
		input := strings.Repeat(" \n\t", 50)
		chunks := SplitMessage(input, 100)
		// Should not panic; result may vary but must be safe.
		_ = chunks
	})

	t.Run("single unclosed tag", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panicked on unclosed tag: %v", r)
			}
		}()
		input := "<b>text without close" + strings.Repeat("x", 200)
		chunks := SplitMessage(input, 100)
		if len(chunks) < 2 {
			t.Errorf("expected at least 2 chunks, got %d", len(chunks))
		}
		// The unclosed <b> should be tracked and closed/reopened across chunks.
		for i, chunk := range chunks {
			runeLen := len([]rune(chunk))
			if runeLen > 100 {
				t.Errorf("chunk %d exceeds maxLen: %d runes > 100", i, runeLen)
			}
		}
	})

	t.Run("no panics on various edge cases", func(t *testing.T) {
		edgeCases := []string{
			"",
			"x",
			"<",
			">",
			"<>",
			"</>",
			"<b>",
			"</b>",
			"<pre><code>",
			"</code></pre>",
			"<pre><code>unclosed",
			strings.Repeat("🎉", 300),
			"<b>" + strings.Repeat("日本語", 100) + "</b>",
		}
		for _, input := range edgeCases {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("panicked on input %q: %v", input[:min(len(input), 30)], r)
					}
				}()
				_ = SplitMessage(input, 50)
			}()
		}
	})
}
