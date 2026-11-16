// Package webui serves the web UI that `pnpm -C web build` writes to
// internal/webui/dist, embedded into the binary.
//
// Go-only builds and CI jobs that don't build the UI need the directory to
// exist: `mkdir -p internal/webui/dist && touch internal/webui/dist/.keep`.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded UI.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// The embed pattern guarantees the directory exists.
		panic(err)
	}
	return http.FileServerFS(sub)
}
