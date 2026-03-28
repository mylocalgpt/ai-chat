package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mylocalgpt/ai-chat/pkg/core"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxToolIterations = 5

const orchestratorSystemPrompt = `You are an AI assistant that manages workspaces and AI agent sessions. Use the available tools for workspace, session, and agent operations. For simple greetings or questions you can answer directly, respond without tools.`

// Orchestrator routes inbound messages through a tool-calling loop backed by
// an in-process MCP session.
type Orchestrator struct {
	router     *Router
	mcpSession *gomcp.ClientSession
	model      string
	tools      []ToolDef
}

// NewOrchestrator creates an Orchestrator that uses the given router and MCP
// session. Call Init before HandleMessage.
func NewOrchestrator(router *Router, mcpSession *gomcp.ClientSession, model string) *Orchestrator {
	return &Orchestrator{
		router:     router,
		mcpSession: mcpSession,
		model:      model,
	}
}

// Init lists the tools from the MCP session and converts them to OpenAI
// function definitions for the router.
func (o *Orchestrator) Init(ctx context.Context) error {
	result, err := o.mcpSession.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("orchestrator: listing tools: %w", err)
	}

	o.tools = make([]ToolDef, len(result.Tools))
	for i, tool := range result.Tools {
		params, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return fmt.Errorf("orchestrator: marshaling schema for %q: %w", tool.Name, err)
		}
		o.tools[i] = ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		}
	}

	slog.Info("orchestrator initialized", "tools", len(o.tools))
	return nil
}

// HandleMessage is the single entry point called by channel adapters. It runs
// a tool-calling loop where the LLM can invoke MCP tools until it produces a
// final text response.
func (o *Orchestrator) HandleMessage(ctx context.Context, msg core.InboundMessage, userContext string) (string, error) {
	systemContent := userContext + "\n\n" + orchestratorSystemPrompt

	messages := []any{
		Message{Role: "system", Content: systemContent},
		Message{Role: "user", Content: msg.Content},
	}

	for i := 0; i < maxToolIterations; i++ {
		resp, err := o.router.CompleteWithTools(ctx, o.model, messages, o.tools)
		if err != nil {
			return "", fmt.Errorf("orchestrator: completion: %w", err)
		}

		choice := resp.Choices[0]

		// If the model finished without tool calls, return the text response.
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			return choice.Message.Content, nil
		}

		// Append the assistant message with tool calls.
		content := choice.Message.Content
		var contentPtr *string
		if content != "" {
			contentPtr = &content
		}
		messages = append(messages, AssistantMessage{
			Role:      "assistant",
			Content:   contentPtr,
			ToolCalls: choice.Message.ToolCalls,
		})

		// Execute each tool call via MCP.
		for _, tc := range choice.Message.ToolCalls {
			var argsMap map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &argsMap); err != nil {
				messages = append(messages, ToolResultMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error parsing arguments: %v", err),
				})
				continue
			}

			toolResult, err := o.mcpSession.CallTool(ctx, &gomcp.CallToolParams{
				Name:      tc.Function.Name,
				Arguments: argsMap,
			})
			if err != nil {
				messages = append(messages, ToolResultMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("tool error: %v", err),
				})
				continue
			}

			// Extract text from tool result content.
			var text string
			for _, c := range toolResult.Content {
				if tc, ok := c.(*gomcp.TextContent); ok {
					text += tc.Text
				}
			}

			messages = append(messages, ToolResultMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    text,
			})
		}
	}

	return "I wasn't able to complete that request.", nil
}
