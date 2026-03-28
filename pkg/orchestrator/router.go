package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBaseURL  = "https://openrouter.ai/api/v1"
	responseMaxSize = 1 << 20 // 1 MB
)

// Router sends chat completion requests to OpenRouter.
type Router struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

// NewRouter creates a Router with the given API key and the default OpenRouter
// base URL. Called by main.go at startup with the key from config. The apiKey
// field is unexported and only used internally for the Authorization header.
func NewRouter(apiKey string) *Router {
	return &Router{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
	}
}

// WithBaseURL returns the Router with a custom base URL (for testing with
// httptest). Default is "https://openrouter.ai/api/v1".
func (r *Router) WithBaseURL(url string) *Router {
	r.baseURL = url
	return r
}

// Complete sends a chat completion request and returns the response content.
func (r *Router) Complete(ctx context.Context, model string, messages []Message) (string, error) {
	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("router: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("router: create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("router: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, responseMaxSize))
	if err != nil {
		return "", fmt.Errorf("router: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("router: status %d: %s", resp.StatusCode, snippet)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("router: unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("router: api error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("router: empty response")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// CompleteWithTools sends a chat completion request with tool definitions and
// returns the full response so callers can inspect tool calls and finish reason.
func (r *Router) CompleteWithTools(ctx context.Context, model string, messages []any, tools []ToolDef) (*ChatResponse, error) {
	reqBody := map[string]any{
		"model":    model,
		"messages": messages,
		"tools":    tools,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("router: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("router: create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("router: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, responseMaxSize))
	if err != nil {
		return nil, fmt.Errorf("router: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("router: status %d: %s", resp.StatusCode, snippet)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("router: unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return nil, fmt.Errorf("router: api error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("router: empty response")
	}

	return &chatResp, nil
}
