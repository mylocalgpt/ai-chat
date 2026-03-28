package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "hello world"}},
			},
		})
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
		resp := ChatResponse{}
		resp.Error = &struct {
			Message string `json:"message"`
		}{Message: "rate limited"}
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
		_ = json.NewEncoder(w).Encode(ChatResponse{})
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
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "ok"}},
			},
		})
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
