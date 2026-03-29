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
	if fence != nil && splitPoint == fence.start {
		fence = nil // split is at fence boundary, not inside
	}
	inFence := fence != nil

	chunkEnd := splitPoint

	if inFence {
		fenceCloseRunes := utf8.RuneCountInString(fenceCloseTag)

		// Scan for open HTML tags before the fence region (tags inside fences are skipped by scanTags).
		ts := scanTags(string(runes[:chunkEnd]), regions)
		closeTags := ts.closeAll()
		reopenTags := ts.reopenAll()
		closeTagRunes := utf8.RuneCountInString(closeTags)

		totalCloseRunes := fenceCloseRunes + closeTagRunes
		for chunkEnd+totalCloseRunes > maxLen {
			chunkEnd = maxLen - totalCloseRunes
			if chunkEnd < 1 {
				chunkEnd = 1
			}
			ts = scanTags(string(runes[:chunkEnd]), regions)
			closeTags = ts.closeAll()
			reopenTags = ts.reopenAll()
			newCloseRunes := utf8.RuneCountInString(closeTags)
			if newCloseRunes == closeTagRunes {
				break // stable
			}
			closeTagRunes = newCloseRunes
			totalCloseRunes = fenceCloseRunes + closeTagRunes
		}

		chunk := string(runes[:chunkEnd]) + fenceCloseTag + closeTags
		remainder := reopenTags + fence.openTag + string(runes[chunkEnd:])

		chunk = strings.TrimSpace(chunk)
		if chunk != "" {
			remainder = strings.TrimLeft(remainder, " \t\n")
		}

		var chunks []string
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		tagReopenRunes := utf8.RuneCountInString(reopenTags)
		newRegions := adjustRegions(regions, chunkEnd, fence, tagReopenRunes)
		subChunks := splitRecursive(remainder, maxLen, newRegions)
		chunks = append(chunks, subChunks...)

		return chunks
	}

	// Not in a fence: scan for open tags in the chunk.
	// Loop until chunkEnd is stable, because reducing chunkEnd may expose different
	// open tags (e.g. cutting before a </i> leaves it on the stack), requiring more room.
	ts := scanTags(string(runes[:chunkEnd]), regions)
	closeTags := ts.closeAll()
	reopenTags := ts.reopenAll()
	closeTagRunes := utf8.RuneCountInString(closeTags)

	for closeTagRunes > 0 && chunkEnd+closeTagRunes > maxLen {
		chunkEnd = maxLen - closeTagRunes
		if chunkEnd < 1 {
			chunkEnd = 1
		}
		ts = scanTags(string(runes[:chunkEnd]), regions)
		closeTags = ts.closeAll()
		reopenTags = ts.reopenAll()
		newCloseRunes := utf8.RuneCountInString(closeTags)
		if newCloseRunes == closeTagRunes {
			break // stable
		}
		closeTagRunes = newCloseRunes
	}

	// Trim first, then append/prepend tags so trimming does not strip them.
	chunk := strings.TrimSpace(string(runes[:chunkEnd]))
	remainder := strings.TrimLeft(string(runes[chunkEnd:]), " \t\n")

	if ts.len() > 0 {
		chunk += closeTags
		remainder = reopenTags + remainder
	}

	var chunks []string
	if chunk != "" {
		chunks = append(chunks, chunk)
	}

	tagReopenRunes := utf8.RuneCountInString(reopenTags)
	newRegions := adjustRegions(regions, chunkEnd, nil, tagReopenRunes)
	subChunks := splitRecursive(remainder, maxLen, newRegions)
	chunks = append(chunks, subChunks...)

	return chunks
}

// runeSliceMatch checks if pattern occurs at position pos in runes.
func runeSliceMatch(runes []rune, pos int, pattern []rune) bool {
	if pos < 0 || pos+len(pattern) > len(runes) {
		return false
	}
	for i, r := range pattern {
		if runes[pos+i] != r {
			return false
		}
	}
	return true
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

	// Priority 1: Before a heading (pattern: \n\n<b> in the search range).
	// Split at the position after the first \n, so the current chunk ends with \n
	// and the next chunk (after TrimLeft) starts with <b>.
	headingPattern := []rune("\n\n<b>")
	for i := searchEnd; i >= searchStart; i-- {
		if runeSliceMatch(runes, i, headingPattern) {
			return i + 1 // split after the first \n
		}
	}

	// Priority 2: After </blockquote>\n - split after the \n.
	// Start scan at searchEnd - len(pattern) so i + len(pattern) never exceeds searchEnd.
	bqPattern := []rune("</blockquote>\n")
	for i := searchEnd - len(bqPattern); i >= searchStart; i-- {
		if runeSliceMatch(runes, i, bqPattern) {
			return i + len(bqPattern)
		}
	}

	// Priority 3: After </code></pre>\n - split after the \n.
	// Start scan at searchEnd - len(pattern) so i + len(pattern) never exceeds searchEnd.
	codeEndPattern := []rune("</code></pre>\n")
	for i := searchEnd - len(codeEndPattern); i >= searchStart; i-- {
		if runeSliceMatch(runes, i, codeEndPattern) {
			return i + len(codeEndPattern)
		}
	}

	// Priority 4: Double newline (paragraph break).
	for i := searchEnd; i >= searchStart; i-- {
		if i+1 < len(runes) && runes[i] == '\n' && runes[i+1] == '\n' {
			return i + 2
		}
		if i >= 2 && runes[i-1] == '\n' && runes[i-2] == '\n' {
			return i
		}
	}

	// Priority 5: Single newline.
	for i := searchEnd; i >= searchStart; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}

	// Priority 6: Space.
	for i := searchEnd; i >= searchStart; i-- {
		if runes[i] == ' ' {
			return i + 1
		}
	}

	// Priority 7: Hard cut.
	return 0
}

