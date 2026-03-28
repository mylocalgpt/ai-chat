package executor

import (
	"errors"
	"strings"
	"testing"
)

func TestSecurityProxyKeywordDetection(t *testing.T) {
	proxy := NewSecurityProxy()

	flags := proxy.Scan("my password is secret123")
	if len(flags) == 0 {
		t.Error("expected flags for 'password' keyword")
	}

	// Check that password is among the flags.
	var foundPassword bool
	for _, f := range flags {
		if f.Keyword == "password" {
			foundPassword = true
		}
	}
	if !foundPassword {
		t.Error("expected flag for 'password' keyword")
	}
}

func TestSecurityProxyPrefixDetection(t *testing.T) {
	proxy := NewSecurityProxy()

	flags := proxy.Scan("Authorization: Bearer sk-abc123")
	if len(flags) == 0 {
		t.Error("expected flags for 'Bearer ' and 'sk-' prefixes")
	}

	var foundBearer, foundSk bool
	for _, f := range flags {
		if f.Keyword == "Bearer " {
			foundBearer = true
		}
		if f.Keyword == "sk-" {
			foundSk = true
		}
	}
	if !foundBearer {
		t.Error("expected flag for 'Bearer ' prefix")
	}
	if !foundSk {
		t.Error("expected flag for 'sk-' prefix")
	}
}

func TestSecurityProxyCaseInsensitiveKeywords(t *testing.T) {
	proxy := NewSecurityProxy()

	tests := []string{"PASSWORD", "Password", "pAsSwOrD"}
	for _, tc := range tests {
		flags := proxy.Scan(tc)
		if len(flags) == 0 {
			t.Errorf("expected flag for %q", tc)
		}
	}
}

func TestSecurityProxyCaseSensitivePrefixes(t *testing.T) {
	proxy := NewSecurityProxy()

	// SK- should NOT flag (prefix check is case-sensitive).
	flags := proxy.Scan("SK-abc123")
	for _, f := range flags {
		if f.Keyword == "sk-" {
			t.Error("SK- should not flag (prefix is case-sensitive)")
		}
	}

	// sk- should flag.
	flags = proxy.Scan("sk-abc123")
	var found bool
	for _, f := range flags {
		if f.Keyword == "sk-" {
			found = true
		}
	}
	if !found {
		t.Error("sk- should flag")
	}
}

func TestSecurityProxyContextExtraction(t *testing.T) {
	proxy := NewSecurityProxy()

	text := "this is a long string with password=secret in the middle"
	flags := proxy.Scan(text)
	if len(flags) == 0 {
		t.Fatal("expected flags")
	}

	// Context should be ~50 chars centered on match.
	ctx := flags[0].Context
	if len(ctx) > 60 {
		t.Errorf("context too long: %d chars", len(ctx))
	}
	if !strings.Contains(ctx, "password") {
		t.Error("context should contain the keyword")
	}
}

func TestSecurityProxyMultipleFlags(t *testing.T) {
	proxy := NewSecurityProxy()

	text := "password=abc and token=xyz and api_key=123"
	flags := proxy.Scan(text)
	if len(flags) < 3 {
		t.Errorf("expected at least 3 flags, got %d", len(flags))
	}
}

func TestSecurityProxyNoFlags(t *testing.T) {
	proxy := NewSecurityProxy()

	flags := proxy.Scan("hello world this is a normal message")
	if len(flags) != 0 {
		t.Errorf("expected no flags, got %d", len(flags))
	}
	// Should return empty slice, not nil.
	if flags == nil {
		t.Error("expected empty slice, not nil")
	}
}

func TestSecurityProxyEnvPattern(t *testing.T) {
	proxy := NewSecurityProxy()

	text := "export SECRET_KEY=abc123\nexport OTHER_VAR=value"
	flags := proxy.Scan(text)

	var found bool
	for _, f := range flags {
		if f.Keyword == "export-env-var" {
			found = true
		}
	}
	if !found {
		t.Error("expected flag for export-env-var pattern")
	}
}

func TestSecurityProxyDotenvReference(t *testing.T) {
	proxy := NewSecurityProxy()

	text := "load config from .env.local"
	flags := proxy.Scan(text)

	var found bool
	for _, f := range flags {
		if f.Keyword == "dotenv-reference" {
			found = true
		}
	}
	if !found {
		t.Error("expected flag for dotenv-reference pattern")
	}
}

func TestSecurityProxyAggressiveMatching(t *testing.T) {
	proxy := NewSecurityProxy()

	// "tokenize" should flag for "token" (by design).
	flags := proxy.Scan("let's tokenize this string")
	var found bool
	for _, f := range flags {
		if f.Keyword == "token" {
			found = true
		}
	}
	if !found {
		t.Error("expected aggressive matching to flag 'token' in 'tokenize'")
	}
}

func TestSecurityProxyNilSafe(t *testing.T) {
	var proxy *SecurityProxy
	flags := proxy.Scan("password=secret")
	if flags != nil {
		t.Error("nil proxy should return nil flags")
	}
}

func TestSecurityFlagError(t *testing.T) {
	flags := []SecurityFlag{{Keyword: "password", Position: 0, Context: "password=abc"}}
	err := &SecurityFlagError{Flags: flags}

	if !strings.Contains(err.Error(), "security flags detected") {
		t.Errorf("error message = %q, should contain 'security flags detected'", err.Error())
	}
}

func TestSecurityFlagErrorWithWrappedError(t *testing.T) {
	flags := []SecurityFlag{{Keyword: "password", Position: 0, Context: "password=abc"}}
	wrapped := errors.New("timeout")
	err := &SecurityFlagError{Flags: flags, Err: wrapped}

	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error message = %q, should contain 'timeout'", err.Error())
	}

	var unwrapped = errors.Unwrap(err)
	if unwrapped != wrapped {
		t.Error("Unwrap should return the wrapped error")
	}
}
