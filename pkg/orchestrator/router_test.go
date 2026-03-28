package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatResponseJSON builds a JSON-encoded ChatResponse for test servers.
// It avoids inline anonymous struct literals that break when the struct changes.
func chatResponseJSON(content string) []byte {
	resp := map[string]any{
		"choices": []map[string]any{
			{
				"finish_reason": "stop",
				"message": map[string]any{
					"content": content,
				},
			},
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

func TestComplete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(chatResponseJSON("hello world"))
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	got, err := router.Complete(context.Background(), "test/model", []Message{
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestComplete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []any{},
			"error":   map[string]any{"message": "rate limited"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	_, err := router.Complete(context.Background(), "test/model", []Message{
		{Role: "user", Content: "hi"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "api error") {
		t.Fatalf("error %q should contain 'api error'", err.Error())
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error %q should contain 'rate limited'", err.Error())
	}
}

func TestComplete_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("too many requests"))
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	_, err := router.Complete(context.Background(), "test/model", []Message{
		{Role: "user", Content: "hi"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Fatalf("error %q should contain 'status 429'", err.Error())
	}
}

func TestComplete_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	_, err := router.Complete(context.Background(), "test/model", []Message{
		{Role: "user", Content: "hi"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("error %q should contain 'empty response'", err.Error())
	}
}

func TestComplete_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not valid json"))
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	_, err := router.Complete(context.Background(), "test/model", []Message{
		{Role: "user", Content: "hi"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("error %q should contain 'unmarshal'", err.Error())
	}
}

func TestComplete_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	_, err := router.Complete(ctx, "test/model", []Message{
		{Role: "user", Content: "hi"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("error %q should contain 'request failed'", err.Error())
	}
}

func TestComplete_HeadersSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer my-secret-key" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer my-secret-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q, want %q", got, "application/json")
		}
		_, _ = w.Write(chatResponseJSON("ok"))
	}))
	defer srv.Close()

	router := NewRouter("my-secret-key").WithBaseURL(srv.URL)
	_, err := router.Complete(context.Background(), "test/model", []Message{
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompleteWithTools_ToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"finish_reason": "tool_calls",
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_abc123",
								"type": "function",
								"function": map[string]any{
									"name":      "agent_send",
									"arguments": `{"workspace_name":"proj1","message":"hello"}`,
								},
							},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	tools := []ToolDef{
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "agent_send",
				Description: "Send a message",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
	}

	resp, err := router.CompleteWithTools(context.Background(), "test/model", []any{
		Message{Role: "user", Content: "hi"},
	}, tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason %q, got %q", "tool_calls", resp.Choices[0].FinishReason)
	}

	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}

	tc := resp.Choices[0].Message.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("expected tool call ID %q, got %q", "call_abc123", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected tool call type %q, got %q", "function", tc.Type)
	}
	if tc.Function.Name != "agent_send" {
		t.Errorf("expected function name %q, got %q", "agent_send", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"workspace_name":"proj1","message":"hello"}` {
		t.Errorf("unexpected arguments: %s", tc.Function.Arguments)
	}
}

func TestCompleteWithTools_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	_, err := router.CompleteWithTools(context.Background(), "test/model", []any{
		Message{Role: "user", Content: "hi"},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("error %q should contain 'empty response'", err.Error())
	}
}
