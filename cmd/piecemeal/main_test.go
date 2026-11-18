package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/config"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
	"github.com/sebdanielsson/piecemeal/internal/logbuf"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

func TestHashPassword(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "in"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("hunter2\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := hashPassword(f, &out); err != nil {
		t.Fatal(err)
	}
	ok, err := auth.VerifyPassword(string(bytes.TrimSpace(out.Bytes())), "hunter2")
	if !ok || err != nil {
		t.Fatalf("hash doesn't verify: %v %v", ok, err)
	}
}

func TestOpenAPIIsValidJSON(t *testing.T) {
	var out bytes.Buffer
	if err := writeOpenAPI(&out); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi version %v", doc["openapi"])
	}
}

func TestHealthcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	}))
	defer srv.Close()
	if err := healthcheck(strings.TrimPrefix(srv.URL, "http://")); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	if err := healthcheck(strings.TrimPrefix(srv.URL, "http://")); err == nil {
		t.Fatal("no error with nothing listening")
	}
}

func TestRoutes(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	save := t.TempDir()
	defaults := settings.Defaults(save)
	defaults.PeerPort = 0
	sm, err := settings.Load(ctx, st, defaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	e, err := engine.New(ctx, engine.Config{DataDir: t.TempDir(), AllowedRoots: []string{save}, ClientConfig: enginetest.Offline}, st, sm, log)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	a, err := auth.New(ctx, auth.Config{LocalLogin: true, TempPasswordOut: io.Discard}, st, log)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.FromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	cfg.QBittorrentWebUIDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg.QBittorrentWebUIDir, "index.html"), []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	mux, _ := routes(routeDeps{cfg: cfg, engine: e, settings: sm, auth: a, store: st, logs: logbuf.New(10), log: log})
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/readyz", http.StatusOK},
		{http.MethodGet, "/metrics", http.StatusUnauthorized},
		{http.MethodGet, "/api/piecemeal/v1/auth/config", http.StatusOK},
		{http.MethodGet, "/api/piecemeal/v1/torrents", http.StatusUnauthorized},
		{http.MethodGet, "/api/v2/app/webapiVersion", http.StatusForbidden},
		{http.MethodPost, "/api/v2/auth/login", http.StatusUnauthorized},
		{http.MethodGet, "/api/v2/auth/login", http.StatusMethodNotAllowed},
		{http.MethodGet, "/qbittorrent/", http.StatusOK},
		{http.MethodGet, "/qbittorrent", http.StatusTemporaryRedirect},
		{http.MethodGet, "/qbittorrent/missing.js", http.StatusNotFound},
		{http.MethodGet, "/qbittorrent/api/v2/app/webapiVersion", http.StatusForbidden},
		{http.MethodPost, "/qbittorrent/api/v2/app/webapiVersion", http.StatusForbidden},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}
