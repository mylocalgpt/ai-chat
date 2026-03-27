package mcp

import (
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the official MCP server and holds references to dependencies.
type Server struct {
	inner *gomcp.Server

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
