package telegram

import (
	"strings"
	"unicode/utf8"
)

const FormattedMaxLen = 4000

const fenceCloseTag = "</code></pre>"

type fenceRegion struct {
	start   int
	end     int
	openTag string // e.g. `<pre><code class="language-go">`
}

// runeIndex returns the rune position of substr in text, starting search at rune position start.
// Returns -1 if not found.
func runeIndex(text, substr string, start int) int {
	runes := []rune(text)
	if start >= len(runes) {
		return -1
	}
	remaining := string(runes[start:])
	byteIdx := strings.Index(remaining, substr)
	if byteIdx == -1 {
		return -1
	}
	return start + utf8.RuneCountInString(remaining[:byteIdx])
}

func findCodeFences(text string) []fenceRegion {
	var regions []fenceRegion
	preOpenPrefix := "<pre><code"
	preClose := "</code></pre>"

	pos := 0
	runeLen := utf8.RuneCountInString(text)
	for pos < runeLen {
		// Find the opening tag prefix (may have class attribute).
		openIdx := runeIndex(text, preOpenPrefix, pos)
		if openIdx == -1 {
			break
		}

		// Find the closing ">" of the opening tag.
		closeBracket := runeIndex(text, ">", openIdx+utf8.RuneCountInString(preOpenPrefix))
		if closeBracket == -1 {
			break
		}

		// Capture the full opening tag, e.g. `<pre><code>` or `<pre><code class="language-go">`.
		runes := []rune(text)
		fullOpenTag := string(runes[openIdx : closeBracket+1])

		// Find the closing </code></pre> tag.
		closeIdx := runeIndex(text, preClose, closeBracket+1)
		if closeIdx == -1 {
			break
		}

		endPos := closeIdx + utf8.RuneCountInString(preClose)
		regions = append(regions, fenceRegion{
			start:   openIdx,
			end:     endPos,
			openTag: fullOpenTag,
		})
		pos = endPos
	}

	return regions
}

func isInCodeFence(pos int, regions []fenceRegion) bool {
	for _, r := range regions {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}

// fenceAt returns the fence region containing pos, or nil if pos is outside all fences.
func fenceAt(pos int, regions []fenceRegion) *fenceRegion {
	for i := range regions {
		if pos >= regions[i].start && pos < regions[i].end {
			return &regions[i]
		}
	}
	return nil
}

func SplitMessage(text string, maxLen int) []string {
	if text == "" {
		return nil
	}
	if maxLen <= 0 {
		maxLen = FormattedMaxLen
	}
	if utf8.RuneCountInString(text) <= maxLen {
		return []string{text}
	}

	regions := findCodeFences(text)

	return splitRecursive(text, maxLen, regions)
}

func splitRecursive(text string, maxLen int, regions []fenceRegion) []string {
	if utf8.RuneCountInString(text) <= maxLen {
		return []string{text}
	}

	runes := []rune(text)
	splitPoint := findBestSplitPoint(runes, maxLen, regions)

	if splitPoint <= 0 {
		splitPoint = maxLen
	}

	fence := fenceAt(splitPoint, regions)
	inFence := fence != nil

	chunkEnd := splitPoint

	if inFence {
		fenceCloseRunes := utf8.RuneCountInString(fenceCloseTag)

		if chunkEnd+fenceCloseRunes > maxLen {
			chunkEnd = maxLen - fenceCloseRunes
			if chunkEnd < 1 {
				chunkEnd = 1
			}
		}

		chunk := string(runes[:chunkEnd]) + fenceCloseTag
		remainder := fence.openTag + string(runes[chunkEnd:])

		chunk = strings.TrimSpace(chunk)
		if chunk != "" {
			remainder = strings.TrimLeft(remainder, " \t\n")
		}

		var chunks []string
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		newRegions := adjustRegions(regions, chunkEnd, fence)
		subChunks := splitRecursive(remainder, maxLen, newRegions)
		chunks = append(chunks, subChunks...)

		return chunks
	}

	chunk := strings.TrimSpace(string(runes[:chunkEnd]))
	remainder := strings.TrimLeft(string(runes[chunkEnd:]), " \t\n")

	var chunks []string
	if chunk != "" {
		chunks = append(chunks, chunk)
	}

	newRegions := adjustRegions(regions, chunkEnd, nil)
	subChunks := splitRecursive(remainder, maxLen, newRegions)
	chunks = append(chunks, subChunks...)

	return chunks
}

func findBestSplitPoint(runes []rune, maxLen int, regions []fenceRegion) int {
	searchStart := maxLen / 2
	searchEnd := maxLen
	if searchEnd > len(runes) {
		searchEnd = len(runes)
	}

	inFence := isInCodeFence(searchEnd, regions)
	if inFence {
		searchEnd = searchEnd - utf8.RuneCountInString(fenceCloseTag)
		if searchEnd < searchStart {
			searchStart = 1
		}
	}

	for i := searchEnd; i >= searchStart; i-- {
		if i+1 < len(runes) && runes[i] == '\n' && runes[i+1] == '\n' {
			return i + 2
		}
		if i >= 2 && runes[i-1] == '\n' && runes[i-2] == '\n' {
			return i
		}
	}

	for i := searchEnd; i >= searchStart; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}

	for i := searchEnd; i >= searchStart; i-- {
		if runes[i] == ' ' {
			return i + 1
		}
	}

	return 0
}

func adjustRegions(regions []fenceRegion, splitPoint int, activeFence *fenceRegion) []fenceRegion {
	var adjusted []fenceRegion

	for _, r := range regions {
		if r.end <= splitPoint {
			continue
		}

		newStart := r.start - splitPoint
		newEnd := r.end - splitPoint

		if activeFence != nil {
			openTagRunes := utf8.RuneCountInString(activeFence.openTag)
			newStart += openTagRunes
			newEnd += openTagRunes
		}

		if newStart < 0 {
			newStart = 0
		}

		adjusted = append(adjusted, fenceRegion{
			start:   newStart,
			end:     newEnd,
			openTag: r.openTag,
		})
	}

	return adjusted
}
