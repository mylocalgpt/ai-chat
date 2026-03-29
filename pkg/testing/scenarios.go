package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mylocalgpt/ai-chat/pkg/channel/telegram"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/executor"
	"github.com/mylocalgpt/ai-chat/pkg/router"
)

type Scenario struct {
	Name     string
	Optional bool
	Run      func(t T, h *TestHarness)
}

var Scenarios = []Scenario{
	{Name: "Basic flow", Run: TestBasicFlow},
	{Name: "Slash commands", Run: TestSlashCommands},
	{Name: "Session lifecycle", Run: TestSessionLifecycle},
	{Name: "Security proxy", Run: TestSecurityProxy},
	{Name: "Formatting", Run: TestFormatting},
	{Name: "Session switching regression", Run: TestSessionSwitchingRegression},
	{Name: "Workspace continuity regression", Run: TestWorkspaceContinuityRegression},
	{Name: "Security confirmation regression", Run: TestSecurityConfirmationRegression},
	{Name: "MCP session targeting regression", Run: TestMCPSessionTargetingRegression},
	{Name: "telegram-acceptance", Optional: true, Run: TestTelegramAcceptance},
}

func DefaultScenarios() []Scenario {
	defaults := make([]Scenario, 0, len(Scenarios))
	for _, s := range Scenarios {
		if !s.Optional {
			defaults = append(defaults, s)
		}
	}
	return defaults
}

func TestBasicFlow(t T, h *TestHarness) {
	ctx := context.Background()

	t.Run("ping_pong", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "ping")
		if err != nil {
			t.Fatalf("sending ping: %v", err)
		}
		if resp != "pong" {
			t.Errorf("expected 'pong', got %q", resp)
		}
	})

	t.Run("echo", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "hello world")
		if err != nil {
			t.Fatalf("sending hello world: %v", err)
		}
		if resp != "Echo: hello world" {
			t.Errorf("expected 'Echo: hello world', got %q", resp)
		}
	})

	t.Run("long_response", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "long")
		if err != nil {
			t.Fatalf("sending long: %v", err)
		}
		if utf8.RuneCountInString(resp) < 5000 {
			t.Errorf("expected 5000+ chars, got %d", utf8.RuneCountInString(resp))
		}
	})

	t.Run("markdown_response", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "markdown")
		if err != nil {
			t.Fatalf("sending markdown: %v", err)
		}
		if !strings.Contains(resp, "# Header") {
			t.Error("expected markdown header")
		}
		if !strings.Contains(resp, "**bold**") {
			t.Error("expected bold markdown")
		}
		if !strings.Contains(resp, "```python") {
			t.Error("expected code fence")
		}
	})

	t.Run("response_files_exist", func(t T) {
		entries, err := os.ReadDir(h.TempDir + "/responses")
		if err != nil {
			t.Fatalf("reading responses dir: %v", err)
		}
		if len(entries) == 0 {
			t.Fatal("expected at least one response file")
		}

		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") {
				path := h.TempDir + "/responses/" + entry.Name()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("reading %s: %v", entry.Name(), err)
					continue
				}

				var rf executor.ResponseFile
				if err := json.Unmarshal(data, &rf); err != nil {
					t.Errorf("parsing %s: %v", entry.Name(), err)
					continue
				}

				if rf.Session == "" {
					t.Errorf("%s: empty session name", entry.Name())
				}
				if rf.Workspace == "" {
					t.Errorf("%s: empty workspace", entry.Name())
				}
				if rf.Agent == "" {
					t.Errorf("%s: empty agent", entry.Name())
				}
				if len(rf.Messages) == 0 {
					t.Errorf("%s: no messages", entry.Name())
				}
			}
		}
	})
}

