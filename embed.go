// Build prerequisite: run "cd web && npm run build" before "go build".
// The go:embed directive requires web/dist/ to exist at compile time.
package aichat

import "embed"

//go:embed all:web/dist
var WebDist embed.FS
