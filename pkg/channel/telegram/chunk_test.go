package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkMessage(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		wantNil  bool
		wantLen  int // expected number of chunks (0 means check wantNil)
		validate func(t *testing.T, chunks []string)
	}{
		{
			name:    "empty string returns nil",
			text:    "",
			maxLen:  100,
			wantNil: true,
		},
		{
			name:    "short message returns single chunk",
			text:    "hello",
			maxLen:  100,
			wantLen: 1,
			validate: func(t *testing.T, chunks []string) {
				if chunks[0] != "hello" {
					t.Errorf("got %q, want %q", chunks[0], "hello")
				}
			},
		},
		{
			name:    "exactly at limit returns single chunk",
			text:    strings.Repeat("a", 100),
			maxLen:  100,
			wantLen: 1,
		},
		{
			name:    "paragraph boundary split",
			text:    strings.Repeat("a", 50) + "\n\n" + strings.Repeat("b", 55),
			maxLen:  100,
			wantLen: 2,
			validate: func(t *testing.T, chunks []string) {
				if chunks[0] != strings.Repeat("a", 50) {
					t.Errorf("chunk 0: got %q", chunks[0])
				}
				if chunks[1] != strings.Repeat("b", 55) {
					t.Errorf("chunk 1: got %q", chunks[1])
				}
			},
		},
		{
			name:    "sentence boundary split when paragraph too long",
			text:    strings.Repeat("a", 60) + ". " + strings.Repeat("b", 50),
			maxLen:  100,
			wantLen: 2,
		},
		{
			name:    "word boundary split when sentence too long",
			text:    strings.Repeat("word ", 25), // 125 chars
			maxLen:  100,
			wantLen: 2,
			validate: func(t *testing.T, chunks []string) {
				for i, c := range chunks {
					if utf8.RuneCountInString(c) > 100 {
						t.Errorf("chunk %d exceeds maxLen: %d runes", i, utf8.RuneCountInString(c))
					}
				}
			},
		},
		{
			name:    "hard split with no spaces",
			text:    strings.Repeat("x", 250),
			maxLen:  100,
			wantLen: 3,
			validate: func(t *testing.T, chunks []string) {
				total := 0
				for i, c := range chunks {
					cLen := utf8.RuneCountInString(c)
					if cLen > 100 {
						t.Errorf("chunk %d exceeds maxLen: %d runes", i, cLen)
					}
					total += cLen
				}
				if total != 250 {
					t.Errorf("total runes = %d, want 250", total)
				}
			},
		},
		{
			name:    "unicode multi-byte characters",
			text:    strings.Repeat("\U0001f600", 120), // 120 emoji, each 4 bytes
			maxLen:  100,
			wantLen: 2,
			validate: func(t *testing.T, chunks []string) {
				for i, c := range chunks {
					if utf8.RuneCountInString(c) > 100 {
						t.Errorf("chunk %d exceeds maxLen: %d runes", i, utf8.RuneCountInString(c))
					}
				}
			},
		},
		{
			name:    "multiple chunks for large text",
			text:    strings.Repeat("word ", 1000), // ~5000 chars
			maxLen:  100,
			wantLen: 0, // just check constraints
			validate: func(t *testing.T, chunks []string) {
				if len(chunks) < 4 {
					t.Errorf("expected 4+ chunks, got %d", len(chunks))
				}
				for i, c := range chunks {
					if utf8.RuneCountInString(c) > 100 {
						t.Errorf("chunk %d exceeds maxLen: %d runes", i, utf8.RuneCountInString(c))
					}
				}
			},
		},
		{
			name:    "no leading or trailing whitespace in chunks",
			text:    "  hello world  \n\n  foo bar  \n\n  baz qux  ",
			maxLen:  15,
			wantLen: 3,
			validate: func(t *testing.T, chunks []string) {
				for i, c := range chunks {
					if c != strings.TrimSpace(c) {
						t.Errorf("chunk %d has extra whitespace: %q", i, c)
					}
				}
			},
		},
		{
			name:    "real telegram limit single chunk",
			text:    strings.Repeat("a", 4096),
			maxLen:  4096,
			wantLen: 1,
		},
		{
			name:    "real telegram limit needs split",
			text:    strings.Repeat("a", 4096) + "\n\n" + strings.Repeat("b", 100),
			maxLen:  4096,
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ChunkMessage(tt.text, tt.maxLen)

			if tt.wantNil {
				if chunks != nil {
					t.Fatalf("expected nil, got %d chunks", len(chunks))
				}
				return
			}

			if chunks == nil {
				t.Fatal("unexpected nil result")
			}

			if tt.wantLen > 0 && len(chunks) != tt.wantLen {
				t.Fatalf("got %d chunks, want %d", len(chunks), tt.wantLen)
			}

			// Universal constraint: no chunk exceeds maxLen.
			for i, c := range chunks {
				if utf8.RuneCountInString(c) > tt.maxLen {
					t.Errorf("chunk %d exceeds maxLen: %d runes > %d", i, utf8.RuneCountInString(c), tt.maxLen)
				}
			}

			if tt.validate != nil {
				tt.validate(t, chunks)
			}
		})
	}
}
