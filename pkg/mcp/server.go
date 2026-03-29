package mcp

import (
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	inner *gomcp.Server

	store      MCPStore
	cfg        *MCPConfig
	sessionMgr SessionManager
	notifier   WorkspaceChangeNotifier
	channel    ChannelAdapter
}

func NewServer(st MCPStore, cfg *MCPConfig, opts ...Option) *Server {
	srv := &Server{store: st, cfg: cfg}
	for _, opt := range opts {
		opt(srv)
	}

	srv.inner = gomcp.NewServer(
		&gomcp.Implementation{Name: "ai-chat", Version: "1.0.0"},
		&gomcp.ServerOptions{
			Instructions: "ai-chat MCP server. Manages workspaces, sessions, health checks, and self-configuration.",
		},
	)

	srv.registerWorkspaceTools()
	srv.registerSessionTools()
	srv.registerHealthTools()
	srv.registerSelfConfigTools()

	return srv
}
