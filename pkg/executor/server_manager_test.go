package executor

import (
	"strings"
	"testing"
	"time"
)

func TestAllocatePortReturnsInRange(t *testing.T) {
	sm := NewServerManager()
	sm.mu.Lock()
	defer sm.mu.Unlock()

	port, err := sm.allocatePort()
	if err != nil {
		t.Fatalf("allocatePort() error: %v", err)
	}
	if port < sm.portStart || port > sm.portEnd {
		t.Errorf("port %d not in range [%d, %d]", port, sm.portStart, sm.portEnd)
	}
}

func TestAllocatePortExhaustion(t *testing.T) {
	sm := NewServerManager()
	// Use an isolated range so the test never conflicts with real services.
	sm.portStart = 30000
	sm.portEnd = 30004
	sm.mu.Lock()
	defer sm.mu.Unlock()

	totalPorts := sm.portEnd - sm.portStart + 1
	for i := 0; i < totalPorts; i++ {
		_, err := sm.allocatePort()
		if err != nil {
			t.Fatalf("allocatePort() failed on port %d: %v", i+1, err)
		}
	}

	_, err := sm.allocatePort()
	if err == nil {
		t.Error("expected error when all ports exhausted, got nil")
	}
}

func TestReleasePortAllowsReuse(t *testing.T) {
	sm := NewServerManager()
	sm.mu.Lock()
	defer sm.mu.Unlock()

	port, err := sm.allocatePort()
	if err != nil {
		t.Fatalf("allocatePort() error: %v", err)
	}

	sm.releasePort(port)

	port2, err := sm.allocatePort()
	if err != nil {
		t.Fatalf("allocatePort() after release error: %v", err)
	}
	if port2 != port {
		t.Errorf("expected released port %d to be reused, got %d", port, port2)
	}
}

func TestWaitForServerURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "standard opencode output",
			input: "opencode server listening on http://127.0.0.1:19100\n",
			want:  "http://127.0.0.1:19100",
		},
		{
			name:  "url on second line",
			input: "Starting server...\nhttp://127.0.0.1:19100\n",
			want:  "http://127.0.0.1:19100",
		},
		{
			name:  "url with trailing whitespace",
			input: "http://127.0.0.1:19100  \n",
			want:  "http://127.0.0.1:19100",
		},
		{
			name:    "no url before EOF",
			input:   "Starting server...\nReady.\n",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			url, err := waitForServerURL(r, 2*time.Second)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got url=%q", url)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.want {
				t.Errorf("url = %q, want %q", url, tt.want)
			}
		})
	}
}

func TestWaitForServerURLTimeout(t *testing.T) {
	// Use a reader that blocks forever (never returns data).
	r, _ := newBlockingReader()
	_, err := waitForServerURL(r, 50*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// blockingReader is a reader that blocks until closed.
type blockingReader struct {
	ch chan struct{}
}

func newBlockingReader() (*blockingReader, func()) {
	br := &blockingReader{ch: make(chan struct{})}
	return br, func() { close(br.ch) }
}

func (br *blockingReader) Read(p []byte) (int, error) {
	<-br.ch
	return 0, nil
}

func TestServerHandleTouch(t *testing.T) {
	h := &ServerHandle{LastUsed: time.Now().Add(-1 * time.Hour)}
	before := h.lastUsed()

	h.Touch()

	after := h.lastUsed()
	if !after.After(before) {
		t.Errorf("Touch() did not advance LastUsed: before=%v, after=%v", before, after)
	}
}

func TestReaperRemovesIdleServers(t *testing.T) {
	sm := NewServerManager()

	// Manually insert a fake handle (no real process).
	sm.mu.Lock()
	sm.usedPorts[19100] = true
	sm.servers["/tmp/test-workspace"] = &ServerHandle{
		Port:     19100,
		LastUsed: time.Now().Add(-1 * time.Hour), // already idle
	}
	sm.mu.Unlock()

	// Reap with very short thresholds.
	cancel := sm.StartReaper(5*time.Millisecond, 10*time.Millisecond)
	defer cancel()

	// Wait for at least one reap cycle.
	time.Sleep(30 * time.Millisecond)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.servers) != 0 {
		t.Errorf("expected 0 servers after reap, got %d", len(sm.servers))
	}
	if sm.usedPorts[19100] {
		t.Error("expected port 19100 to be released after reap")
	}
}

func TestReaperKeepsActiveServers(t *testing.T) {
	sm := NewServerManager()

	// Manually insert a handle that was just used.
	sm.mu.Lock()
	sm.usedPorts[19100] = true
	sm.servers["/tmp/test-workspace"] = &ServerHandle{
		Port:     19100,
		LastUsed: time.Now(),
	}
	sm.mu.Unlock()

	// Reap with short interval but long maxIdle.
	cancel := sm.StartReaper(5*time.Millisecond, 1*time.Hour)
	defer cancel()

	time.Sleep(30 * time.Millisecond)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.servers) != 1 {
		t.Errorf("expected 1 server (not idle), got %d", len(sm.servers))
	}
}

func TestShutdownClearsMaps(t *testing.T) {
	sm := NewServerManager()

	// Insert fake handles.
	sm.mu.Lock()
	sm.usedPorts[19100] = true
	sm.usedPorts[19101] = true
	sm.servers["/workspace/a"] = &ServerHandle{Port: 19100}
	sm.servers["/workspace/b"] = &ServerHandle{Port: 19101}
	sm.mu.Unlock()

	sm.Shutdown()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.servers) != 0 {
		t.Errorf("expected 0 servers after shutdown, got %d", len(sm.servers))
	}
	if len(sm.usedPorts) != 0 {
		t.Errorf("expected 0 used ports after shutdown, got %d", len(sm.usedPorts))
	}
}

func TestGetReturnsExistingHandle(t *testing.T) {
	sm := NewServerManager()

	handle := &ServerHandle{Port: 19100, Workspace: "/tmp/ws"}
	sm.mu.Lock()
	sm.servers["/tmp/ws"] = handle
	sm.mu.Unlock()

	got, ok := sm.Get("/tmp/ws")
	if !ok {
		t.Fatal("Get() returned false for existing workspace")
	}
	if got != handle {
		t.Error("Get() returned different handle")
	}
}

func TestGetReturnsFalseForMissing(t *testing.T) {
	sm := NewServerManager()

	_, ok := sm.Get("/tmp/nonexistent")
	if ok {
		t.Error("Get() returned true for nonexistent workspace")
	}
}

func TestNewServerManagerInitializesMaps(t *testing.T) {
	sm := NewServerManager()
	if sm.servers == nil {
		t.Error("servers map is nil")
	}
	if sm.usedPorts == nil {
		t.Error("usedPorts map is nil")
	}
}
