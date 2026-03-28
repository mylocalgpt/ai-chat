package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

func setupTestOrchestrator(t *testing.T) (*Orchestrator, *store.Store) {
	t.Helper()
	// Dummy server that returns a valid response (not used by HandleAction tests)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"type":"direct_answer","content":"ok","confidence":0.9,"reasoning":"ok"}`}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	router := NewRouter("test-key").WithBaseURL(srv.URL)
	st := newTestStore(t)
	orch := NewOrchestrator(router, st, "test/model", "test/model2")
	return orch, st
}

func TestHandleAction_WorkspaceSwitch_ExactName(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	_, _ = st.CreateWorkspace(ctx, "laboratory", "/home/user/lab", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionWorkspaceSwitch, Workspace: "laboratory"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "Switched to laboratory" {
		t.Errorf("resp = %q, want 'Switched to laboratory'", resp)
	}
}

func TestHandleAction_WorkspaceSwitch_CaseInsensitive(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	_, _ = st.CreateWorkspace(ctx, "Laboratory", "/home/user/lab", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionWorkspaceSwitch, Workspace: "laboratory"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "Switched to Laboratory" {
		t.Errorf("resp = %q, want 'Switched to Laboratory'", resp)
	}
}

func TestHandleAction_WorkspaceSwitch_AliasMatch(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	ws, _ := st.CreateWorkspace(ctx, "laboratory", "/home/user/lab", "")
	_ = st.UpdateWorkspaceMetadata(ctx, ws.ID, json.RawMessage(`{"aliases": ["lab"]}`))

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionWorkspaceSwitch, Workspace: "lab"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "Switched to laboratory" {
		t.Errorf("resp = %q, want 'Switched to laboratory'", resp)
	}
}

func TestHandleAction_WorkspaceSwitch_PrefixMatch(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	_, _ = st.CreateWorkspace(ctx, "fitness-tracker", "/home/user/fitness", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionWorkspaceSwitch, Workspace: "fitness"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "Switched to fitness-tracker" {
		t.Errorf("resp = %q, want 'Switched to fitness-tracker'", resp)
	}
}

func TestHandleAction_WorkspaceSwitch_NotFound(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	_, _ = st.CreateWorkspace(ctx, "myproject", "/home/user/project", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionWorkspaceSwitch, Workspace: "nonexistent"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp, "not found") {
		t.Errorf("resp should contain 'not found', got: %s", resp)
	}
	if !strings.Contains(resp, "myproject") {
		t.Errorf("resp should list available workspaces, got: %s", resp)
	}
}

func TestHandleAction_WorkspaceSwitch_Ambiguous(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	_, _ = st.CreateWorkspace(ctx, "fitness-app", "/home/user/app", "")
	_, _ = st.CreateWorkspace(ctx, "fitness-tracker", "/home/user/tracker", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionWorkspaceSwitch, Workspace: "fitness"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp, "ambiguous") {
		t.Errorf("resp should contain 'ambiguous', got: %s", resp)
	}
	if !strings.Contains(resp, "fitness-app") || !strings.Contains(resp, "fitness-tracker") {
		t.Errorf("resp should list matching workspaces, got: %s", resp)
	}
}

func TestHandleAction_WorkspaceSwitch_UpdatesContext(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	ws, _ := st.CreateWorkspace(ctx, "myproject", "/home/user/project", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionWorkspaceSwitch, Workspace: "myproject"}

	_, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify context was updated in store
	uc, err := st.GetUserContext(ctx, "user1", "telegram")
	if err != nil {
		t.Fatalf("GetUserContext: %v", err)
	}
	if uc.ActiveWorkspaceID != ws.ID {
		t.Errorf("active workspace id = %d, want %d", uc.ActiveWorkspaceID, ws.ID)
	}
}

func TestHandleAction_Status_WithWorkspace(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	ws, _ := st.CreateWorkspace(ctx, "myproject", "/home/user/project", "")
	_ = st.SetActiveWorkspace(ctx, "user1", "telegram", ws.ID)
	_, _ = st.CreateSession(ctx, ws.ID, "claude", "tmux-1")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionStatus}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp, "myproject") {
		t.Errorf("resp should contain workspace name, got: %s", resp)
	}
	if !strings.Contains(resp, "claude") {
		t.Errorf("resp should contain session agent, got: %s", resp)
	}
}

func TestHandleAction_Status_NoWorkspace(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	_, _ = st.CreateWorkspace(ctx, "available-ws", "/home/user/ws", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionStatus}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp, "No active workspace") {
		t.Errorf("resp should say no active workspace, got: %s", resp)
	}
	if !strings.Contains(resp, "available-ws") {
		t.Errorf("resp should list available workspaces, got: %s", resp)
	}
}

func TestHandleAction_DirectAnswer(t *testing.T) {
	orch, _ := setupTestOrchestrator(t)
	ctx := context.Background()

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionDirectAnswer, Content: "The answer is 42."}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "The answer is 42." {
		t.Errorf("resp = %q, want 'The answer is 42.'", resp)
	}
}

func TestHandleAction_AgentTask(t *testing.T) {
	orch, _ := setupTestOrchestrator(t)
	ctx := context.Background()

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionAgentTask, Content: "fix the bug"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "" {
		t.Errorf("resp = %q, want empty string for agent_task", resp)
	}
}

func TestHandleAction_MetaCommand_ListWorkspaces(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	_, _ = st.CreateWorkspace(ctx, "project-a", "/home/user/a", "")
	_, _ = st.CreateWorkspace(ctx, "project-b", "/home/user/b", "")

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionMetaCommand, Content: "list workspaces", Reasoning: "user wants workspace list"}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp, "project-a") || !strings.Contains(resp, "project-b") {
		t.Errorf("resp should list workspaces, got: %s", resp)
	}
}

func TestMatchWorkspace_NoWorkspaces(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := matchWorkspace(ctx, "anything", st)
	if err == nil {
		t.Fatal("expected error when no workspaces exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found, got: %v", err)
	}
}

func TestHandleAction_Status_NoSession(t *testing.T) {
	orch, st := setupTestOrchestrator(t)
	ctx := context.Background()

	ws, _ := st.CreateWorkspace(ctx, "myproject", "/home/user/project", "")
	_ = st.SetActiveWorkspace(ctx, "user1", "telegram", ws.ID)

	msg := core.InboundMessage{SenderID: "user1", Channel: "telegram"}
	action := Action{Type: ActionStatus}

	resp, err := orch.HandleAction(ctx, msg, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp, "No active session") {
		t.Errorf("resp should say no active session, got: %s", resp)
	}
}

// Suppress unused import warnings
var _ = errors.Is
