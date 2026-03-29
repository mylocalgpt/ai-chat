package mcp

type MCPConfig struct {
	AllowedUsers  []int64
	ResponsesDir  string
	BinaryPath    string
	TelegramToken string
}

type Option func(*Server)

func WithSessionManager(sm SessionManager) Option {
	return func(s *Server) { s.sessionMgr = sm }
}

func WithNotifier(n WorkspaceChangeNotifier) Option {
	return func(s *Server) { s.notifier = n }
}

func WithChannelAdapter(c ChannelAdapter) Option {
	return func(s *Server) { s.channel = c }
}

func WithMCPConfig(cfg *MCPConfig) Option {
	return func(s *Server) { s.cfg = cfg }
}
