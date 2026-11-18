package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostCheck(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := hostCheck([]string{"piecemeal.example.com"}, ok)
	for _, c := range []struct {
		host, path string
		want       int
	}{
		{"piecemeal.example.com", "/", http.StatusNoContent},
		{"PIECEMEAL.example.com.:443", "/", http.StatusNoContent},
		{"localhost:8080", "/", http.StatusNoContent},
		{"127.0.0.1:8080", "/", http.StatusNoContent},
		{"[::1]:8080", "/", http.StatusNoContent},
		{"rebound.attacker.example", "/api/piecemeal/v1/auth/login", http.StatusMisdirectedRequest},
		{"10.0.0.5:8080", "/", http.StatusMisdirectedRequest},
		{"10.0.0.5:8080", "/healthz", http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		r.Host = c.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s%s: %d, want %d", c.host, c.path, w.Code, c.want)
		}
	}
	// Off by default.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "anything.example"
	w := httptest.NewRecorder()
	hostCheck(nil, ok).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("no allowlist: %d", w.Code)
	}
}
