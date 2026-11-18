package webui

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Alternative serves an alternative qBittorrent web UI, such as VueTorrent,
// from a directory on disk. A release archive's directory works too: its
// files are in public/. Directories without an index.html are not listed.
func Alternative(dir string) http.Handler {
	if _, err := os.Stat(filepath.Join(dir, "public", "index.html")); err == nil {
		dir = filepath.Join(dir, "public")
	}
	root := os.DirFS(dir)
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "."
		}
		st, err := fs.Stat(root, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if st.IsDir() {
			if _, err := fs.Stat(root, path.Join(name, "index.html")); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		files.ServeHTTP(w, r)
	})
}
