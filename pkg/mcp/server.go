package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the official MCP server and holds references to dependencies.
type Server struct {
	inner         *gomcp.Server
	serverSession *gomcp.ServerSession // held to prevent GC

	// Dependencies injected via options. All are nil-safe.
	store    MCPStore
	cfg      *ServerConfig
	executor Executor                // optional, nil until wired
	notifier WorkspaceChangeNotifier // optional
	channel  ChannelAdapter          // optional
}

// NewServer creates an MCP server with the given store and config.
// Optional dependencies (executor, notifier, channel) are set via options.
func NewServer(st MCPStore, cfg *ServerConfig, opts ...Option) *Server {
	srv := &Server{store: st, cfg: cfg}
	for _, opt := range opts {
		opt(srv)
	}

	srv.inner = gomcp.NewServer(
		&gomcp.Implementation{Name: "ai-chat", Version: "1.0.0"},
		&gomcp.ServerOptions{
			Instructions: "AI chat orchestrator MCP server. Manages workspaces, sessions, health checks, and model configuration.",
		},
	)

	srv.registerWorkspaceTools()
	srv.registerSessionTools()
	srv.registerHealthTools()
	srv.registerModelTools()

	return srv
}

// ConnectInProcess creates an in-process MCP client session connected to this
// server via in-memory transports. The returned session can list and call tools
// without any network I/O.
func (s *Server) ConnectInProcess(ctx context.Context) (*gomcp.ClientSession, error) {
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	serverSession, err := s.inner.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: server connect: %w", err)
	}
	s.serverSession = serverSession

	client := gomcp.NewClient(
		&gomcp.Implementation{Name: "ai-chat-orchestrator", Version: "1.0.0"},
		nil,
	)

	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: client connect: %w", err)
	}

	return session, nil
}
