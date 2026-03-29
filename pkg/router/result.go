package router

import "github.com/mylocalgpt/ai-chat/pkg/core"

type ResultKind string

const (
	ResultText                 ResultKind = "text"
	ResultWorkspacePicker      ResultKind = "workspace_picker"
	ResultSessionPicker        ResultKind = "session_picker"
	ResultSecurityConfirmation ResultKind = "security_confirmation"
	ResultNoReply              ResultKind = "no_reply"
)

type Result struct {
	Kind                 ResultKind
	Text                 string
	WorkspacePicker      *WorkspacePickerData
	SessionPicker        *SessionPickerData
	SecurityConfirmation *SecurityConfirmationData
}

type WorkspaceOption struct {
	ID   int64
	Name string
	Path string
}

type WorkspacePickerData struct {
	Workspaces        []WorkspaceOption
	ActiveWorkspaceID int64
	Prompt            string
}

type SessionOption struct {
	ID       int64
	Name     string
	Slug     string
	Agent    string
	Status   string
	IsActive bool
}

type SessionPickerData struct {
	WorkspaceID     int64
	WorkspaceName   string
	Sessions        []SessionOption
	ActiveSessionID int64
	Prompt          string
}

type SecurityConfirmationData struct {
	Token       string
	Summary     string
	SessionID   int64
	WorkspaceID int64
}

type Request struct {
	Message *core.InboundMessage
}

func TextResult(text string) Result {
	return Result{Kind: ResultText, Text: text}
}

func NoReplyResult() Result {
	return Result{Kind: ResultNoReply}
}
