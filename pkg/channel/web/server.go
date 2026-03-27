package web

import (
	"io/fs"
	"net/http"
)

// NewFileHandler returns an http.Handler that serves the embedded web UI.
// The distFS should be the embedded web/dist directory.
func NewFileHandler(distFS fs.FS) http.Handler {
	sub, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		panic("web/dist not found in embedded FS: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}
