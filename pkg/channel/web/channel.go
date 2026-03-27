package web

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/mylocalgpt/ai-chat/pkg/core"
	"github.com/mylocalgpt/ai-chat/pkg/store"
)

// Compile-time interface check.
var _ core.Channel = (*WebChannel)(nil)

// WebChannel implements core.Channel for the browser-based web UI.
type WebChannel struct {
	store      *store.Store
	connMgr    *ConnManager
	msgHandler func(context.Context, core.InboundMessage)
	mux        *http.ServeMux
	running    atomic.Bool
}

// NewWebChannel creates a new web channel that registers its WebSocket
// handler on the given mux.
func NewWebChannel(st *store.Store, mux *http.ServeMux) *WebChannel {
	return &WebChannel{
		store:   st,
		connMgr: NewConnManager(),
		mux:     mux,
	}
}

// SetMessageHandler registers the callback for inbound messages.
// Called by the orchestrator before Start.
func (wc *WebChannel) SetMessageHandler(fn func(context.Context, core.InboundMessage)) {
	wc.msgHandler = fn
}

// Start registers the /ws handler on the mux and marks the channel as running.
func (wc *WebChannel) Start(_ context.Context) error {
	wc.mux.HandleFunc("/ws", wc.handleWS)
	wc.running.Store(true)
	slog.Info("web channel started")
	return nil
}

// Stop closes all connections and marks the channel as not running.
func (wc *WebChannel) Stop() error {
	wc.running.Store(false)
	wc.connMgr.CloseAll()
	slog.Info("web channel stopped")
	return nil
}

// Send delivers an outbound message to the browser via WebSocket.
func (wc *WebChannel) Send(_ context.Context, msg core.OutboundMessage) error {
	return wc.connMgr.Send(webSenderID, ServerMessage{
		Type:    wsMsgResponse,
		Content: msg.Content,
	})
}
