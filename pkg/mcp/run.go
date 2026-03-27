package mcp

import (
	"context"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Run starts the MCP server on the stdio transport.
// It blocks until the context is canceled or the connection is closed.
func (s *Server) Run(ctx context.Context) error {
	return s.inner.Run(ctx, &gomcp.StdioTransport{})
}
