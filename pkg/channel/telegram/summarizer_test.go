package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

func TestParseResponseText(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "multiple text parts",
			body: `{"info":{},"parts":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}]}`,
			want: "Hello world",
		},
		{
			name: "empty parts array",
			body: `{"info":{},"parts":[]}`,
			want: "",
		},
		{
			name: "non-text parts skipped",
			body: `{"info":{},"parts":[{"type":"step-start","stepId":"abc"},{"type":"text","text":"summary"},{"type":"step-finish","reason":"stop"}]}`,
			want: "summary",
		},
		{
			name: "single text part",
			body: `{"info":{},"parts":[{"type":"text","text":"just one part"}]}`,
			want: "just one part",
		},
		{
			name:    "invalid json",
			body:    `{not valid`,
			wantErr: true,
		},
		{
			name: "no parts field",
			body: `{"info":{}}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := io.NopCloser(strings.NewReader(tt.body))
			got, err := parseResponseText(r)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseResponseText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseResponseText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFallbackSummary(t *testing.T) {
	tests := []struct {
		name    string
		content string
		maxLen  int
		want    string
	}{
		{
			name:    "short content returned unchanged",
			content: "Hello world",
			maxLen:  100,
			want:    "Hello world",
		},
		{
			name:    "exact length returned unchanged",
			content: "12345",
			maxLen:  5,
			want:    "12345",
		},
		{
			name:    "long content truncated with ellipsis",
			content: "abcdefghij",
			maxLen:  8,
			want:    "abcde...",
		},
		{
			name:    "unicode handled correctly",
			content: "Hello, \u4e16\u754c\uff01\u3053\u3093\u306b\u3061\u306f",
			maxLen:  10,
			want:    "Hello, \u4e16\u754c\u3001...", // Wait, let me recalculate
		},
	}

	// Fix the unicode test case: "Hello, 世界！こんにちは" is 13 runes.
	// With maxLen=10: first 7 runes = "Hello, 世" + "..." won't work right.
	// Let's be precise:
	// H,e,l,l,o,,,_,世,界,！,こ,ん,に,ち,は = 15 runes... let me recount.
	// "Hello, " = 7 runes, "世界！こんにちは" = 8 runes, total = 15.
	// maxLen=10 -> runes[:7] + "..." = "Hello, 世..."
	// Wait, runes[:10-3] = runes[:7] = "Hello, " + "..." = "Hello, ..."
	// Let me just use a cleaner test:
	tests[3] = struct {
		name    string
		content string
		maxLen  int
		want    string
	}{
		name:    "unicode handled correctly",
		content: "\u4e16\u754c\u3053\u3093\u306b\u3061\u306f\u5143\u6c17\u3067\u3059\u304b", // 世界こんにちは元気ですか (12 runes)
		maxLen:  8,
		want:    "\u4e16\u754c\u3053\u3093\u306b...", // 世界こんに... (5 runes + "...")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fallbackSummary(tt.content, tt.maxLen)
			if got != tt.want {
				t.Errorf("fallbackSummary(%q, %d) = %q, want %q", tt.content, tt.maxLen, got, tt.want)
			}
		})
	}
}

// newMockOpenCodeServer creates a test HTTP server that mimics the opencode
// serve session lifecycle endpoints. Returns the server and a cleanup function.
// If releaseMsg is non-nil, the message handler blocks until it's closed
// (instead of using a real time.Sleep, which stalls httptest.Server.Close).
func newMockOpenCodeServer(t *testing.T, responseText string, releaseMsg <-chan struct{}) *httptest.Server {
	t.Helper()

	var createdSession string
	var deletedSession string

	mux := http.NewServeMux()

	// POST /session - create session
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		createdSession = "ses_test_summary"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": createdSession})
	})

	// POST /session/{id}/message - send prompt, return response
	mux.HandleFunc("POST /session/{id}/message", func(w http.ResponseWriter, r *http.Request) {
		if releaseMsg != nil {
			<-releaseMsg
			return
		}

		resp := responseMessage{
			Parts: []responsePart{
				{Type: "text", Text: responseText},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// DELETE /session/{id} - delete session
	mux.HandleFunc("DELETE /session/{id}", func(w http.ResponseWriter, r *http.Request) {
		deletedSession = r.PathValue("id")
		_ = deletedSession // used for verification if needed
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /global/health - health check
	mux.HandleFunc("GET /global/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})

	return httptest.NewServer(mux)
}

// newTestServerManager creates a ServerManager with a pre-registered handle
// pointing at the given test server URL.
func newTestServerManager(t *testing.T, serverURL, workspace string) *executor.ServerManager {
	t.Helper()

	mgr := executor.NewServerManager()
	handle := &executor.ServerHandle{
		URL:       serverURL,
		Workspace: workspace,
		Client:    &http.Client{Timeout: 30 * time.Second},
		LastUsed:  time.Now(),
	}
	mgr.Register(workspace, handle)
	return mgr
}

func TestSummarize(t *testing.T) {
	const workspace = "/tmp/test-workspace"
	const summaryText = "- Created a new file\n- Fixed 3 bugs\n- Updated tests"

	ts := newMockOpenCodeServer(t, summaryText, nil)
	defer ts.Close()

	mgr := newTestServerManager(t, ts.URL, workspace)
	summarizer := NewSummarizer(mgr)

	got, err := summarizer.Summarize(context.Background(), "long response content here...", workspace)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if got != summaryText {
		t.Errorf("Summarize() = %q, want %q", got, summaryText)
	}
}

func TestSummarizeTimeout(t *testing.T) {
	const workspace = "/tmp/test-workspace"

	// Block the message handler until the test is done, without a real
	// time.Sleep that stalls httptest.Server.Close for 60 seconds.
	release := make(chan struct{})
	ts := newMockOpenCodeServer(t, "should not return", release)
	defer func() {
		close(release)
		ts.Close()
	}()

	mgr := newTestServerManager(t, ts.URL, workspace)
	summarizer := NewSummarizer(mgr)

	// Use a context with a very short timeout to verify timeout behavior.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := summarizer.Summarize(ctx, "content", workspace)
	if err == nil {
		t.Fatal("Summarize() expected error for timeout, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Summarize() error = %v, want context deadline exceeded", err)
	}
}
