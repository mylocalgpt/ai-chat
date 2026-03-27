package mcp

// ServerConfig holds configuration the MCP server needs from the app config.
// This avoids importing pkg/config directly, keeping the MCP package decoupled.
type ServerConfig struct {
	AllowedUsers []int64 // Telegram allowed user IDs for send_test_message
}

// Option configures optional dependencies on the MCP server.
type Option func(*Server)

// WithExecutor sets the session executor (for session restart/kill).
func WithExecutor(e Executor) Option {
	return func(s *Server) { s.executor = e }
}

// WithNotifier sets the workspace change notifier (e.g. Telegram command sync).
func WithNotifier(n WorkspaceChangeNotifier) Option {
	return func(s *Server) { s.notifier = n }
}

// WithChannelAdapter sets the channel adapter (for health checks and test messages).
func WithChannelAdapter(c ChannelAdapter) Option {
	return func(s *Server) { s.channel = c }
}
