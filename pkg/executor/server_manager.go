package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	portRangeStart = 19100
	portRangeEnd   = 19199
)

// ServerManager owns the lifecycle of opencode serve processes:
// port allocation, spawning, health checking, idle reaping, and graceful shutdown.
type ServerManager struct {
	mu        sync.Mutex
	servers   map[string]*ServerHandle // workspace path -> handle
	usedPorts map[int]bool
}

// NewServerManager returns an initialized ServerManager.
func NewServerManager() *ServerManager {
	return &ServerManager{
		servers:   make(map[string]*ServerHandle),
		usedPorts: make(map[int]bool),
	}
}

// allocatePort returns the first available port in the configured range.
// Must be called under sm.mu lock.
func (sm *ServerManager) allocatePort() (int, error) {
	for p := portRangeStart; p <= portRangeEnd; p++ {
		if !sm.usedPorts[p] {
			sm.usedPorts[p] = true
			return p, nil
		}
	}
	return 0, fmt.Errorf("all ports exhausted (%d-%d)", portRangeStart, portRangeEnd)
}

// releasePort marks a port as available.
// Must be called under sm.mu lock.
func (sm *ServerManager) releasePort(port int) {
	delete(sm.usedPorts, port)
}

// ServerHandle represents a running opencode serve process.
type ServerHandle struct {
	URL       string
	Port      int
	Workspace string
	Process   *os.Process
	Cmd       *exec.Cmd
	Client    *http.Client
	LastUsed  time.Time
	mu        sync.Mutex
}

// Health checks whether the server is responsive by hitting GET {URL}/global/health.
func (h *ServerHandle) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL+"/global/health", nil)
	if err != nil {
		return fmt.Errorf("health check: build request: %w", err)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("health check: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// Stop gracefully shuts down the server process. Sends SIGINT first,
// waits up to 5 seconds, then SIGKILL if still alive.
func (h *ServerHandle) Stop() error {
	if h.Process == nil {
		return nil
	}

	// Send SIGINT for graceful shutdown.
	if err := h.Process.Signal(os.Interrupt); err != nil {
		// Process may already be dead; try kill directly.
		_ = h.Process.Kill()
		h.waitCmd()
		return nil
	}

	// Wait up to 5 seconds for graceful exit.
	done := make(chan struct{})
	go func() {
		h.waitCmd()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		_ = h.Process.Kill()
		<-done // wait for cmd.Wait to finish after kill
		return nil
	}
}

// waitCmd calls Cmd.Wait() to clean up zombie processes.
func (h *ServerHandle) waitCmd() {
	if h.Cmd != nil {
		_ = h.Cmd.Wait()
	}
}

// Touch updates the last-used timestamp.
func (h *ServerHandle) Touch() {
	h.mu.Lock()
	h.LastUsed = time.Now()
	h.mu.Unlock()
}

// lastUsed returns the last-used timestamp safely.
func (h *ServerHandle) lastUsed() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.LastUsed
}

// waitForServerURL reads stdout from the opencode serve process and extracts
// the listening URL. Uses a goroutine + channel pattern because bufio.Scanner
// blocks and cannot participate in a select statement.
func waitForServerURL(r io.Reader, timeout time.Duration) (string, error) {
	type lineResult struct {
		line string
		err  error
	}
	ch := make(chan lineResult, 1)

	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			ch <- lineResult{line: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			ch <- lineResult{err: err}
		}
		close(ch)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return "", fmt.Errorf("timeout waiting for server URL after %s", timeout)
		case result, ok := <-ch:
			if !ok {
				return "", fmt.Errorf("stdout closed before server URL was found")
			}
			if result.err != nil {
				return "", fmt.Errorf("reading stdout: %w", result.err)
			}
			if idx := strings.Index(result.line, "http://"); idx >= 0 {
				return strings.TrimSpace(result.line[idx:]), nil
			}
		}
	}
}

