package executor

import (
	"regexp"
	"testing"
)

func TestNewSessionNameFormat(t *testing.T) {
	name, slug, err := NewSessionName("lab")
	if err != nil {
		t.Fatalf("NewSessionName: %v", err)
	}

	re := regexp.MustCompile(`^ai-chat-lab-[a-z0-9]{4}$`)
	if !re.MatchString(name) {
		t.Errorf("name %q does not match expected format ai-chat-lab-XXXX", name)
	}
	if len(slug) != 4 {
		t.Errorf("slug %q length = %d, want 4", slug, len(slug))
	}

	slugRe := regexp.MustCompile(`^[a-z0-9]{4}$`)
	if !slugRe.MatchString(slug) {
		t.Errorf("slug %q is not 4 alphanumeric chars", slug)
	}

	// Name should end with the slug.
	if name != "ai-chat-lab-"+slug {
		t.Errorf("name %q does not end with slug %q", name, slug)
	}
}

func TestNewSessionNameSanitizesWorkspace(t *testing.T) {
	name, _, err := NewSessionName("My Project!")
	if err != nil {
		t.Fatalf("NewSessionName: %v", err)
	}

	re := regexp.MustCompile(`^ai-chat-my-project-[a-z0-9]{4}$`)
	if !re.MatchString(name) {
		t.Errorf("name %q does not match expected sanitized format", name)
	}
}

func TestNewSessionNameEmptyWorkspace(t *testing.T) {
	_, _, err := NewSessionName("...")
	if err == nil {
		t.Error("expected error for empty workspace after sanitization")
	}
}

func TestNewSessionNameUniqueSlugs(t *testing.T) {
	_, slug1, err := NewSessionName("proj")
	if err != nil {
		t.Fatalf("NewSessionName 1: %v", err)
	}
	_, slug2, err := NewSessionName("proj")
	if err != nil {
		t.Fatalf("NewSessionName 2: %v", err)
	}

	// Probabilistic: 1 in 1.7M chance of collision. Acceptable.
	if slug1 == slug2 {
		t.Errorf("two calls produced identical slugs: %q", slug1)
	}
}

func TestParseSessionSlugValid(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantWorkspace string
		wantSlug      string
	}{
		{"simple", "ai-chat-lab-a3f2", "lab", "a3f2"},
		{"hyphenated workspace", "ai-chat-my-project-x1y2", "my-project", "x1y2"},
		{"numeric slug", "ai-chat-proj-0000", "proj", "0000"},
		{"long workspace", "ai-chat-a-b-c-d-z9z9", "a-b-c-d", "z9z9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, slug, ok := ParseSessionSlug(tt.input)
			if !ok {
				t.Fatalf("ParseSessionSlug(%q) returned ok=false", tt.input)
			}
			if ws != tt.wantWorkspace {
				t.Errorf("workspace = %q, want %q", ws, tt.wantWorkspace)
			}
			if slug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", slug, tt.wantSlug)
			}
		})
	}
}

func TestParseSessionSlugInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"wrong prefix", "other-lab-a3f2"},
		{"no prefix", "lab-a3f2"},
		{"empty after prefix", "ai-chat-"},
		{"slug too short", "ai-chat-lab-a3f"},
		{"slug too long", "ai-chat-lab-a3f2x"},
		{"slug with uppercase", "ai-chat-lab-A3F2"},
		{"slug with special char", "ai-chat-lab-a3f!"},
		{"no slug", "ai-chat-lab"},
		{"empty workspace", "ai-chat--a3f2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := ParseSessionSlug(tt.input)
			if ok {
				t.Errorf("ParseSessionSlug(%q) returned ok=true, want false", tt.input)
			}
		})
	}
}

func TestParseSessionSlugRoundTrip(t *testing.T) {
	name, slug, err := NewSessionName("my-project")
	if err != nil {
		t.Fatalf("NewSessionName: %v", err)
	}

	ws, parsedSlug, ok := ParseSessionSlug(name)
	if !ok {
		t.Fatalf("ParseSessionSlug(%q) returned ok=false", name)
	}
	if ws != "my-project" {
		t.Errorf("workspace = %q, want %q", ws, "my-project")
	}
	if parsedSlug != slug {
		t.Errorf("slug = %q, want %q", parsedSlug, slug)
	}
}

func TestValidateWorkspaceName(t *testing.T) {
	// Valid: exactly at limit.
	if err := ValidateWorkspaceName("12345678901234567890"); err != nil {
		t.Errorf("20-char name should be valid: %v", err)
	}

	// Invalid: over limit.
	if err := ValidateWorkspaceName("123456789012345678901"); err == nil {
		t.Error("21-char name should be rejected")
	}

	// Valid: short name.
	if err := ValidateWorkspaceName("lab"); err != nil {
		t.Errorf("short name should be valid: %v", err)
	}

	// Valid: empty name (validation only checks length).
	if err := ValidateWorkspaceName(""); err != nil {
		t.Errorf("empty name should pass length validation: %v", err)
	}
}
