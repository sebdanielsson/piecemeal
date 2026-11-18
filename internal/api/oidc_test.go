package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/auth/authtest"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

func TestOIDCSignIn(t *testing.T) {
	ctx := context.Background()
	provider := authtest.NewProvider(t, "piecemeal")
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.DiscardHandler)
	a, err := auth.New(ctx, auth.Config{LocalLogin: true, TempPasswordOut: io.Discard}, st, log)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	o := auth.NewOIDC(auth.OIDCConfig{
		Issuer: provider.URL, ClientID: "piecemeal", ClientSecret: "s", RedirectURL: srv.URL + Prefix + "/auth/oidc/callback",
		Name: "Pocket ID", AllowedGroups: []string{"media"}, AdminGroups: []string{"admins"},
	}, a)
	New(mux, Deps{Auth: a, OIDC: o, Log: log})

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// signIn runs the flow and returns where the callback redirects to.
	signIn := func(claims map[string]any) string {
		t.Helper()
		resp, err := browser.Get(srv.URL + Prefix + "/auth/oidc/login?next=/torrents")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("login: %d", resp.StatusCode)
		}
		authorize, _ := http.NewRequest(http.MethodGet, resp.Header.Get("Location"), nil)
		code := provider.Authorize(t, authorize, claims)
		callback := srv.URL + Prefix + "/auth/oidc/callback?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(authorize.URL.Query().Get("state"))
		resp, err = browser.Get(callback)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Location")
	}

	// Not in an allowed group: back to the sign-in page with a reason.
	if loc := signIn(map[string]any{"sub": "u1", "preferred_username": "alice", "groups": []string{"other"}}); !strings.HasPrefix(loc, "/login?error=") {
		t.Fatalf("denied sign-in redirected to %q", loc)
	}
	if loc := signIn(map[string]any{"sub": "u1", "preferred_username": "admin", "name": "Alice", "groups": []string{"media", "admins"}}); loc != "/torrents" {
		t.Fatalf("sign-in redirected to %q", loc)
	}
	me, _ := http.NewRequest(http.MethodGet, srv.URL+Prefix+"/auth/me", nil)
	resp, err := browser.Do(me)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// "admin" is the local admin's name, so the OIDC user gets another.
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"username":"admin-2"`) || !strings.Contains(string(b), `"isAdmin":true`) {
		t.Fatalf("me: %d %s", resp.StatusCode, b)
	}

	// A callback without the state cookie from this browser is refused.
	other := &http.Client{CheckRedirect: browser.CheckRedirect}
	resp, err = other.Get(srv.URL + Prefix + "/auth/oidc/callback?code=x&state=y")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") {
		t.Fatalf("forged callback redirected to %q", loc)
	}

	cfg := httptest.NewRecorder()
	mux.ServeHTTP(cfg, httptest.NewRequest(http.MethodGet, Prefix+"/auth/config", nil))
	if !strings.Contains(cfg.Body.String(), `"oidc":true`) || !strings.Contains(cfg.Body.String(), `"oidcName":"Pocket ID"`) {
		t.Fatalf("auth config: %s", cfg.Body)
	}
}
