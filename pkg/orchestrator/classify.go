package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

const systemPrompt = `You are a message router for an AI assistant system. Classify the user's intent and respond with JSON only.

Action types:
- "workspace_switch": user wants to switch to a different workspace
- "agent_task": user wants an AI agent to do work (code, research, etc.). Set "workspace" to the active workspace name if one exists.
- "status": user is asking about current state or system status
- "direct_answer": simple question you can answer directly. Put your answer in "content". Never echo the user's message back.
- "meta_command": system command (list workspaces, help, etc.)

If no workspaces exist and the user asks for work that needs one, use "direct_answer" with "content" explaining they need to create a workspace first (via MCP or the API).

Respond with exactly this JSON structure:
{"type": "<action_type>", "workspace": "<name or empty>", "agent": "<name or empty>", "content": "<your response or forwarded message>", "confidence": <0.0-1.0>, "reasoning": "<brief explanation>"}`

// buildClassifyPrompt constructs the system+user prompt for intent classification.
func buildClassifyPrompt(msg core.InboundMessage, userCtx UserContext, workspaces []core.Workspace) []Message {
	var userMsg strings.Builder

	userMsg.WriteString("Current workspace: ")
	if userCtx.ActiveWorkspace != nil {
		userMsg.WriteString(userCtx.ActiveWorkspace.Name)
		userMsg.WriteString(" (")
		userMsg.WriteString(userCtx.ActiveWorkspace.Path)
		userMsg.WriteString(")")
	} else {
		userMsg.WriteString("none")
	}
	userMsg.WriteString("\n")

	userMsg.WriteString("Available workspaces: ")
	if len(workspaces) == 0 {
		userMsg.WriteString("none")
	} else {
		parts := make([]string, len(workspaces))
		for i, w := range workspaces {
			parts[i] = w.Name + ": " + w.Path
		}
		userMsg.WriteString(strings.Join(parts, ", "))
	}
	userMsg.WriteString("\n\n")

	userMsg.WriteString("Message: ")
	userMsg.WriteString(msg.Content)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg.String()},
	}
}

// classifyIntent sends the classification prompt to a specific model via the
// router and parses the JSON response into an Action.
func classifyIntent(ctx context.Context, router *Router, model string, msg core.InboundMessage, userCtx UserContext, workspaces []core.Workspace) (Action, error) {
	messages := buildClassifyPrompt(msg, userCtx, workspaces)

	raw, err := router.Complete(ctx, model, messages)
	if err != nil {
		return Action{}, fmt.Errorf("classify: %w", err)
	}

	if raw == "" {
		return Action{}, fmt.Errorf("classify: empty response from model")
	}

	cleaned := stripCodeFences(raw)

	var action Action
	if err := json.Unmarshal([]byte(cleaned), &action); err != nil {
		snippet := cleaned
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return Action{}, fmt.Errorf("classify: invalid json: %w (raw: %s)", err, snippet)
	}

	if !validActionTypes[action.Type] {
		return Action{}, fmt.Errorf("classify: unknown action type %q", action.Type)
	}

	return action, nil
}

// stripCodeFences removes markdown code fence wrappers from a string.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}
