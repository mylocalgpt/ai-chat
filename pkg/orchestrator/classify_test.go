package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestBuildClassifyPrompt_WithWorkspace(t *testing.T) {
	msg := core.InboundMessage{Content: "switch to myproject"}
	userCtx := UserContext{
		SenderID: "user1",
		Channel:  "telegram",
		ActiveWorkspace: &core.Workspace{
			Name: "current-ws",
			Path: "/home/user/current",
		},
	}
	workspaces := []core.Workspace{
		{Name: "current-ws", Path: "/home/user/current"},
		{Name: "myproject", Path: "/home/user/myproject"},
	}

	messages := buildClassifyPrompt(msg, userCtx, workspaces)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("first message role = %q, want 'system'", messages[0].Role)
	}
	if messages[1].Role != "user" {
		t.Errorf("second message role = %q, want 'user'", messages[1].Role)
	}

	userContent := messages[1].Content
	if !strings.Contains(userContent, "current-ws") {
		t.Error("user message should contain current workspace name")
	}
	if !strings.Contains(userContent, "/home/user/current") {
		t.Error("user message should contain current workspace path")
	}
	if !strings.Contains(userContent, "myproject: /home/user/myproject") {
		t.Error("user message should contain available workspaces")
	}
	if !strings.Contains(userContent, "switch to myproject") {
		t.Error("user message should contain the original message")
	}
}

func TestBuildClassifyPrompt_NilWorkspace(t *testing.T) {
	msg := core.InboundMessage{Content: "hello"}
	userCtx := UserContext{
		SenderID:        "user1",
		Channel:         "telegram",
		ActiveWorkspace: nil,
	}

	messages := buildClassifyPrompt(msg, userCtx, nil)
	userContent := messages[1].Content

	if !strings.Contains(userContent, "Current workspace: none") {
		t.Errorf("should show 'none' for nil workspace, got: %s", userContent)
	}
	if !strings.Contains(userContent, "Available workspaces: none") {
		t.Errorf("should show 'none' for empty workspace list, got: %s", userContent)
	}
}

func mockRouterResponse(t *testing.T, response string) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: response}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return NewRouter("test-key").WithBaseURL(srv.URL)
}

func mockRouterError(t *testing.T) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	t.Cleanup(srv.Close)
	return NewRouter("test-key").WithBaseURL(srv.URL)
}

func TestClassifyIntent_ValidJSON(t *testing.T) {
	response := `{"type":"agent_task","workspace":"myproject","agent":"claude","content":"fix the bug","confidence":0.95,"reasoning":"user wants code work"}`
	router := mockRouterResponse(t, response)

	action, err := classifyIntent(context.Background(), router, "test/model",
		core.InboundMessage{Content: "fix the bug in myproject"},
		UserContext{SenderID: "user1", Channel: "telegram"},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Type != ActionAgentTask {
		t.Errorf("type = %q, want %q", action.Type, ActionAgentTask)
	}
	if action.Workspace != "myproject" {
		t.Errorf("workspace = %q, want 'myproject'", action.Workspace)
	}
	if action.Confidence != 0.95 {
		t.Errorf("confidence = %f, want 0.95", action.Confidence)
	}
}

func TestClassifyIntent_JSONInCodeFences(t *testing.T) {
	response := "```json\n" + `{"type":"status","workspace":"","agent":"","content":"checking status","confidence":0.9,"reasoning":"status query"}` + "\n```"
	router := mockRouterResponse(t, response)

	action, err := classifyIntent(context.Background(), router, "test/model",
		core.InboundMessage{Content: "what's the status?"},
		UserContext{SenderID: "user1", Channel: "telegram"},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Type != ActionStatus {
		t.Errorf("type = %q, want %q", action.Type, ActionStatus)
	}
}

func TestClassifyIntent_InvalidActionType(t *testing.T) {
	response := `{"type":"unknown_action","workspace":"","agent":"","content":"test","confidence":0.5,"reasoning":"test"}`
	router := mockRouterResponse(t, response)

	_, err := classifyIntent(context.Background(), router, "test/model",
		core.InboundMessage{Content: "test"},
		UserContext{SenderID: "user1", Channel: "telegram"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for invalid action type")
	}
	if !strings.Contains(err.Error(), "unknown action type") {
		t.Errorf("error %q should contain 'unknown action type'", err.Error())
	}
}

func TestClassifyIntent_MalformedJSON(t *testing.T) {
	router := mockRouterResponse(t, "this is not json at all")

	_, err := classifyIntent(context.Background(), router, "test/model",
		core.InboundMessage{Content: "test"},
		UserContext{SenderID: "user1", Channel: "telegram"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "invalid json") {
		t.Errorf("error %q should contain 'invalid json'", err.Error())
	}
}

func TestClassifyIntent_MissingConfidence(t *testing.T) {
	response := `{"type":"direct_answer","workspace":"","agent":"","content":"42","reasoning":"simple answer"}`
	router := mockRouterResponse(t, response)

	action, err := classifyIntent(context.Background(), router, "test/model",
		core.InboundMessage{Content: "what is 6*7?"},
		UserContext{SenderID: "user1", Channel: "telegram"},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Confidence != 0.0 {
		t.Errorf("confidence = %f, want 0.0 for missing field", action.Confidence)
	}
}

func TestClassifyIntent_RouterError(t *testing.T) {
	router := mockRouterError(t)

	_, err := classifyIntent(context.Background(), router, "test/model",
		core.InboundMessage{Content: "test"},
		UserContext{SenderID: "user1", Channel: "telegram"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error from router")
	}
	if !strings.Contains(err.Error(), "classify") {
		t.Errorf("error %q should contain 'classify'", err.Error())
	}
}

func TestClassifyIntent_EmptyResponse(t *testing.T) {
	router := mockRouterResponse(t, "")

	_, err := classifyIntent(context.Background(), router, "test/model",
		core.InboundMessage{Content: "test"},
		UserContext{SenderID: "user1", Channel: "telegram"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error %q should contain 'empty response'", err.Error())
	}
}

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain json", `{"type":"test"}`, `{"type":"test"}`},
		{"json fence", "```json\n{\"type\":\"test\"}\n```", `{"type":"test"}`},
		{"plain fence", "```\n{\"type\":\"test\"}\n```", `{"type":"test"}`},
		{"with whitespace", "  ```json\n{\"type\":\"test\"}\n```  ", `{"type":"test"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCodeFences(tt.input)
			if got != tt.want {
				fmt.Printf("input: %q\n", tt.input)
				t.Errorf("stripCodeFences() = %q, want %q", got, tt.want)
			}
		})
	}
}