func TestSlashCommands(t T, h *TestHarness) {
	ctx := context.Background()

	t.Run("status", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "/status")
		if err != nil {
			t.Fatalf("sending /status: %v", err)
		}
		if !strings.Contains(resp, "test-ws") {
			t.Errorf("expected workspace name 'test-ws' in response, got %q", resp)
		}
		if !strings.Contains(resp, "Agent:") {
			t.Errorf("expected agent info in response, got %q", resp)
		}
	})

	t.Run("workspaces", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "/workspaces")
		if err != nil {
			t.Fatalf("sending /workspaces: %v", err)
		}
		if !strings.Contains(resp, "test-ws") {
			t.Errorf("expected 'test-ws' in response, got %q", resp)
		}
	})

	t.Run("new", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "/new")
		if err != nil {
			t.Fatalf("sending /new: %v", err)
		}
		if !strings.Contains(resp, "Created session") && !strings.Contains(resp, "unknown agent") {
			t.Errorf("expected 'Created session' or 'unknown agent' in response, got %q", resp)
		}
	})

	t.Run("sessions", func(t T) {
		_, _ = h.SendMessage(ctx, "test-sender", "first message")
		_, _ = h.SendMessage(ctx, "test-sender", "second message")

		resp, err := h.SendMessage(ctx, "test-sender", "/sessions")
		if err != nil {
			t.Fatalf("sending /sessions: %v", err)
		}

		if resp == "No sessions in current workspace." {
			t.Error("expected sessions to be listed")
		}
	})

	t.Run("clear", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "/clear")
		if err != nil {
			t.Fatalf("sending /clear: %v", err)
		}
		if !strings.Contains(resp, "Cleared session") {
			t.Errorf("expected 'Cleared session' in response, got %q", resp)
		}
	})
}

func TestSessionLifecycle(t T, h *TestHarness) {
	ctx := context.Background()

	t.Run("first_message_creates_session", func(t T) {
		_, err := h.SendMessage(ctx, "test-sender", "first message")
		if err != nil {
			t.Fatalf("sending first message: %v", err)
		}

		active, err := h.Store.GetActiveWorkspace(ctx, "test-sender", "test")
		if err != nil {
			t.Fatalf("getting active workspace: %v", err)
		}
		activeSession, err := h.Store.GetActiveSessionForWorkspace(ctx, "test-sender", "test", active.WorkspaceID)
		if err != nil {
			t.Fatalf("getting active session: %v", err)
		}

		sess, err := h.Store.GetSessionByID(ctx, activeSession.SessionID)
		if err != nil {
			t.Fatalf("getting session: %v", err)
		}
		if sess.Status != "active" {
			t.Errorf("expected status 'active', got %q", sess.Status)
		}
	})

	t.Run("second_message_reuses_session", func(t T) {
		active, _ := h.Store.GetActiveWorkspace(ctx, "test-sender", "test")
		activeSession1, _ := h.Store.GetActiveSessionForWorkspace(ctx, "test-sender", "test", active.WorkspaceID)
		sessionID1 := activeSession1.SessionID

		_, err := h.SendMessage(ctx, "test-sender", "second message")
		if err != nil {
			t.Fatalf("sending second message: %v", err)
		}

		activeSession2, err := h.Store.GetActiveSessionForWorkspace(ctx, "test-sender", "test", active.WorkspaceID)
		if err != nil {
			t.Fatalf("getting active session: %v", err)
		}
		if activeSession2.SessionID != sessionID1 {
			t.Errorf("expected same session ID, got %d vs %d", sessionID1, activeSession2.SessionID)
		}
	})

	t.Run("clear_creates_new_session", func(t T) {
		active, _ := h.Store.GetActiveWorkspace(ctx, "test-sender", "test")
		activeSession1, _ := h.Store.GetActiveSessionForWorkspace(ctx, "test-sender", "test", active.WorkspaceID)
		oldSessionID := activeSession1.SessionID

		_, err := h.SendMessage(ctx, "test-sender", "/clear")
		if err != nil {
			t.Fatalf("sending /clear: %v", err)
		}

		activeSession2, _ := h.Store.GetActiveSessionForWorkspace(ctx, "test-sender", "test", active.WorkspaceID)
		if activeSession2.SessionID == oldSessionID {
			t.Error("expected new session ID after clear")
		}

		oldSess, err := h.Store.GetSessionByID(ctx, oldSessionID)
		if err != nil {
			t.Fatalf("getting old session: %v", err)
		}
		if oldSess.Status != "expired" {
			t.Errorf("expected old session status 'expired', got %q", oldSess.Status)
		}
	})

	t.Run("third_message_uses_new_session", func(t T) {
		active, _ := h.Store.GetActiveWorkspace(ctx, "test-sender", "test")
		activeSession1, _ := h.Store.GetActiveSessionForWorkspace(ctx, "test-sender", "test", active.WorkspaceID)
		sessionID1 := activeSession1.SessionID

		_, err := h.SendMessage(ctx, "test-sender", "third message")
		if err != nil {
			t.Fatalf("sending third message: %v", err)
		}

		activeSession2, _ := h.Store.GetActiveSessionForWorkspace(ctx, "test-sender", "test", active.WorkspaceID)
		if activeSession2.SessionID != sessionID1 {
			t.Errorf("expected same session ID after clear, got %d vs %d", sessionID1, activeSession2.SessionID)
		}
	})

	t.Run("clear_preserves_agent", func(t T) {
		if _, err := h.SendMessage(ctx, "test-sender", "/agent opencode"); err != nil {
			t.Fatalf("setting agent: %v", err)
		}
		if _, err := h.SendMessage(ctx, "test-sender", "hello with opencode"); err != nil {
			t.Fatalf("sending opencode message: %v", err)
		}
		before, err := h.ActiveSession(ctx, "test-sender", "test")
		if err != nil {
			t.Fatalf("getting active session before clear: %v", err)
		}
		if before.Agent != "opencode" {
			t.Fatalf("expected active agent opencode before clear, got %q", before.Agent)
		}
		if _, err := h.SendMessage(ctx, "test-sender", "/clear"); err != nil {
			t.Fatalf("clearing session: %v", err)
		}
		after, err := h.ActiveSession(ctx, "test-sender", "test")
		if err != nil {
			t.Fatalf("getting active session after clear: %v", err)
		}
		if after.ID == before.ID {
			t.Fatal("expected /clear to create a new session")
		}
		if after.Agent != "opencode" {
			t.Fatalf("expected /clear to preserve agent opencode, got %q", after.Agent)
		}
	})

	t.Run("sessions_listed_with_previews", func(t T) {
		resp, err := h.SendMessage(ctx, "test-sender", "/sessions")
		if err != nil {
			t.Fatalf("sending /sessions: %v", err)
		}

		if resp == "No sessions in current workspace." {
			t.Error("expected sessions to be listed")
		}
	})
}

