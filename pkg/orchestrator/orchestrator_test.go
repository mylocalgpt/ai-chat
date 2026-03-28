package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// testInput is the input schema for the test tool.
type testInput struct {
	Echo string `json:"echo"`
}

// newTestMCPSession creates an MCP server with a simple "echo" tool and returns
// an in-process client session connected to it.
func newTestMCPSession(t *testing.T) *gomcp.ClientSession {
	t.Helper()

	srv := gomcp.NewServer(
		&gomcp.Implementation{Name: "test-server", Version: "0.1.0"},
		nil,
	)
	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "echo",
		Description: "Returns the input message",
	}, func(_ context.Context, _ *gomcp.CallToolRequest, input testInput) (*gomcp.CallToolResult, any, error) {
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{
				&gomcp.TextContent{Text: fmt.Sprintf("echoed: %s", input.Echo)},
			},
		}, nil, nil
	})

	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	ctx := context.Background()
	_, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := gomcp.NewClient(
		&gomcp.Implementation{Name: "test-client", Version: "0.1.0"},
		nil,
	)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	return session
}

// callTracker records requests and returns canned responses in sequence.
type callTracker struct {
	mu        sync.Mutex
	calls     int
	responses []string // raw JSON response bodies
}

func (ct *callTracker) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct.mu.Lock()
		idx := ct.calls
		ct.calls++
		ct.mu.Unlock()

		if idx < len(ct.responses) {
			_, _ = w.Write([]byte(ct.responses[idx]))
		} else {
			// Default: return a stop response
			_, _ = w.Write(chatResponseJSON("fallback response"))
		}
	}
}

func TestHandleMessage_TextResponse(t *testing.T) {
	session := newTestMCPSession(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(chatResponseJSON("Hello! How can I help?"))
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	orch := NewOrchestrator(router, session, "test/model")

	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	got, err := orch.HandleMessage(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "test",
		Content:  "hello",
	}, "No active workspace.")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got != "Hello! How can I help?" {
		t.Errorf("got %q, want %q", got, "Hello! How can I help?")
	}
}

func TestHandleMessage_ToolCallThenText(t *testing.T) {
	session := newTestMCPSession(t)

	// First response: tool call. Second response: final text.
	toolCallResp := map[string]any{
		"choices": []map[string]any{
			{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"content": "",
					"tool_calls": []map[string]any{
						{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "echo",
								"arguments": `{"echo":"test message"}`,
							},
						},
					},
				},
			},
		},
	}
	toolCallJSON, _ := json.Marshal(toolCallResp)

	ct := &callTracker{
		responses: []string{
			string(toolCallJSON),
			string(chatResponseJSON("The echo tool returned: echoed: test message")),
		},
	}

	srv := httptest.NewServer(ct.handler())
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	orch := NewOrchestrator(router, session, "test/model")

	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	got, err := orch.HandleMessage(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "test",
		Content:  "echo something",
	}, "")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got != "The echo tool returned: echoed: test message" {
		t.Errorf("got %q", got)
	}

	ct.mu.Lock()
	calls := ct.calls
	ct.mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 router calls, got %d", calls)
	}
}

func TestHandleMessage_MaxIterations(t *testing.T) {
	session := newTestMCPSession(t)

	// Always return tool calls to exhaust the loop.
	toolCallResp := map[string]any{
		"choices": []map[string]any{
			{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"content": "",
					"tool_calls": []map[string]any{
						{
							"id":   "call_loop",
							"type": "function",
							"function": map[string]any{
								"name":      "echo",
								"arguments": `{"echo":"loop"}`,
							},
						},
					},
				},
			},
		},
	}
	toolCallJSON, _ := json.Marshal(toolCallResp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(toolCallJSON)
	}))
	defer srv.Close()

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	orch := NewOrchestrator(router, session, "test/model")

	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	got, err := orch.HandleMessage(context.Background(), core.InboundMessage{
		SenderID: "user1",
		Channel:  "test",
		Content:  "loop forever",
	}, "")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got != "I wasn't able to complete that request." {
		t.Errorf("got %q, want fallback message", got)
	}
}

func TestInit_LoadsTools(t *testing.T) {
	session := newTestMCPSession(t)

	router := NewRouter("test-key")
	orch := NewOrchestrator(router, session, "test/model")

	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if len(orch.tools) == 0 {
		t.Fatal("expected at least one tool after Init")
	}

	found := false
	for _, td := range orch.tools {
		if td.Function.Name == "echo" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'echo' tool")
	}
}
