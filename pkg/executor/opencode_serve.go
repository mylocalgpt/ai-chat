package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

// SSEStream reads server-sent events from an HTTP response body.
type SSEStream struct {
	reader *bufio.Reader
	body   io.ReadCloser
}

// Next reads the next SSE event from the stream.
// Returns the event type, the raw JSON data, and any error.
// Returns io.EOF when the stream is exhausted.
func (s *SSEStream) Next() (eventType string, data []byte, err error) {
	var eventName string
	var dataLines [][]byte

	for {
		line, err := s.reader.ReadString('\n')
		// Trim trailing newline/carriage return.
		line = strings.TrimRight(line, "\r\n")

		if err != nil {
			// If we hit EOF mid-event with accumulated data, return it.
			if err == io.EOF && len(dataLines) > 0 {
				return eventName, bytes.Join(dataLines, []byte("\n")), nil
			}
			return "", nil, err
		}

		// Blank line signals end of event.
		if line == "" {
			if len(dataLines) > 0 || eventName != "" {
				return eventName, bytes.Join(dataLines, []byte("\n")), nil
			}
			// Blank line with no accumulated event data; keep reading.
			continue
		}

		// SSE comment lines start with ':'.
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))))
		}
		// Other fields (id:, retry:) are ignored per SSE spec.
	}
}

// Close closes the underlying response body.
func (s *SSEStream) Close() error {
	return s.body.Close()
}

// SubscribeEvents opens a long-lived SSE connection to the opencode serve
// event stream. The returned SSEStream must be closed by the caller.
func (h *ServerHandle) SubscribeEvents(ctx context.Context) (*SSEStream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL+"/event", nil)
	if err != nil {
		return nil, fmt.Errorf("subscribe events: build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-opencode-directory", h.Workspace)

	// Use a client with no timeout; SSE connections are long-lived and would
	// be killed by the handle's default 30s timeout.
	sseClient := &http.Client{Timeout: 0}
	resp, err := sseClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("subscribe events: request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("subscribe events: unexpected status %d", resp.StatusCode)
	}

	return &SSEStream{
		reader: bufio.NewReader(resp.Body),
		body:   resp.Body,
	}, nil
}

// parseSSEEvent maps a raw SSE event to a typed AgentEvent.
// Returns (event, true) for recognized events, (AgentEvent{}, false) for
// ignored or unknown events.
func parseSSEEvent(eventType string, data []byte) (core.AgentEvent, bool) {
	switch eventType {
	case "message.part.delta":
		var d struct {
			Field string `json:"field"`
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(data, &d); err != nil {
			return core.AgentEvent{}, false
		}
		if d.Field == "text" {
			return core.AgentEvent{Type: core.EventTextDelta, Text: d.Delta}, true
		}
		return core.AgentEvent{}, false

	case "message.part.updated":
		var p struct {
			Type     string           `json:"type"`
			Text     string           `json:"text"`
			ToolName string           `json:"toolName"`
			Input    json.RawMessage  `json:"input"`
			Output   string           `json:"output"`
			Tokens   *core.TokenUsage `json:"tokens"`
			Cost     float64          `json:"cost"`
			Reason   string           `json:"reason"`
		}
		if err := json.Unmarshal(data, &p); err != nil {
			return core.AgentEvent{}, false
		}
		switch p.Type {
		case "text":
			return core.AgentEvent{Type: core.EventText, Text: p.Text}, true
		case "tool-use":
			return core.AgentEvent{Type: core.EventToolUse, ToolName: p.ToolName, ToolInput: string(p.Input)}, true
		case "tool-result":
			return core.AgentEvent{Type: core.EventToolResult, ToolName: p.ToolName, ToolOutput: p.Output}, true
		case "step-start":
			return core.AgentEvent{Type: core.EventStepStart}, true
		case "step-finish":
			return core.AgentEvent{Type: core.EventStepFinish, Tokens: p.Tokens, Cost: p.Cost, Reason: p.Reason}, true
		}
		return core.AgentEvent{}, false

	case "session.status":
		var s struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &s); err != nil {
			return core.AgentEvent{}, false
		}
		switch s.Type {
		case "busy":
			return core.AgentEvent{Type: core.EventBusy}, true
		case "idle":
			return core.AgentEvent{Type: core.EventIdle}, true
		}
		return core.AgentEvent{}, false

	default:
		// server.heartbeat, message.updated, server.connected, unknown
		return core.AgentEvent{}, false
	}
}

// CreateSession creates a new session on the opencode serve instance.
// Returns the agent session ID (e.g. "ses_xxx").
func (h *ServerHandle) CreateSession(ctx context.Context, title string) (string, error) {
	body, err := json.Marshal(map[string]string{"title": title})
	if err != nil {
		return "", fmt.Errorf("create session: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL+"/session", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create session: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-opencode-directory", h.Workspace)

	resp, err := h.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create session: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("create session: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("create session: decode response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("create session: empty session ID in response")
	}
	return result.ID, nil
}

// SendPrompt sends a message to an existing session and returns the response
// body for streaming. The caller is responsible for closing the returned
// ReadCloser.
func (h *ServerHandle) SendPrompt(ctx context.Context, sessionID, message string) (io.ReadCloser, error) {
	payload := map[string]any{
		"parts": []map[string]string{
			{"type": "text", "text": message},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("send prompt: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL+"/session/"+sessionID+"/message", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("send prompt: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-opencode-directory", h.Workspace)

	// Use a client with no timeout; the response streams until the agent finishes.
	promptClient := &http.Client{Timeout: 0}
	resp, err := promptClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send prompt: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("send prompt: unexpected status %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// Abort cancels in-flight generation for the given session.
func (h *ServerHandle) Abort(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL+"/session/"+sessionID+"/abort", nil)
	if err != nil {
		return fmt.Errorf("abort: build request: %w", err)
	}
	req.Header.Set("x-opencode-directory", h.Workspace)

	resp, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("abort: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("abort: unexpected status %d", resp.StatusCode)
	}
	return nil
}