func TestSessionSwitchingRegression(t T, h *TestHarness) {
	ctx := context.Background()

	ws, err := h.Store.GetWorkspace(ctx, "test-ws")
	if err != nil {
		t.Fatalf("getting default workspace: %v", err)
	}
	if _, err := h.SendMessage(ctx, "test-sender", "first session"); err != nil {
		t.Fatalf("sending first message: %v", err)
	}
	first, err := h.ActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting first session: %v", err)
	}
	if _, err := h.SendMessage(ctx, "test-sender", "/new"); err != nil {
		t.Fatalf("creating second session: %v", err)
	}
	second, err := h.ActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting second session: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected /new to activate a different session")
	}
	if _, err := h.Router.HandleSessionSelection(ctx, "test-sender", "test", ws.ID, first.ID); err != nil {
		t.Fatalf("switching back to first session: %v", err)
	}
	active, err := h.ActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting active session after switch: %v", err)
	}
	if active.ID != first.ID {
		t.Fatalf("expected session %d after switch, got %d", first.ID, active.ID)
	}
	responseFile, err := h.ResponseFileForActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting response file: %v", err)
	}
	if _, err := h.SendMessage(ctx, "test-sender", "back on first"); err != nil {
		t.Fatalf("sending post-switch message: %v", err)
	}
	latest, err := executor.LatestAgentMessage(responseFile)
	if err != nil {
		t.Fatalf("reading first session response file: %v", err)
	}
	if latest != "Echo: back on first" {
		t.Fatalf("expected response in switched session file, got %q", latest)
	}
}

