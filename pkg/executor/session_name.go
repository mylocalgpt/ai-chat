package executor

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

const sessionPrefix = "ai-chat-"

// MaxWorkspaceNameLen is the maximum allowed length for a workspace name
// after sanitization.
const MaxWorkspaceNameLen = 20

// slugCharset is the set of characters used for random slug generation.
const slugCharset = "abcdefghijklmnopqrstuvwxyz0123456789"

// slugLen is the length of the random slug.
const slugLen = 4

// slugRe matches exactly 4 lowercase alphanumeric characters.
var slugRe = regexp.MustCompile(`^[a-z0-9]{4}$`)

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

// NewSessionName generates a session name with a random 4-char slug.
// Returns the full name ("ai-chat-lab-a3f2") and the slug ("a3f2").
func NewSessionName(workspace string) (name, slug string, err error) {
	w := sanitizePart(workspace)
	if w == "" {
		return "", "", fmt.Errorf("workspace name is empty after sanitization")
	}

	slug, err = generateSlug()
	if err != nil {
		return "", "", fmt.Errorf("generating slug: %w", err)
	}

	name = sessionPrefix + w + "-" + slug
	return name, slug, nil
}

// ParseSessionSlug extracts workspace and slug from a session name.
// Returns ok=false if the name doesn't match "ai-chat-<workspace>-<slug>" format.
func ParseSessionSlug(name string) (workspace, slug string, ok bool) {
	if !strings.HasPrefix(name, sessionPrefix) {
		return "", "", false
	}
	rest := name[len(sessionPrefix):]
	if rest == "" {
		return "", "", false
	}

	// Split on the last hyphen. The trailing segment must be exactly 4
	// lowercase alphanumeric characters to be recognized as a slug.
	lastHyphen := strings.LastIndex(rest, "-")
	if lastHyphen < 1 { // must have at least 1 char for workspace
		return "", "", false
	}

	candidate := rest[lastHyphen+1:]
	if !slugRe.MatchString(candidate) {
		return "", "", false
	}

	workspace = rest[:lastHyphen]
	if workspace == "" {
		return "", "", false
	}

	return workspace, candidate, true
}

// ValidateWorkspaceName checks workspace name length against MaxWorkspaceNameLen.
// Returns an error if too long.
func ValidateWorkspaceName(name string) error {
	if len(name) > MaxWorkspaceNameLen {
		return fmt.Errorf("workspace name %q exceeds max length %d", name, MaxWorkspaceNameLen)
	}
	return nil
}

// generateSlug creates a random 4-character alphanumeric slug using crypto/rand.
func generateSlug() (string, error) {
	b := make([]byte, slugLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = slugCharset[int(b[i])%len(slugCharset)]
	}
	return string(b), nil
}
