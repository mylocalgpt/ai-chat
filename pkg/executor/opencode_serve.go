package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

// unwrapSSEEnvelope handles the opencode serve event envelope format.
// Events arrive as: data: {"type":"<event_type>","properties":{<payload>}}
// This function extracts the event type and inner payload so parseSSEEvent
// can process them with the same logic used for native SSE event: fields.
func unwrapSSEEnvelope(data []byte) (eventType string, payload []byte) {
	var envelope struct {
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type == "" {
		return "", data
	}
	if len(envelope.Properties) == 0 {
		return envelope.Type, nil
	}
	return envelope.Type, envelope.Properties
}

// parseSSEEvent maps a raw SSE event to a typed AgentEvent.
// Returns (event, true) for recognized events, (AgentEvent{}, false) for
// ignored or unknown events.
func parseSSEEvent(eventType string, data []byte) (core.AgentEvent, bool) {
	// opencode serve wraps events in an envelope:
	//   data: {"type":"message.part.delta","properties":{...}}
	// When there is no SSE "event:" field, unwrap the envelope to get the
	// real event type and payload.
	if eventType == "" && len(data) > 0 {
		eventType, data = unwrapSSEEnvelope(data)
	}

	switch eventType {
	case "message.part.delta":
		var d struct {
			Field string `json:"field"`
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(data, &d); err != nil {
			raw := string(data)
			if len(raw) > 200 {
				raw = raw[:200]
			}
			slog.Warn("sse parse failed", "event_type", eventType, "raw", raw, slog.Any("err", err))
			return core.AgentEvent{}, false
		}
		if d.Field == "text" {
			return core.AgentEvent{Type: core.EventTextDelta, Text: d.Delta}, true
		}
		return core.AgentEvent{}, false

	case "message.part.updated":
		// The envelope format nests the part data:
		//   {"sessionID":"...","part":{"type":"text","text":"..."}, ...}
		// Legacy/direct format has fields at the top level:
		//   {"type":"text","text":"..."}
		var wrapper struct {
			Part json.RawMessage `json:"part"`
		}
		partData := data
		if json.Unmarshal(data, &wrapper) == nil && len(wrapper.Part) > 0 {
			partData = wrapper.Part
		}

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
		if err := json.Unmarshal(partData, &p); err != nil {
			raw := string(data)
			if len(raw) > 200 {
				raw = raw[:200]
			}
			slog.Warn("sse parse failed", "event_type", eventType, "raw", raw, slog.Any("err", err))
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
		// The envelope format nests the status: {"sessionID":"...","status":{"type":"idle"}}
		// Legacy/direct format is just: {"type":"idle"}
		var s struct {
			Type   string `json:"type"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		if err := json.Unmarshal(data, &s); err != nil {
			raw := string(data)
			if len(raw) > 200 {
				raw = raw[:200]
			}
			slog.Warn("sse parse failed", "event_type", eventType, "raw", raw, slog.Any("err", err))
			return core.AgentEvent{}, false
		}
		statusType := s.Type
		if statusType == "" {
			statusType = s.Status.Type
		}
		switch statusType {
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("create session: unexpected status %d: %s", resp.StatusCode, body)
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("send prompt: unexpected status %d: %s", resp.StatusCode, body)
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("abort: unexpected status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// DeleteSession removes an ephemeral session from the opencode serve instance.
func (h *ServerHandle) DeleteSession(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, h.URL+"/session/"+sessionID, nil)
	if err != nil {
		return fmt.Errorf("delete session: build request: %w", err)
	}
	req.Header.Set("x-opencode-directory", h.Workspace)

	resp, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("delete session: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("delete session: unexpected status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// ---------------------------------------------------------------------------
// OpenCodeServeAdapter
// ---------------------------------------------------------------------------

// OpenCodeServeAdapter implements StreamingAdapter using opencode serve.
type OpenCodeServeAdapter struct {
	serverMgr  *ServerManager
	sessionIDs sync.Map // key: workspace+":"+slug -> OpenCode session ID
}

// NewOpenCodeServeAdapter returns a new serve-based adapter.
func NewOpenCodeServeAdapter(mgr *ServerManager) *OpenCodeServeAdapter {
	return &OpenCodeServeAdapter{serverMgr: mgr}
}

// Name returns the adapter name.
func (a *OpenCodeServeAdapter) Name() string {
	return "opencode"
}

// sessionKey builds a deterministic map key from a SessionInfo.
func sessionKey(s core.SessionInfo) string {
	return s.WorkspacePath + ":" + s.Slug
}

// Spawn ensures the opencode serve server is running and creates a new
// agent session when AgentSessionID is empty.
func (a *OpenCodeServeAdapter) Spawn(ctx context.Context, session core.SessionInfo) error {
	slog.Debug("adapter spawn", "session", session.Name, "workspace", session.WorkspacePath)
	handle, err := a.serverMgr.GetOrStart(session.WorkspacePath)
	if err != nil {
		return fmt.Errorf("opencode serve spawn: %w", err)
	}

	// Create the response file so message dispatch can append to it.
	dir := filepath.Dir(session.ResponseFile)
	if _, err := NewResponseFile(dir, session); err != nil {
		return fmt.Errorf("opencode serve spawn: create response file: %w", err)
	}

	if session.AgentSessionID == "" {
		id, err := handle.CreateSession(ctx, session.Name)
		if err != nil {
			return fmt.Errorf("opencode serve spawn: create session: %w", err)
		}
		a.sessionIDs.Store(sessionKey(session), id)
	}
	slog.Debug("adapter spawned", "session", session.Name, "workspace", session.WorkspacePath)
	return nil
}

// GetAgentSessionID returns the OpenCode session ID created during Spawn.
// Uses LoadAndDelete for read-once semantics to prevent stale data.
func (a *OpenCodeServeAdapter) GetAgentSessionID(session core.SessionInfo) string {
	v, ok := a.sessionIDs.LoadAndDelete(sessionKey(session))
	if !ok {
		return ""
	}
	return v.(string)
}

// SendStream sends a message and returns a channel of streaming agent events.
// SSE subscription happens before sending the prompt to avoid missing early events.
func (a *OpenCodeServeAdapter) SendStream(ctx context.Context, session core.SessionInfo, message string) (<-chan core.AgentEvent, error) {
	handle, err := a.serverMgr.GetOrStart(session.WorkspacePath)
	if err != nil {
		return nil, fmt.Errorf("opencode serve send stream: %w", err)
	}
	handle.Touch()

	// Subscribe to SSE BEFORE sending the prompt so no early events are missed.
	sse, err := handle.SubscribeEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("opencode serve send stream: subscribe: %w", err)
	}
	slog.Debug("sse subscribed", "session", session.Name)

	respBody, err := handle.SendPrompt(ctx, session.AgentSessionID, message)
	if err != nil {
		_ = sse.Close()
		return nil, fmt.Errorf("opencode serve send stream: send prompt: %w", err)
	}
	slog.Debug("prompt sent", "session", session.Name)

	ch := make(chan core.AgentEvent, 64)
	go a.consumeEvents(ctx, sse, respBody, session.Name, ch)
	return ch, nil
}

// consumeEvents reads SSE events and forwards parsed AgentEvents to ch.
// Stops on EventIdle, context cancellation, or stream error.
func (a *OpenCodeServeAdapter) consumeEvents(ctx context.Context, sse *SSEStream, respBody io.ReadCloser, sessionName string, ch chan<- core.AgentEvent) {
	defer close(ch)
	defer func() { _ = sse.Close() }()
	defer func() { _ = respBody.Close() }()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		eventType, data, err := sse.Next()
		if err != nil {
			if err == io.EOF {
				slog.Debug("sse stream ended", "session", sessionName)
				return
			}
			// Context cancellation surfaces as a read error; don't send
			// an extra EventError in that case.
			if ctx.Err() != nil {
				return
			}
			slog.Warn("sse read error", "session", sessionName, slog.Any("err", err))
			ch <- core.AgentEvent{Type: core.EventError, Text: err.Error()}
			return
		}

		evt, ok := parseSSEEvent(eventType, data)
		if !ok {
			continue
		}

		ch <- evt

		if evt.Type == core.EventIdle {
			return
		}
	}
}

// AbortStream cancels in-flight generation for the given session.
func (a *OpenCodeServeAdapter) AbortStream(ctx context.Context, session core.SessionInfo) error {
	handle, err := a.serverMgr.GetOrStart(session.WorkspacePath)
	if err != nil {
		return fmt.Errorf("opencode serve abort: %w", err)
	}
	return handle.Abort(ctx, session.AgentSessionID)
}

// Send is the synchronous fallback for the AgentAdapter interface.
// It calls SendStream, drains the channel, and writes the agent response
// to the response file.
func (a *OpenCodeServeAdapter) Send(ctx context.Context, session core.SessionInfo, message string) error {
	ch, err := a.SendStream(ctx, session, message)
	if err != nil {
		return err
	}

	var lastText string
	for evt := range ch {
		switch evt.Type {
		case core.EventText:
			// EventText is a complete snapshot; keep the latest one.
			lastText = evt.Text
		case core.EventError:
			return fmt.Errorf("opencode serve send: agent error: %s", evt.Text)
		}
	}

	if lastText != "" {
		if err := AppendMessage(session.ResponseFile, ResponseMessage{
			Role:      "agent",
			Content:   lastText,
			Timestamp: time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("opencode serve send: append agent message: %w", err)
		}
	}
	return nil
}

// IsAlive checks whether the opencode serve server is healthy.
func (a *OpenCodeServeAdapter) IsAlive(session core.SessionInfo) bool {
	handle, ok := a.serverMgr.Get(session.WorkspacePath)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return handle.Health(ctx) == nil
}

// Stop is a no-op. Individual sessions don't own the server;
// the idle reaper in ServerManager handles server shutdown.
func (a *OpenCodeServeAdapter) Stop(_ context.Context, _ core.SessionInfo) error {
	return nil
}
