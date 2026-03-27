package executor

import (
	"regexp"
	"strings"
)

const sessionPrefix = "ai-chat-"

// sanitizeRe matches characters that are problematic in tmux session names.
var sanitizeRe = regexp.MustCompile(`[^a-z0-9-]`)

// multiHyphenRe collapses multiple consecutive hyphens.
var multiHyphenRe = regexp.MustCompile(`-{2,}`)

// sanitizePart lowercases, replaces non-alphanumeric characters with hyphens,
// collapses multiple hyphens, and trims leading/trailing hyphens.
func sanitizePart(s string) string {
	s = strings.ToLower(s)
	s = sanitizeRe.ReplaceAllString(s, "-")
	s = multiHyphenRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// SessionName returns a tmux session name in the form ai-chat-<workspace>-<agent>
// with both parts sanitized for safe use as tmux session names.
func SessionName(workspace, agent string) string {
	w := sanitizePart(workspace)
	a := sanitizePart(agent)
	return sessionPrefix + w + "-" + a
}

// ParseSessionName is the inverse of SessionName. It accepts a session name
// and a list of known agent suffixes and returns the workspace and agent parts.
// It returns ok=false if the name does not have the ai-chat- prefix or if no
// known agent suffix matches. Agent matching is done from the right so that
// workspace names containing hyphens are handled correctly.
func ParseSessionName(name string, knownAgents []string) (workspace, agent string, ok bool) {
	if !strings.HasPrefix(name, sessionPrefix) {
		return "", "", false
	}
	rest := name[len(sessionPrefix):]
	if rest == "" {
		return "", "", false
	}

	// Try each known agent, matching the longest first to handle agents like
	// "claude-oneshot" before "claude".
	bestAgent := ""
	bestWorkspace := ""
	for _, a := range knownAgents {
		suffix := "-" + a
		if strings.HasSuffix(rest, suffix) {
			ws := rest[:len(rest)-len(suffix)]
			if ws == "" {
				continue
			}
			// Pick the longest matching agent to resolve ambiguity.
			if len(a) > len(bestAgent) {
				bestAgent = a
				bestWorkspace = ws
			}
		}
	}

	if bestAgent == "" {
		return "", "", false
	}
	return bestWorkspace, bestAgent, true
}
