package web

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ConnManager tracks active WebSocket connections.
type ConnManager struct {
	mu    sync.RWMutex
	conns map[string]*websocket.Conn
}

// NewConnManager creates a new connection manager.
func NewConnManager() *ConnManager {
	return &ConnManager{
		conns: make(map[string]*websocket.Conn),
	}
}

// Add registers a connection for the given sender ID. If a connection
// already exists for that sender, the old one is closed first.
func (cm *ConnManager) Add(senderID string, conn *websocket.Conn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if old, ok := cm.conns[senderID]; ok {
		_ = old.Close(websocket.StatusGoingAway, "replaced by new connection")
	}
	cm.conns[senderID] = conn
}

// Remove unregisters the connection for the given sender ID.
func (cm *ConnManager) Remove(senderID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.conns, senderID)
}

// Send sends a message to a specific sender's connection.
func (cm *ConnManager) Send(senderID string, msg ServerMessage) error {
	cm.mu.RLock()
	conn, ok := cm.conns[senderID]
	cm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no connection for sender %q", senderID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return writeJSON(ctx, conn, msg)
}

// Broadcast sends a message to all active connections.
func (cm *ConnManager) Broadcast(msg ServerMessage) error {
	cm.mu.RLock()
	conns := make(map[string]*websocket.Conn, len(cm.conns))
	for k, v := range cm.conns {
		conns[k] = v
	}
	cm.mu.RUnlock()

	var firstErr error
	for _, conn := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := writeJSON(ctx, conn, msg); err != nil && firstErr == nil {
			firstErr = err
		}
		cancel()
	}
	return firstErr
}

// CloseAll closes all active connections.
func (cm *ConnManager) CloseAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for id, conn := range cm.conns {
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
		delete(cm.conns, id)
	}
}