// startServer spawns an opencode serve process for the given workspace.
// Must be called under sm.mu lock (for port allocation); releases lock
// is NOT done here - caller manages locking.
func (sm *ServerManager) startServer(workspace string) (*ServerHandle, error) {
	port, err := sm.allocatePort()
	if err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}

	cmd := exec.Command("opencode", "serve", "--port", strconv.Itoa(port), "--hostname", "127.0.0.1")
	cmd.Dir = workspace

	// Route stderr to slog for debuggability.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		sm.releasePort(port)
		return nil, fmt.Errorf("start server: stderr pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sm.releasePort(port)
		return nil, fmt.Errorf("start server: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		sm.releasePort(port)
		return nil, fmt.Errorf("start server: exec: %w", err)
	}

	// Stderr logging goroutine.
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			slog.Debug("opencode serve stderr", "workspace", workspace, "line", scanner.Text())
		}
	}()

	// Parse URL from stdout.
	url, err := waitForServerURL(stdout, 10*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		sm.releasePort(port)
		return nil, fmt.Errorf("start server: %w", err)
	}

	// Drain remaining stdout so the process never blocks on writes.
	// The waitForServerURL goroutine stops being read after the URL is found;
	// without this drain the OS pipe buffer fills and the process hangs.
	go func() {
		_, _ = io.Copy(io.Discard, stdout)
	}()

	handle := &ServerHandle{
		URL:       url,
		Port:      port,
		Workspace: workspace,
		Process:   cmd.Process,
		Cmd:       cmd,
		Client:    &http.Client{Timeout: 30 * time.Second},
		LastUsed:  time.Now(),
	}

	// Health check with 5 second timeout.
	healthCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := handle.Health(healthCtx); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		sm.releasePort(port)
		return nil, fmt.Errorf("start server: initial health check: %w", err)
	}

	sm.servers[workspace] = handle
	return handle, nil
}

// GetOrStart returns an existing healthy server for the workspace,
// or starts a new one.
func (sm *ServerManager) GetOrStart(workspace string) (*ServerHandle, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if handle, ok := sm.servers[workspace]; ok {
		healthCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := handle.Health(healthCtx); err == nil {
			handle.Touch()
			return handle, nil
		}

		// Unhealthy; clean up and fall through to start new one.
		slog.Info("opencode serve unhealthy, restarting", "workspace", workspace)
		_ = handle.Stop()
		sm.releasePort(handle.Port)
		delete(sm.servers, workspace)
	}

	return sm.startServer(workspace)
}

// Get returns the server handle for a workspace if one exists. Does not start a new server.
func (sm *ServerManager) Get(workspace string) (*ServerHandle, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	h, ok := sm.servers[workspace]
	return h, ok
}

// Register adds a pre-configured ServerHandle for a workspace. This is
// primarily useful for testing, where the handle points at a mock HTTP server
// instead of a real opencode serve process.
func (sm *ServerManager) Register(workspace string, handle *ServerHandle) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.servers[workspace] = handle
}

// StartReaper spawns a background goroutine that periodically stops idle servers.
// Returns a cancel function to stop the reaper.
func (sm *ServerManager) StartReaper(interval, maxIdle time.Duration) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sm.reapIdle(maxIdle)
			}
		}
	}()

	return cancel
}

// reapIdle stops servers that have been idle longer than maxIdle.
func (sm *ServerManager) reapIdle(maxIdle time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for workspace, handle := range sm.servers {
		if now.Sub(handle.lastUsed()) > maxIdle {
			slog.Info("reaping idle opencode server", "workspace", workspace)
			_ = handle.Stop()
			sm.releasePort(handle.Port)
			delete(sm.servers, workspace)
		}
	}
}

// Shutdown stops all managed servers and clears internal state.
func (sm *ServerManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for workspace, handle := range sm.servers {
		slog.Info("shutting down opencode server", "workspace", workspace)
		_ = handle.Stop()
		sm.releasePort(handle.Port)
	}

	// Clear maps.
	sm.servers = make(map[string]*ServerHandle)
	sm.usedPorts = make(map[int]bool)
}
