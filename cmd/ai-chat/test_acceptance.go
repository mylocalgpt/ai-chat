package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mylocalgpt/ai-chat/pkg/app"
	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/config"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
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
	repoRoot, err := resolveAcceptanceTargetRepo()
	if err != nil {
		return err
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
	runtime := app.NewRuntimeWithRegistry(st, registry, app.RuntimeConfig{
		ResponsesDir:    responsesDir,
		SoftIdleTimeout: 30 * time.Minute,
		HardIdleTimeout: 2 * time.Hour,
		ReaperInterval:  5 * time.Minute,
	})

	adapter, err := telegram.NewTelegramAdapter(telegram.TelegramAdapterConfig{
		BotToken:     botToken,
		AllowedUsers: acceptanceAllowedUsers(cfg.Telegram.AllowedUsers, chatID),
	}, st)
	if err != nil {
		return fmt.Errorf("creating telegram adapter: %w", err)
	}
	testBot := newAcceptanceTelegramBot(chatID)
	adapter.SetBot(testBot)
	adapter.SetRouter(runtime.Router)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	wg := app.StartBackground(ctx, st, runtime.SessionManager, adapter)
	if err := adapter.Start(ctx); err != nil {
		cancel()
		wg.Wait()
		return fmt.Errorf("starting telegram adapter: %w", err)
	}
	defer func() {
		_ = adapter.Stop()
		cancel()
		wg.Wait()
	}()

	metadata, _ := json.Marshal(map[string]string{"default_agent": "mock"})
	ws, err := st.CreateWorkspace(ctx, "telegram-acceptance", repoRoot, "")
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
		if err := runtimeStep(ctx, adapter, testBot, senderID, chatID, step.content); err != nil {
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

func acceptanceAllowedUsers(allowedUsers []int64, chatID int64) []int64 {
	for _, allowedUser := range allowedUsers {
		if allowedUser == chatID {
			return append([]int64(nil), allowedUsers...)
		}
	}

	merged := make([]int64, 0, len(allowedUsers)+1)
	merged = append(merged, allowedUsers...)
	merged = append(merged, chatID)
	return merged
}

func resolveAcceptanceTargetRepo() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving acceptance repo root: %w", err)
	}
	root := wd
	for {
		if info, err := os.Stat(filepath.Join(root, ".git")); err == nil && info != nil {
			return root, nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", fmt.Errorf("resolving acceptance repo root: no git repository found from %s", wd)
		}
		root = parent
	}
}

func runtimeStep(ctx context.Context, adapter *telegram.TelegramAdapter, testBot *acceptanceTelegramBot, _ string, chatID int64, content string) error {
	beforeMessages := len(testBot.sentMessages)
	adapter.ProcessUpdate(ctx, &models.Update{Message: &models.Message{
		ID:   beforeMessages + 1,
		Date: int(time.Now().Unix()),
		From: &models.User{ID: chatID, FirstName: "Acceptance"},
		Chat: models.Chat{ID: chatID},
		Text: content,
	}})
	if len(testBot.sentMessages) == beforeMessages {
		return nil
	}
	last := testBot.sentMessages[len(testBot.sentMessages)-1]
	if kb, ok := last.ReplyMarkup.(*models.InlineKeyboardMarkup); ok && len(kb.InlineKeyboard) > 0 && len(kb.InlineKeyboard[0]) > 0 {
		callbackData := kb.InlineKeyboard[0][0].CallbackData
		adapter.ProcessCallback(ctx, &models.Update{CallbackQuery: &models.CallbackQuery{
			ID:   fmt.Sprintf("cb-%d", len(testBot.callbackAnswers)+1),
			From: models.User{ID: chatID, FirstName: "Acceptance"},
			Data: callbackData,
			Message: models.MaybeInaccessibleMessage{
				Type: models.MaybeInaccessibleMessageTypeMessage,
				Message: &models.Message{
					ID:   testBot.lastSentMessageID(),
					Date: int(time.Now().Unix()),
					Chat: models.Chat{ID: chatID},
				},
			},
		}})
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

type acceptanceTelegramBot struct {
	chatID          int64
	sentMessages    []*bot.SendMessageParams
	editedMessages  []*bot.EditMessageTextParams
	callbackAnswers []*bot.AnswerCallbackQueryParams
	chatActions     []*bot.SendChatActionParams
	commands        []*bot.SetMyCommandsParams
	nextMessageID   int
}

func newAcceptanceTelegramBot(chatID int64) *acceptanceTelegramBot {
	return &acceptanceTelegramBot{chatID: chatID, nextMessageID: 100}
}

func (b *acceptanceTelegramBot) GetMe(context.Context) (*models.User, error) {
	return &models.User{ID: 1, Username: "acceptance-bot", IsBot: true}, nil
}

func (b *acceptanceTelegramBot) Start(context.Context) {}

func (b *acceptanceTelegramBot) SendMessage(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	b.sentMessages = append(b.sentMessages, params)
	msg := &models.Message{ID: b.nextMessageID, Chat: models.Chat{ID: b.chatID}, Text: params.Text}
	b.nextMessageID++
	return msg, nil
}

func (b *acceptanceTelegramBot) SendChatAction(_ context.Context, params *bot.SendChatActionParams) (bool, error) {
	b.chatActions = append(b.chatActions, params)
	return true, nil
}

func (b *acceptanceTelegramBot) SetMyCommands(_ context.Context, params *bot.SetMyCommandsParams) (bool, error) {
	b.commands = append(b.commands, params)
	return true, nil
}

func (b *acceptanceTelegramBot) AnswerCallbackQuery(_ context.Context, params *bot.AnswerCallbackQueryParams) (bool, error) {
	b.callbackAnswers = append(b.callbackAnswers, params)
	return true, nil
}

func (b *acceptanceTelegramBot) EditMessageText(_ context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	b.editedMessages = append(b.editedMessages, params)
	return &models.Message{ID: params.MessageID, Chat: models.Chat{ID: b.chatID}, Text: params.Text}, nil
}

func (b *acceptanceTelegramBot) lastSentMessageID() int {
	return b.nextMessageID - 1
}
