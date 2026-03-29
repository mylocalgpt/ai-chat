package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/app"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
	"github.com/mylocalgpt/ai-chat/pkg/store"
	testingpkg "github.com/mylocalgpt/ai-chat/pkg/testing"
)

func runTelegramAcceptance(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	botToken, chatID, err := cfg.TelegramAcceptance()
	if err != nil {
		return fmt.Errorf("telegram acceptance configuration error: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "ai-chat-telegram-acceptance-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dbPath := filepath.Join(tempDir, "acceptance.db")
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening acceptance database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db); err != nil {
		return fmt.Errorf("migrating acceptance database: %w", err)
	}
	st := store.New(db)

	responsesDir := filepath.Join(tempDir, "responses")
	if err := os.MkdirAll(responsesDir, 0o755); err != nil {
		return fmt.Errorf("creating responses dir: %w", err)
	}

	registry := &acceptanceRegistry{mock: testingMockAdapter()}
	runtime := app.NewRuntime(st, registry, app.RuntimeConfig{
		ResponsesDir:    responsesDir,
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		ReaperInterval:  5 * time.Minute,
	})

	adapter, err := telegram.NewTelegramAdapter(telegram.TelegramAdapterConfig{
		BotToken:     botToken,
		AllowedUsers: cfg.Telegram.AllowedUsers,
	}, st)
	if err != nil {
		return fmt.Errorf("creating telegram adapter: %w", err)
	}
	adapter.SetRouter(runtime.Router)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	wg := app.StartBackground(ctx, st, runtime.SessionManager, adapter)
	defer wg.Wait()
	if err := adapter.Start(ctx); err != nil {
		return fmt.Errorf("starting telegram adapter: %w", err)
	}
	defer func() { _ = adapter.Stop() }()

	metadata, _ := json.Marshal(map[string]string{"default_agent": "mock"})
	ws, err := st.CreateWorkspace(ctx, "telegram-acceptance", tempDir, "")
	if err != nil {
		return fmt.Errorf("creating acceptance workspace: %w", err)
	}
	if err := st.UpdateWorkspaceMetadata(ctx, ws.ID, metadata); err != nil {
		return fmt.Errorf("updating acceptance workspace metadata: %w", err)
	}
	senderID := fmt.Sprintf("%d", chatID)
	if err := st.SetActiveWorkspace(ctx, senderID, "telegram", ws.ID); err != nil {
		return fmt.Errorf("setting acceptance active workspace: %w", err)
	}

	steps := []struct {
		name    string
		content string
		assert  func(context.Context, *store.Store, string) error
	}{
		{name: "workspace status", content: "/status", assert: ensureActiveWorkspace("telegram-acceptance")},
		{name: "session creation", content: "/new", assert: ensureActiveSession()},
		{name: "formatted response", content: "markdown", assert: ensureLatestAgentMessage(responsesDir, "# Header")},
		{name: "security confirmation", content: "please use password reset flow", assert: ensureLatestAgentMessage(responsesDir, "Echo: please use password reset flow")},
	}

	for _, step := range steps {
		if err := runtimeStep(ctx, runtime, st, senderID, step.content); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
		if err := step.assert(ctx, st, senderID); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	msg := core.OutboundMessage{
		Channel:     "telegram",
		RecipientID: senderID,
		Content:     strings.Repeat("```go\nfmt.Println(\"chunk\")\n```\n\n", 200),
	}
	if err := adapter.Send(ctx, msg); err != nil {
		return fmt.Errorf("sending multi-chunk telegram message: %w", err)
	}

	return nil
}

func runtimeStep(ctx context.Context, runtime *app.Runtime, st *store.Store, senderID, content string) error {
	result, err := runtime.Router.Route(ctx, router.Request{Message: &core.InboundMessage{SenderID: senderID, Channel: "telegram", Content: content}})
	if err != nil {
		return err
	}
	if result.Kind == router.ResultText && result.Text != "" {
		if err := st.CreateMessage(ctx, &core.Message{Channel: "telegram", SenderID: senderID, Content: result.Text, Direction: core.OutboundDirection, Status: core.StatusDone}); err != nil {
			return err
		}
	}
	if result.Kind == router.ResultSecurityConfirmation && result.SecurityConfirmation != nil {
		_, err := runtime.SessionManager.HandleSecurityDecision(ctx, senderID, "telegram", result.SecurityConfirmation.Token, true)
		return err
	}
	return nil
}

func ensureActiveWorkspace(name string) func(context.Context, *store.Store, string) error {
	return func(ctx context.Context, st *store.Store, senderID string) error {
		active, err := st.GetActiveWorkspace(ctx, senderID, "telegram")
		if err != nil {
			return err
		}
		ws, err := st.GetWorkspaceByID(ctx, active.WorkspaceID)
		if err != nil {
			return err
		}
		if ws.Name != name {
			return fmt.Errorf("active workspace = %q, want %q", ws.Name, name)
		}
		return nil
	}
}

func ensureActiveSession() func(context.Context, *store.Store, string) error {
	return func(ctx context.Context, st *store.Store, senderID string) error {
		active, err := st.GetActiveWorkspace(ctx, senderID, "telegram")
		if err != nil {
			return err
		}
		_, err = st.GetActiveSessionForWorkspace(ctx, senderID, "telegram", active.WorkspaceID)
		return err
	}
}

func ensureLatestAgentMessage(responsesDir, contains string) func(context.Context, *store.Store, string) error {
	return func(ctx context.Context, st *store.Store, senderID string) error {
		active, err := st.GetActiveWorkspace(ctx, senderID, "telegram")
		if err != nil {
			return err
		}
		activeSession, err := st.GetActiveSessionForWorkspace(ctx, senderID, "telegram", active.WorkspaceID)
		if err != nil {
			return err
		}
		sess, err := st.GetSessionByID(ctx, activeSession.SessionID)
		if err != nil {
			return err
		}
		latest, err := executor.LatestAgentMessage(executor.ResponseFilePath(responsesDir, sess.TmuxSession))
		if err != nil {
			return err
		}
		if !strings.Contains(latest, contains) {
			return fmt.Errorf("latest agent message %q does not contain %q", latest, contains)
		}
		return nil
	}
}

type acceptanceRegistry struct {
	mock executor.AgentAdapter
}

func (r *acceptanceRegistry) GetAdapter(agent string) (executor.AgentAdapter, error) {
	if agent == "mock" {
		return r.mock, nil
	}
	return nil, fmt.Errorf("unknown agent: %q", agent)
}

func (r *acceptanceRegistry) KnownAgents() []string {
	return []string{"mock"}
}

func testingMockAdapter() executor.AgentAdapter {
	adapter := testingpkg.NewMockAdapter("mock")
	adapter.AddResponse("please use password reset flow", testingpkg.MockResponse{Content: "Echo: please use password reset flow"})
	return adapter
}
