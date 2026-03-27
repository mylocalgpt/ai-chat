package web

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

// DevMode returns true when AI_CHAT_DEV=1 is set.
func DevMode() bool {
	return os.Getenv("AI_CHAT_DEV") == "1"
}

// NewDevProxyHandler returns a reverse proxy to the Vite dev server.
func NewDevProxyHandler() http.Handler {
	target, _ := url.Parse("http://localhost:5173")
	return httputil.NewSingleHostReverseProxy(target)
}
