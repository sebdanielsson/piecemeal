package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUnknownAPIRoutesAreNotTheSPA(t *testing.T) {
	for path, want := range map[string]int{
		"/api/piecemeal/v1/nope": http.StatusNotFound,
		"/assets/missing.js":     http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
	}
}

func TestAlternativeHidesDotfiles(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "ui", ".env": "secret", "js/.map": "x", "js/app.js": "js"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h := Alternative(dir)
	for path, want := range map[string]int{
		"/": http.StatusOK, "/js/app.js": http.StatusOK,
		"/.env": http.StatusNotFound, "/js/.map": http.StatusNotFound, "/js/../.env": http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
		if want == http.StatusOK && rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s: no CSP", path)
		}
	}
}