func TestWorkspaceContinuityRegression(t T, h *TestHarness) {
	ctx := context.Background()
	alpha, err := h.Store.GetWorkspace(ctx, "test-ws")
	if err != nil {
		t.Fatalf("getting default workspace: %v", err)
	}
	beta, err := h.CreateWorkspace(ctx, "beta")
	if err != nil {
		t.Fatalf("creating second workspace: %v", err)
	}
	if _, err := h.SendMessage(ctx, "test-sender", "alpha-one"); err != nil {
		t.Fatalf("sending alpha message: %v", err)
	}
	alphaSession, err := h.ActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting alpha session: %v", err)
	}
	if _, err := h.Router.HandleWorkspaceSelection(ctx, "test-sender", "test", beta.ID); err != nil {
		t.Fatalf("switching to beta: %v", err)
	}
	if _, err := h.SendMessage(ctx, "test-sender", "beta-one"); err != nil {
		t.Fatalf("sending beta message: %v", err)
	}
	betaSession, err := h.ActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting beta session: %v", err)
	}
	if betaSession.WorkspaceID != beta.ID {
		t.Fatalf("expected beta session to belong to workspace %d, got %d", beta.ID, betaSession.WorkspaceID)
	}
	if _, err := h.Router.HandleWorkspaceSelection(ctx, "test-sender", "test", alpha.ID); err != nil {
		t.Fatalf("switching back to alpha: %v", err)
	}
	restored, err := h.ActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting restored alpha session: %v", err)
	}
	if restored.ID != alphaSession.ID {
		t.Fatalf("expected workspace switch to restore alpha session %d, got %d", alphaSession.ID, restored.ID)
	}
}

func TestSecurityConfirmationRegression(t T, h *TestHarness) {
	ctx := context.Background()
	baseline := h.Outbound.Count()
	result, err := h.Router.Route(ctx, router.Request{Message: &core.InboundMessage{SenderID: "test-sender", Channel: "test", Content: "my password is hunter2"}})
	if err != nil {
		t.Fatalf("routing confirm-required message: %v", err)
	}
	if result.Kind != router.ResultSecurityConfirmation || result.SecurityConfirmation == nil {
		t.Fatalf("expected security confirmation, got %#v", result)
	}
	if _, err := h.ApproveSecurity(ctx, "test-sender", result.SecurityConfirmation.Token, false); err != nil {
		t.Fatalf("rejecting security confirmation: %v", err)
	}
	if h.Outbound.Count() != baseline {
		t.Fatalf("expected cancelled security confirmation not to add outbound messages, count changed from %d to %d", baseline, h.Outbound.Count())
	}

	result, err = h.Router.Route(ctx, router.Request{Message: &core.InboundMessage{SenderID: "test-sender", Channel: "test", Content: "please use password reset flow"}})
	if err != nil {
		t.Fatalf("routing second confirm-required message: %v", err)
	}
	if result.Kind != router.ResultSecurityConfirmation || result.SecurityConfirmation == nil {
		t.Fatalf("expected second security confirmation, got %#v", result)
	}
	responseFile, err := h.ResponseFileForActiveSession(ctx, "test-sender", "test")
	if err != nil {
		t.Fatalf("getting response file for approved message: %v", err)
	}
	before := h.Outbound.Count()
	if _, err := h.ApproveSecurity(ctx, "test-sender", result.SecurityConfirmation.Token, true); err != nil {
		t.Fatalf("approving security confirmation: %v", err)
	}
	content, err := h.Outbound.WaitForContent(context.Background(), before)
	if err != nil {
		t.Fatalf("waiting for approved outbound content: %v", err)
	}
	if content != "Response blocked because it may contain sensitive content." {
		t.Fatalf("expected sanitized outbound response after approval, got %q", content)
	}
	latest, err := executor.LatestAgentMessage(responseFile)
	if err != nil {
		t.Fatalf("reading approved response file: %v", err)
	}
	if latest != "Echo: please use password reset flow" {
		t.Fatalf("expected approved message to reach agent, got %q", latest)
	}
}

