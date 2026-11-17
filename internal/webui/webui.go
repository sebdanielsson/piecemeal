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
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded UI. Paths that aren't files get index.html,
// so the SPA's client-side routes work on reload. Vite's hashed assets are
// cached forever; everything else is revalidated.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// The embed pattern guarantees the directory exists.
		panic(err)
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(sub, name); err != nil || st.IsDir() {
			if (path.Ext(name) != "" && name != "index.html") || strings.HasPrefix(name, "api/") {
				// A missing asset, not a client-side route.
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, sub)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if name == "index.html" {
			serveIndex(w, r, sub)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	b, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "the web UI is not built into this binary", http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(b)
}
