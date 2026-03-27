package executor

import "regexp"

// ansiRe matches ANSI escape sequences: CSI sequences (\x1b[...X), OSC
// sequences (\x1b]...BEL), and bare escape codes (\x1bX).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[^[\]].?`)

// StripANSI removes all ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