func TestMCPSessionTargetingRegression(t T, h *TestHarness) {
	ctx := context.Background()
	alpha, err := h.Store.GetWorkspace(ctx, "test-ws")
	if err != nil {
		t.Fatalf("getting alpha workspace: %v", err)
	}
	beta, err := h.CreateWorkspace(ctx, "beta-mcp")
	if err != nil {
		t.Fatalf("creating beta workspace: %v", err)
	}
	alphaSession, err := h.MCP.CreateSession(ctx, *alpha, "mock")
	if err != nil {
		t.Fatalf("creating alpha session: %v", err)
	}
	betaSession, err := h.MCP.CreateSession(ctx, *beta, "mock")
	if err != nil {
		t.Fatalf("creating beta session: %v", err)
	}
	if _, err := h.MCP.SwitchSession(ctx, alpha.ID, alphaSession.ID); err != nil {
		t.Fatalf("switching active MCP session: %v", err)
	}
	if err := h.MCP.Send(ctx, betaSession.ID, "ping"); err != nil {
		t.Fatalf("sending to explicit beta session: %v", err)
	}
	betaFile := executor.ResponseFilePath(h.TempDir+"/responses", betaSession.TmuxSession)
	betaLatest, err := executor.LatestAgentMessage(betaFile)
	if err != nil {
		t.Fatalf("reading beta response file: %v", err)
	}
	if betaLatest != "pong" {
		t.Fatalf("expected explicit MCP send to target beta session, got %q", betaLatest)
	}
	alphaFile := executor.ResponseFilePath(h.TempDir+"/responses", alphaSession.TmuxSession)
	alphaLatest, err := executor.LatestAgentMessage(alphaFile)
	if err == nil && alphaLatest == "pong" {
		t.Fatal("expected explicit MCP send not to land in active alpha session")
	}
}

func TestTelegramAcceptance(t T, h *TestHarness) {
	t.Fatal("telegram acceptance requires explicit runtime configuration and is only supported via `ai-chat test --scenario telegram-acceptance`")
}

func TestSecurityProxy(t T, h *TestHarness) {
	t.Run("password_keyword", func(t T) {
		flags := h.Proxy.Scan("my password is hunter2")
		if len(flags) == 0 {
			t.Error("expected security flags for password keyword")
		}
		found := false
		for _, f := range flags {
			if f.Keyword == "password" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected 'password' keyword flag")
		}
	})

	t.Run("api_key_prefix", func(t T) {
		flags := h.Proxy.Scan("here is sk-test123456 for the api")
		if len(flags) == 0 {
			t.Error("expected security flags for sk- prefix")
		}
		found := false
		for _, f := range flags {
			if f.Keyword == "sk-" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected 'sk-' prefix flag")
		}
	})

	t.Run("normal_message_no_flags", func(t T) {
		flags := h.Proxy.Scan("hello world this is a test")
		if len(flags) > 0 {
			t.Errorf("expected no flags for normal message, got %d: %v", len(flags), flags)
		}
	})

	t.Run("aws_key_prefix", func(t T) {
		flags := h.Proxy.Scan("set AKIA12345 as the key")
		if len(flags) == 0 {
			t.Error("expected security flags for AKIA prefix")
		}
		found := false
		for _, f := range flags {
			if f.Keyword == "AKIA" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected 'AKIA' prefix flag")
		}
	})
}

