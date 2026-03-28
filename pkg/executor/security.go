package executor

import (
	"fmt"
	"regexp"
	"strings"
)

// SecurityFlag represents a detected security concern in text.
type SecurityFlag struct {
	Keyword  string
	Position int
	Context  string
}

// SecurityFlagError wraps security flags detected during adapter operations.
type SecurityFlagError struct {
	Flags []SecurityFlag
	Err   error
}

func (e *SecurityFlagError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("security flags detected (%d): %v", len(e.Flags), e.Err)
	}
	return fmt.Sprintf("security flags detected (%d)", len(e.Flags))
}

func (e *SecurityFlagError) Unwrap() error {
	return e.Err
}

// SecurityProxy scans text for credential patterns.
type SecurityProxy struct {
	keywords map[string]bool
	prefixes []string
}

// NewSecurityProxy returns a proxy with default keyword and prefix sets.
func NewSecurityProxy() *SecurityProxy {
	return &SecurityProxy{
		keywords: map[string]bool{
			"password":    true,
			"passwd":      true,
			"secret":      true,
			"credential":  true,
			"api_key":     true,
			"apikey":      true,
			"token":       true,
			"private_key": true,
			"ssh_key":     true,
			"access_key":  true,
		},
		prefixes: []string{
			"Bearer ",
			"sk-",
			"ghp_",
			"ghs_",
			"AKIA",
		},
	}
}

// Scan searches text for security patterns and returns all flags found.
func (s *SecurityProxy) Scan(text string) []SecurityFlag {
	if s == nil {
		return nil
	}

	flags := make([]SecurityFlag, 0)

	// Keyword matching (case-insensitive, substring-based).
	lowerText := strings.ToLower(text)
	for keyword := range s.keywords {
		start := 0
		for {
			idx := strings.Index(lowerText[start:], keyword)
			if idx == -1 {
				break
			}
			pos := start + idx
			flags = append(flags, SecurityFlag{
				Keyword:  keyword,
				Position: pos,
				Context:  extractContext(text, pos, len(keyword)),
			})
			start = pos + 1
		}
	}

	// Prefix matching (case-sensitive).
	for _, prefix := range s.prefixes {
		start := 0
		for {
			idx := strings.Index(text[start:], prefix)
			if idx == -1 {
				break
			}
			pos := start + idx
			flags = append(flags, SecurityFlag{
				Keyword:  prefix,
				Position: pos,
				Context:  extractContext(text, pos, len(prefix)),
			})
			start = pos + 1
		}
	}

	// Env pattern detection.
	flags = append(flags, s.scanEnvPatterns(text)...)

	// Dotenv reference detection.
	flags = append(flags, s.scanDotenvReferences(text)...)

	return flags
}

var envPatternRe = regexp.MustCompile(`(?m)^export\s+\w*(?:KEY|SECRET|TOKEN|PASSWORD|CREDENTIAL)\w*=`)

func (s *SecurityProxy) scanEnvPatterns(text string) []SecurityFlag {
	var flags []SecurityFlag
	matches := envPatternRe.FindAllStringIndex(text, -1)
	for _, match := range matches {
		line := text[match[0]:match[1]]
		if len(line) > 80 {
			line = line[:80]
		}
		flags = append(flags, SecurityFlag{
			Keyword:  "export-env-var",
			Position: match[0],
			Context:  line,
		})
	}
	return flags
}

var dotenvPatternRe = regexp.MustCompile(`\.env(?:\.\w+)?`)

func (s *SecurityProxy) scanDotenvReferences(text string) []SecurityFlag {
	var flags []SecurityFlag
	matches := dotenvPatternRe.FindAllStringIndex(text, -1)
	for _, match := range matches {
		flags = append(flags, SecurityFlag{
			Keyword:  "dotenv-reference",
			Position: match[0],
			Context:  extractContext(text, match[0], match[1]-match[0]),
		})
	}
	return flags
}

// extractContext returns ~50 chars centered on the match position.
func extractContext(text string, pos, length int) string {
	const contextRadius = 25

	start := pos - contextRadius
	if start < 0 {
		start = 0
	}

	end := pos + length + contextRadius
	if end > len(text) {
		end = len(text)
	}

	return text[start:end]
}
