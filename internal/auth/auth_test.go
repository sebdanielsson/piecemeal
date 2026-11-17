package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword(h, "correct horse"); !ok || err != nil {
		t.Fatalf("right password: %v, %v", ok, err)
	}
	if ok, _ := VerifyPassword(h, "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	for _, bad := range []string{"", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=999999999,t=1,p=1$c2FsdA$aGFzaA"} {
		if _, err := VerifyPassword(bad, "x"); !errors.Is(err, ErrBadHash) {
			t.Errorf("VerifyPassword(%q): %v, want ErrBadHash", bad, err)
		}
	}
}

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, Config{AdminUsername: "admin", AdminPasswordHash: h, LocalLogin: true}, st, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}

func TestLoginSessionAndLogout(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	r := httptest.NewRequest(http.MethodPost, "/", nil)

	if _, _, err := s.Login(ctx, r, "admin", "nope", KindWeb); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("bad password: %v", err)
	}
	token, p, err := s.Login(ctx, r, "admin", "secret", KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsAdmin || p.Scope != ScopeFull {
		t.Fatalf("principal %+v", p)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieWeb, Value: token})
	got, err := s.Authenticate(req, CookieWeb)
	if err != nil || got.Username != "admin" || got.SessionKind != KindWeb {
		t.Fatalf("authenticate: %+v, %v", got, err)
	}
	if err := s.Logout(ctx, got); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(req, CookieWeb); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("after logout: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	now := time.Now()
	s.now = func() time.Time { return now }
	token, _, err := s.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "secret", KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieWeb, Value: token})
	now = now.Add(8 * 24 * time.Hour) // past the 7-day idle timeout
	if _, err := s.Authenticate(req, CookieWeb); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("idle session: %v", err)
	}
}

func TestLockout(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	for range 5 {
		_, _, _ = s.Login(ctx, r, "admin", "wrong", KindWeb)
	}
	if _, _, err := s.Login(ctx, r, "admin", "secret", KindWeb); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("after 5 failures: %v, want ErrLockedOut", err)
	}
}

func TestAPIKeys(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	_, admin, err := s.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "secret", KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	k, secret, err := s.CreateAPIKey(ctx, admin, "radarr", ScopeQBittorrent)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	p, err := s.Authenticate(req, CookieSID)
	if err != nil || p.Scope != ScopeQBittorrent || p.APIKeyID != k.ID || p.Method != "apiKey" {
		t.Fatalf("key auth: %+v, %v", p, err)
	}
	req.Header.Set("Authorization", "Bearer pm_wrong")
	if _, err := s.Authenticate(req, CookieSID); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("wrong key: %v", err)
	}
	if err := s.DeleteAPIKey(ctx, admin, k.ID); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	if _, err := s.Authenticate(req, CookieSID); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("deleted key: %v", err)
	}
}

func TestClientIP(t *testing.T) {
	s := &Service{cfg: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}}
	for _, tc := range []struct{ remote, xff, want string }{
		{"192.0.2.1:1234", "203.0.113.9", "192.0.2.1"},              // untrusted remote: header ignored
		{"10.0.0.1:1234", "203.0.113.9", "203.0.113.9"},             // trusted proxy
		{"10.0.0.1:1234", "198.51.100.1, 10.0.0.2", "198.51.100.1"}, // chain of proxies
		{"10.0.0.1:1234", "", "10.0.0.1"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := s.ClientIP(r); got != tc.want {
			t.Errorf("ClientIP(%s, %q) = %s, want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}