// tagStack tracks open inline HTML tags for split boundary handling.
type tagStack struct {
	tags []string // tag names in open order, e.g. ["b", "i"]
}

func (ts *tagStack) push(tag string) {
	ts.tags = append(ts.tags, tag)
}

// pop removes the last occurrence of tag from the stack (handles misnesting gracefully).
func (ts *tagStack) pop(tag string) {
	for i := len(ts.tags) - 1; i >= 0; i-- {
		if ts.tags[i] == tag {
			ts.tags = append(ts.tags[:i], ts.tags[i+1:]...)
			return
		}
	}
}

// closeAll returns closing tags in reverse order, e.g. "</i></b>".
func (ts *tagStack) closeAll() string {
	if len(ts.tags) == 0 {
		return ""
	}
	var b strings.Builder
	for i := len(ts.tags) - 1; i >= 0; i-- {
		b.WriteString("</")
		b.WriteString(ts.tags[i])
		b.WriteByte('>')
	}
	return b.String()
}

// reopenAll returns opening tags in original order, e.g. "<b><i>".
func (ts *tagStack) reopenAll() string {
	if len(ts.tags) == 0 {
		return ""
	}
	var b strings.Builder
	for _, tag := range ts.tags {
		b.WriteByte('<')
		b.WriteString(tag)
		b.WriteByte('>')
	}
	return b.String()
}

func (ts *tagStack) clone() tagStack {
	if len(ts.tags) == 0 {
		return tagStack{}
	}
	cloned := make([]string, len(ts.tags))
	copy(cloned, ts.tags)
	return tagStack{tags: cloned}
}

func (ts *tagStack) len() int {
	return len(ts.tags)
}

// trackedTags are the inline HTML tags we track across split boundaries.
// We do NOT track pre, code, or pre><code - those are handled by the fence system.
var trackedTags = map[string]bool{
	"b":          true,
	"i":          true,
	"u":          true,
	"s":          true,
	"a":          true,
	"blockquote": true,
}

// scanTags scans HTML text for open/close tags, skipping positions inside fence regions.
// Returns a tagStack with the currently open tags at the end of text.
func scanTags(text string, regions []fenceRegion) tagStack {
	var ts tagStack
	runes := []rune(text)
	n := len(runes)

	i := 0
	for i < n {
		if runes[i] != '<' {
			i++
			continue
		}

		// Skip if inside a fence region.
		if isInCodeFence(i, regions) {
			i++
			continue
		}

		// Found '<', determine if it is a closing tag.
		if i+1 >= n {
			break
		}

		isClose := runes[i+1] == '/'

		// Find the closing '>'.
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

		// Extract tag name.
		var tagName string
		if isClose {
			// </tagname>
			tagName = string(runes[i+2 : end])
		} else {
			// <tagname> or <tagname attr="...">
			// Find end of tag name (space or >).
			nameEnd := end
			for j := i + 1; j < end; j++ {
				if runes[j] == ' ' {
					nameEnd = j
					break
				}
			}
			tagName = string(runes[i+1 : nameEnd])
		}

		tagName = strings.TrimSpace(strings.ToLower(tagName))

		if trackedTags[tagName] {
			if isClose {
				ts.pop(tagName)
			} else {
				ts.push(tagName)
			}
		}

		i = end + 1
	}

	return ts
}

func adjustRegions(regions []fenceRegion, splitPoint int, activeFence *fenceRegion, tagReopenRunes int) []fenceRegion {
	var adjusted []fenceRegion

	// Total prefix length in the remainder text.
	var prefixRunes int
	if activeFence != nil {
		prefixRunes = tagReopenRunes + utf8.RuneCountInString(activeFence.openTag)
	} else {
		prefixRunes = tagReopenRunes
	}

	for _, r := range regions {
		if r.end <= splitPoint {
			continue
		}

		// For the active fence (the one we split inside), its position in the
		// remainder is fixed: it starts right after the tag reopen prefix,
		// because the remainder is: reopenTags + fence.openTag + rawText[chunkEnd:].
		if activeFence != nil && r.start == activeFence.start && r.end == activeFence.end {
			newEnd := r.end - splitPoint + prefixRunes
			adjusted = append(adjusted, fenceRegion{
				start:   tagReopenRunes,
				end:     newEnd,
				openTag: r.openTag,
			})
			continue
		}

		// For other regions (after the split point), shift by the difference
		// between the prefix and the consumed text.
		newStart := r.start - splitPoint + prefixRunes
		newEnd := r.end - splitPoint + prefixRunes

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
