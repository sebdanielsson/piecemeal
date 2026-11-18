package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/auth"
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
