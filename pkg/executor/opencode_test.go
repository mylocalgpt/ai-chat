package executor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

func TestOpenCodeDetectStatus(t *testing.T) {
	a := NewOpenCodeTmuxAdapter(NewTmux(), nil)

	tests := []struct {
		name      string
		output    string
		wantState AgentState
	}{
		{
			"ready with prompt",
			"Some response text\n> \n",
			AgentReady,
		},
		{
			"ready with ask anything",
			"Welcome to OpenCode\nAsk anything...",
			AgentReady,
		},
		{
			"rate limited",
			"Model error: rate limit exceeded for gpt-4",
			AgentRateLimited,
		},
		{
			"rate limited too many",
			"Too many requests. Please wait.",
			AgentRateLimited,
		},
		{
			"crashed fatal",
			"fatal: unexpected error in rendering",
			AgentCrashed,
		},
		{
			"crashed panic",
			"panic: runtime error: index out of range",
			AgentCrashed,
		},
		{
			"crashed segfault",
			"caught segfault signal",
			AgentCrashed,
		},
		{
			"normal response with error word",
			"The error you're seeing is caused by a missing import.\nHere's how to fix it.",
			AgentWorking,
		},
		{
			"working",
			"Generating response...\nThinking...",
			AgentWorking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := a.DetectStatus(tt.output)
			if status.State != tt.wantState {
				t.Errorf("DetectStatus() state = %q, want %q (detail: %s)", status.State, tt.wantState, status.Detail)
			}
		})
	}
}

func TestExtractOpenCodeResponseIgnoresStartupChrome(t *testing.T) {
	pane := `▄
                                             █▀▀█ █▀▀█ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█ █▀▀█ █▀▀█
                                             █  █ █  █ █▀▀▀ █  █ █    █  █ █  █ █▀▀▀
                                             ▀▀▀▀ █▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀


                           ┃
                           ┃  Ask anything... "What is the tech stack of this project?"
                           ┃
                           ┃  Build  GPT-5.4 GitHub Copilot · high
                           ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
                                                                           tab agents  ctrl+p commands



                                  ● Tip Press F2 to quickly switch between recently used models

















  ~/code/peteretelej/research-lab:main                                                                                   1.3.5`

	if got := extractOpenCodeResponse(pane); got != "" {
		t.Fatalf("extractOpenCodeResponse() = %q, want empty", got)
	}
}

func TestExtractOpenCodeResponseReturnsAssistantTextOnly(t *testing.T) {
	pane := `                                                                                   █
  ┃                                                                                █    Greeting: Hello
  ┃  Hello                                                                         █
  ┃                                                                                █    Context
                                                                                   █    22,275 tokens
     Hello                                                                         █    6% used
                                                                                   █    $0.00 spent
     ▣  Build · gpt-5.4 · 2.0s                                                     █
                                                                                   █    ▼ MCP
                                                                                   █    • context7 Connected
                                                                                   █    • diffchunk Connected
                                                                                   █    • largefile Connected
                                                                                   █    • local-docs Connected
                                                                                   █
                                                                                   █    LSP
                                                                                   █    LSPs will activate as files are read

  ┃
  ┃
  ┃
  ┃  Build  GPT-5.4 GitHub Copilot · high                                               ~/code/peteretelej/research-lab:main
  ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
                                                         22.3K (6%)  ctrl+p commands    • OpenCode 1.3.5`

	if got := extractOpenCodeResponse(pane); got != "Hello" {
		t.Fatalf("extractOpenCodeResponse() = %q, want %q", got, "Hello")
	}
}

type stagedTmux struct {
	stages []string
	idx    int
	alive  bool
}

func (m *stagedTmux) NewSession(name, workDir string) error {
	m.alive = true
	return nil
}

func (m *stagedTmux) HasSession(name string) bool {
	return m.alive
}

func (m *stagedTmux) KillSession(name string) error {
	m.alive = false
	return nil
}

func (m *stagedTmux) ListSessions() ([]string, error) {
	if !m.alive {
		return nil, nil
	}
	return []string{"test-session"}, nil
}

func (m *stagedTmux) SendKeys(session, text string) error {
	return nil
}

func (m *stagedTmux) CapturePaneRaw(session string, lines int) (string, error) {
	if len(m.stages) == 0 {
		return "", nil
	}
	if m.idx >= len(m.stages) {
		return m.stages[len(m.stages)-1], nil
	}
	out := m.stages[m.idx]
	m.idx++
	return out, nil
}

func TestOpenCodeSendWaitsForPromptEcho(t *testing.T) {
	tmx := &stagedTmux{stages: []string{
		"snapshot",
		`● Tip OpenCode uses LSP servers for intelligent`,
		"⬝⬝⬝⬝■■■■  esc interrupt",
		"Hello\nHello\nBuild · gpt-5.4 · 2.0s",
	}}
	a := NewOpenCodeTmuxAdapter(tmx, nil)
	a.pollInterval = 5 * time.Millisecond
	a.timeout = 100 * time.Millisecond

	tmpDir := t.TempDir()
	session := core.SessionInfo{
		Name:          "test-session",
		Workspace:     "lab",
		WorkspacePath: tmpDir,
		Agent:         "opencode",
		ResponseFile:  filepath.Join(tmpDir, "test-session.json"),
	}
	if _, err := NewResponseFile(tmpDir, session); err != nil {
		t.Fatalf("NewResponseFile: %v", err)
	}

	if err := a.Send(context.Background(), session, "Hello"); err != nil {
		t.Fatalf("Send() failed: %v", err)
	}

	rf, err := ReadResponseFile(session.ResponseFile)
	if err != nil {
		t.Fatalf("ReadResponseFile: %v", err)
	}
	if len(rf.Messages) != 1 {
		t.Fatalf("expected 1 agent message, got %d", len(rf.Messages))
	}
	if rf.Messages[0].Content != "Hello" {
		t.Fatalf("agent message = %q, want %q", rf.Messages[0].Content, "Hello")
	}
}
