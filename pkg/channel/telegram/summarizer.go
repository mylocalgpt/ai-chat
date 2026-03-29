package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mylocalgpt/ai-chat/pkg/executor"
)

const summarizeTimeout = 30 * time.Second

const summarizePrompt = "Summarize the following AI agent response in 3-5 bullet points. Keep it under 2000 characters. Focus on what was done and key findings. Use markdown formatting.\n\n---\n\n"

// Summarizer creates ephemeral sessions on an opencode serve instance to
// generate AI-powered summaries of long agent responses.
type Summarizer struct {
	serverMgr *executor.ServerManager
}

// NewSummarizer returns a new Summarizer that uses the given ServerManager
// to obtain server handles for summarization requests.
func NewSummarizer(serverMgr *executor.ServerManager) *Summarizer {
	return &Summarizer{serverMgr: serverMgr}
}

// Summarize sends content to an ephemeral opencode serve session and returns
// the AI-generated summary. The caller handles fallback on error.
func (s *Summarizer) Summarize(ctx context.Context, content, workspace string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, summarizeTimeout)
	defer cancel()

	handle, err := s.serverMgr.GetOrStart(workspace)
	if err != nil {
		return "", fmt.Errorf("summarize: get server: %w", err)
	}

	sessionID, err := handle.CreateSession(timeoutCtx, "summary")
	if err != nil {
		return "", fmt.Errorf("summarize: create session: %w", err)
	}
	// Clean up the ephemeral session even if the parent context is cancelled.
	defer func() { _ = handle.DeleteSession(context.Background(), sessionID) }()

	prompt := summarizePrompt + content
	respBody, err := handle.SendPrompt(timeoutCtx, sessionID, prompt)
	if err != nil {
		return "", fmt.Errorf("summarize: send prompt: %w", err)
	}

	text, err := parseResponseText(respBody)
	if err != nil {
		return "", fmt.Errorf("summarize: parse response: %w", err)
	}
	return text, nil
}

// responseMessage mirrors the JSON structure returned by
// POST /session/{id}/message.
type responseMessage struct {
	Parts []responsePart `json:"parts"`
}

type responsePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// parseResponseText reads and parses the JSON response body from SendPrompt,
// concatenating all text parts into a single string.
func parseResponseText(r io.ReadCloser) (string, error) {
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	var msg responseMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	var b strings.Builder
	for _, p := range msg.Parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String(), nil
}

// fallbackSummary truncates content to maxLen runes, appending "..." if
// truncation is needed. Used by callers when AI summarization fails.
func fallbackSummary(content string, maxLen int) string {
	if utf8.RuneCountInString(content) <= maxLen {
		return content
	}
	runes := []rune(content)
	return string(runes[:maxLen-3]) + "..."
}
