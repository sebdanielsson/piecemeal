package webui

import (
	"net/http"
	"net/http/httptest"
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
