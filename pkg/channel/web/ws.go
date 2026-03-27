package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// WebSocket message types.
type wsMessageType string

const (
	wsMsgMessage         wsMessageType = "message"
	wsMsgSwitchWorkspace wsMessageType = "switch_workspace"
	wsMsgResponse        wsMessageType = "response"
	wsMsgTyping          wsMessageType = "typing"
	wsMsgStatus          wsMessageType = "status"
	wsMsgError           wsMessageType = "error"
)

// ClientMessage is a message from the browser.
type ClientMessage struct {
	Type      wsMessageType `json:"type"`
	Content   string        `json:"content,omitempty"`
	Workspace string        `json:"workspace,omitempty"`
}

// ServerMessage is a message to the browser.
type ServerMessage struct {
	Type            wsMessageType   `json:"type"`
	Content         string          `json:"content,omitempty"`
	Workspace       string          `json:"workspace,omitempty"`
	MessageID       int64           `json:"message_id,omitempty"`
	Message         string          `json:"message,omitempty"`
	Workspaces      []WorkspaceInfo `json:"workspaces,omitempty"`
	ActiveWorkspace string          `json:"active_workspace,omitempty"`
	Messages        []MessageInfo   `json:"messages,omitempty"`
}

// WorkspaceInfo is a workspace summary for the browser.
type WorkspaceInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// MessageInfo is a message summary for the browser.
type MessageInfo struct {
	ID        int64  `json:"id"`
	Content   string `json:"content"`
	Direction string `json:"direction"`
	CreatedAt string `json:"created_at"`
}

const (
	webSenderID    = "web-default"
	webChannelName = "web"
	historyLimit   = 50
)

// handleWS upgrades the HTTP connection to WebSocket and manages the session.
func (wc *WebChannel) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		slog.Error("websocket accept failed", "error", err)
		return
	}

	wc.connMgr.Add(webSenderID, conn)
	defer func() {
		wc.connMgr.Remove(webSenderID)
		conn.CloseNow()
	}()

	ctx := r.Context()

	// Send initial status.
	if err := wc.sendStatus(ctx, conn); err != nil {
		slog.Error("failed to send initial status", "error", err)
		return
	}

	// Read loop.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				slog.Info("websocket closed normally")
			} else {
				slog.Warn("websocket read error", "error", err)
			}
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			wc.sendError(ctx, conn, "invalid message format")
			continue
		}

		switch msg.Type {
		case wsMsgMessage:
			wc.handleMessage(ctx, conn, msg)
		case wsMsgSwitchWorkspace:
			wc.handleSwitchWorkspace(ctx, conn, msg)
		default:
			wc.sendError(ctx, conn, fmt.Sprintf("unknown message type: %s", msg.Type))
		}
	}
}

// sendStatus sends the current workspace and message state to the client.
func (wc *WebChannel) sendStatus(ctx context.Context, conn *websocket.Conn) error {
	workspaces, err := wc.store.ListWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("listing workspaces: %w", err)
	}

	var wsInfos []WorkspaceInfo
	for _, w := range workspaces {
		wsInfos = append(wsInfos, WorkspaceInfo{ID: w.ID, Name: w.Name})
	}

	var activeWorkspaceName string
	var activeWorkspaceID int64

	uc, err := wc.store.GetUserContext(ctx, webSenderID, webChannelName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// No context yet; default to first workspace.
			if len(workspaces) > 0 {
				activeWorkspaceName = workspaces[0].Name
				activeWorkspaceID = workspaces[0].ID
			}
		} else {
			return fmt.Errorf("getting user context: %w", err)
		}
	} else {
		activeWorkspaceID = uc.ActiveWorkspaceID
		// Find workspace name.
		for _, w := range workspaces {
			if w.ID == uc.ActiveWorkspaceID {
				activeWorkspaceName = w.Name
				break
			}
		}
	}

	var msgInfos []MessageInfo
	if activeWorkspaceID > 0 {
		messages, err := wc.store.ListMessages(ctx, activeWorkspaceID, historyLimit)
		if err != nil {
			return fmt.Errorf("listing messages: %w", err)
		}
		for _, m := range messages {
			msgInfos = append(msgInfos, MessageInfo{
				ID:        m.ID,
				Content:   m.Content,
				Direction: string(m.Direction),
				CreatedAt: m.CreatedAt.Format(time.RFC3339),
			})
		}
	}

	return writeJSON(ctx, conn, ServerMessage{
		Type:            wsMsgStatus,
		Workspaces:      wsInfos,
		ActiveWorkspace: activeWorkspaceName,
		Messages:        msgInfos,
	})
}

// handleMessage processes an incoming chat message from the browser.
func (wc *WebChannel) handleMessage(ctx context.Context, conn *websocket.Conn, msg ClientMessage) {
	if msg.Content == "" {
		wc.sendError(ctx, conn, "empty message")
		return
	}

	inbound := core.InboundMessage{
		ID:        fmt.Sprintf("web-%d", time.Now().UnixNano()),
		Channel:   webChannelName,
		SenderID:  webSenderID,
		Content:   msg.Content,
		Timestamp: time.Now(),
	}

	if wc.msgHandler != nil {
		wc.msgHandler(ctx, inbound)
	} else {
		slog.Warn("no message handler registered, echoing")
		wc.sendError(ctx, conn, "no handler registered")
	}
}

// handleSwitchWorkspace switches the active workspace and sends updated status.
func (wc *WebChannel) handleSwitchWorkspace(ctx context.Context, conn *websocket.Conn, msg ClientMessage) {
	if msg.Workspace == "" {
		wc.sendError(ctx, conn, "workspace name required")
		return
	}

	ws, err := wc.store.GetWorkspace(ctx, msg.Workspace)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			wc.sendError(ctx, conn, fmt.Sprintf("workspace %q not found", msg.Workspace))
		} else {
			wc.sendError(ctx, conn, "failed to look up workspace")
			slog.Error("workspace lookup failed", "workspace", msg.Workspace, "error", err)
		}
		return
	}

	if err := wc.store.SetActiveWorkspace(ctx, webSenderID, webChannelName, ws.ID); err != nil {
		wc.sendError(ctx, conn, "failed to switch workspace")
		slog.Error("set active workspace failed", "error", err)
		return
	}

	if err := wc.sendStatus(ctx, conn); err != nil {
		slog.Error("failed to send status after workspace switch", "error", err)
	}
}

// sendError sends an error message to the client.
func (wc *WebChannel) sendError(ctx context.Context, conn *websocket.Conn, message string) {
	if err := writeJSON(ctx, conn, ServerMessage{Type: wsMsgError, Message: message}); err != nil {
		slog.Error("failed to send error to client", "error", err)
	}
}

// writeJSON marshals and sends a JSON message over the WebSocket.
func writeJSON(ctx context.Context, conn *websocket.Conn, msg ServerMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
