package testing

import (
	"context"
	"fmt"
	"sync"

	"github.com/mylocalgpt/ai-chat/pkg/core"
)

type MockChannel struct {
	name     string
	mu       sync.Mutex
	cond     *sync.Cond
	messages []core.OutboundMessage
	started  bool
}

func NewMockChannel(name string) *MockChannel {
	m := &MockChannel{name: name}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *MockChannel) Start(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

func (m *MockChannel) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	m.cond.Broadcast()
	return nil
}

func (m *MockChannel) Send(ctx context.Context, msg core.OutboundMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	m.messages = append(m.messages, msg)
	m.cond.Broadcast()
	return nil
}

func (m *MockChannel) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

func (m *MockChannel) Messages() []core.OutboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]core.OutboundMessage, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *MockChannel) WaitForContent(ctx context.Context, after int) (string, error) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.cond.Broadcast()
			m.mu.Unlock()
		case <-done:
		}
	}()
	defer close(done)

	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.messages) <= after {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		m.cond.Wait()
	}
	if after < 0 || after >= len(m.messages) {
		return "", fmt.Errorf("message index out of range")
	}
	return m.messages[after].Content, nil
}

var _ core.Channel = (*MockChannel)(nil)
