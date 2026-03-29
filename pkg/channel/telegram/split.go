package telegram

import (
	"strings"
	"unicode/utf8"
)

const FormattedMaxLen = 4000

const fenceCloseTag = "</code></pre>"
const fenceOpenTag = "<pre><code>"

type fenceRegion struct {
	start int
	end   int
}

func findCodeFences(text string) []fenceRegion {
	var regions []fenceRegion
	preOpen := "<pre><code>"
	preClose := "</code></pre>"

	pos := 0
	for pos < len(text) {
		openIdx := strings.Index(text[pos:], preOpen)
		if openIdx == -1 {
			break
		}
		openIdx += pos

		closeIdx := strings.Index(text[openIdx+len(preOpen):], preClose)
		if closeIdx == -1 {
			break
		}
		closeIdx += openIdx + len(preOpen)

		regions = append(regions, fenceRegion{
			start: openIdx,
			end:   closeIdx + len(preClose),
		})
		pos = closeIdx + len(preClose)
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

	inFence := isInCodeFence(splitPoint, regions)

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
		remainder := fenceOpenTag + string(runes[chunkEnd:])

		chunk = strings.TrimSpace(chunk)
		if chunk != "" {
			remainder = strings.TrimLeft(remainder, " \t\n")
		}

		var chunks []string
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		newRegions := adjustRegions(regions, chunkEnd, inFence)
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

	newRegions := adjustRegions(regions, chunkEnd, false)
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

func adjustRegions(regions []fenceRegion, splitPoint int, inFence bool) []fenceRegion {
	var adjusted []fenceRegion

	for _, r := range regions {
		if r.end <= splitPoint {
			continue
		}

		newStart := r.start - splitPoint
		newEnd := r.end - splitPoint

		if inFence {
			newStart += utf8.RuneCountInString(fenceOpenTag)
			newEnd += utf8.RuneCountInString(fenceOpenTag)
		}

		if newStart < 0 {
			newStart = 0
		}

		adjusted = append(adjusted, fenceRegion{
			start: newStart,
			end:   newEnd,
		})
	}

	return adjusted
}