func TestFormatting(t T, h *TestHarness) {
	t.Run("bold_conversion", func(t T) {
		input := "**bold text**"
		output := telegram.FormatHTML(input)
		if !strings.Contains(output, "<b>bold text</b>") {
			t.Errorf("expected bold HTML, got %q", output)
		}
	})

	t.Run("code_fence_conversion", func(t T) {
		input := "```python\nprint('hello')\n```"
		output := telegram.FormatHTML(input)
		if !strings.Contains(output, "<pre><code>") {
			t.Errorf("expected pre/code tags, got %q", output)
		}
		if !strings.Contains(output, "print('hello')") {
			t.Errorf("expected code content, got %q", output)
		}
	})

	t.Run("inline_code_conversion", func(t T) {
		input := "use `code` here"
		output := telegram.FormatHTML(input)
		if !strings.Contains(output, "<code>code</code>") {
			t.Errorf("expected code tags, got %q", output)
		}
	})

	t.Run("header_conversion", func(t T) {
		input := "# Header"
		output := telegram.FormatHTML(input)
		if !strings.Contains(output, "<b>Header</b>") {
			t.Errorf("expected bold header, got %q", output)
		}
	})

	t.Run("link_conversion", func(t T) {
		input := "[click here](https://example.com)"
		output := telegram.FormatHTML(input)
		if !strings.Contains(output, "<a href=\"https://example.com\">click here</a>") {
			t.Errorf("expected anchor tag, got %q", output)
		}
	})

	t.Run("html_entity_escaping", func(t T) {
		input := "a < b & c > d"
		output := telegram.FormatHTML(input)
		if !strings.Contains(output, "&lt;") || !strings.Contains(output, "&gt;") || !strings.Contains(output, "&amp;") {
			t.Errorf("expected HTML entities, got %q", output)
		}
	})

	t.Run("message_splitting_under_limit", func(t T) {
		text := strings.Repeat("a", 3000)
		chunks := telegram.SplitMessage(text, 4000)
		if len(chunks) != 1 {
			t.Errorf("expected 1 chunk for text under limit, got %d", len(chunks))
		}
	})

	t.Run("message_splitting_over_limit", func(t T) {
		text := strings.Repeat("a", 3000) + "\n\n" + strings.Repeat("b", 2000)
		chunks := telegram.SplitMessage(text, 4000)
		if len(chunks) < 2 {
			t.Errorf("expected 2+ chunks for text over limit, got %d", len(chunks))
		}
		for i, chunk := range chunks {
			if utf8.RuneCountInString(chunk) > 4000 {
				t.Errorf("chunk %d exceeds limit: %d runes", i, utf8.RuneCountInString(chunk))
			}
		}
	})

	t.Run("code_fence_aware_splitting", func(t T) {
		codeBlock := "```python\n" + strings.Repeat("x", 5000) + "\n```"
		chunks := telegram.SplitMessage(codeBlock, 4000)

		for i, chunk := range chunks {
			if strings.Contains(chunk, "<pre><code>") {
				if !strings.HasSuffix(chunk, "</code></pre>") {
					t.Errorf("chunk %d has unclosed code fence", i)
				}
			}
			if strings.HasPrefix(chunk, "<pre><code>") {
				if !strings.HasSuffix(chunk, "</code></pre>") {
					t.Errorf("chunk %d has unclosed code fence", i)
				}
			}
		}
	})
}

func RunScenario(t T, h *TestHarness, name string) bool {
	for _, s := range Scenarios {
		if s.Name == name {
			start := time.Now()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("scenario %s panicked: %v", name, r)
				}
				dur := time.Since(start)
				t.Logf("  %s: %s (%.1fs)", name, passFail(!t.Failed()), dur.Seconds())
			}()
			s.Run(t, h)
			return !t.Failed()
		}
	}
	t.Errorf("unknown scenario: %s", name)
	return false
}

func passFail(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

func RunAllScenarios(t T, h *TestHarness) map[string]bool {
	results := make(map[string]bool)
	for _, s := range DefaultScenarios() {
		t.Run(strings.ToLower(strings.ReplaceAll(s.Name, " ", "_")), func(t T) {
			s.Run(t, h)
		})
		results[s.Name] = !t.Failed()
	}
	return results
}

func FormatReport(results map[string]bool, total time.Duration) string {
	var b strings.Builder
	b.WriteString("ai-chat test results:\n")

	passed := 0
	for _, s := range DefaultScenarios() {
		status := "FAIL"
		if results[s.Name] {
			status = "PASS"
			passed++
		}
		fmt.Fprintf(&b, "  %-18s %s\n", s.Name+":", status)
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "%d/%d passed (%.1fs total)\n", passed, len(DefaultScenarios()), total.Seconds())
	return b.String()
}
