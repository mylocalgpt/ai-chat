package telegram

import (
	"strings"
	"unicode/utf8"
)

// TelegramMaxMessageLen is the maximum number of characters allowed in a
// single Telegram message.
const TelegramMaxMessageLen = 4096

// paragraphSep and sentence-level separators used for splitting.
var defaultSeparators = []string{"\n\n", ".\n", ". ", "? ", "! ", " "}

// ChunkMessage splits text into chunks that each fit within maxLen characters
// (measured in runes, not bytes). It splits at natural boundaries in priority
// order: paragraphs, sentences, words. Returns a single-element slice for
// text within maxLen. Returns nil for empty text.
func ChunkMessage(text string, maxLen int) []string {
	if text == "" {
		return nil
	}
	if maxLen <= 0 {
		maxLen = TelegramMaxMessageLen
	}
	if utf8.RuneCountInString(text) <= maxLen {
		return []string{text}
	}
	return chunkRecursive(text, maxLen, defaultSeparators)
}

// chunkRecursive tries each separator in order to split text into segments,
// then accumulates segments into chunks that fit within maxLen. Segments
// that still exceed maxLen are recursively split with the next separator.
func chunkRecursive(text string, maxLen int, separators []string) []string {
	if utf8.RuneCountInString(text) <= maxLen {
		return []string{text}
	}

	// If we have separators left to try, split on the first one.
	if len(separators) > 0 {
		sep := separators[0]
		rest := separators[1:]

		segments := splitKeepSep(text, sep)
		if len(segments) <= 1 {
			// This separator didn't produce multiple segments, try next.
			return chunkRecursive(text, maxLen, rest)
		}

		return accumulate(segments, maxLen, sep, rest)
	}

	// No separators left, hard split at rune boundaries.
	return hardSplit(text, maxLen)
}

// splitKeepSep splits text on sep and returns trimmed non-empty segments.
func splitKeepSep(text, sep string) []string {
	parts := strings.Split(text, sep)
	var segments []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return segments
}

// accumulate packs segments into chunks that fit within maxLen. If a segment
// itself exceeds maxLen, it is recursively split using remaining separators.
func accumulate(segments []string, maxLen int, sep string, remainingSeps []string) []string {
	var chunks []string
	var current strings.Builder

	joiner := sep

	for _, seg := range segments {
		segLen := utf8.RuneCountInString(seg)

		if segLen > maxLen {
			// Flush current buffer before handling oversized segment.
			if current.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(current.String()))
				current.Reset()
			}
			// Recursively split the oversized segment.
			sub := chunkRecursive(seg, maxLen, remainingSeps)
			chunks = append(chunks, sub...)
			continue
		}

		if current.Len() == 0 {
			current.WriteString(seg)
			continue
		}

		// Check if adding this segment would exceed the limit.
		combined := utf8.RuneCountInString(current.String()) + utf8.RuneCountInString(joiner) + segLen
		if combined <= maxLen {
			current.WriteString(joiner)
			current.WriteString(seg)
		} else {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
			current.WriteString(seg)
		}
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

// hardSplit breaks text at rune boundaries when no other separator works.
func hardSplit(text string, maxLen int) []string {
	runes := []rune(text)
	var chunks []string

	for len(runes) > 0 {
		if len(runes) <= maxLen {
			chunks = append(chunks, string(runes))
			break
		}

		// Try to find a space near the end to avoid splitting words.
		splitAt := maxLen
		for i := maxLen - 1; i > maxLen/2; i-- {
			if runes[i] == ' ' {
				splitAt = i
				break
			}
		}

		chunk := strings.TrimSpace(string(runes[:splitAt]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		runes = runes[splitAt:]
		// Trim leading spaces from remainder.
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}

	return chunks
}
