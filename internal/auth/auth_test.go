package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
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
	s := &Service{cfg: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}, log: slog.New(slog.DiscardHandler)}
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

func TestParallelGuessesCountTowardLockout(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	verified := 0
	for range 12 {
		wg.Go(func() {
			_, _, err := s.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "wrong", KindWeb)
			if errors.Is(err, ErrBadCredentials) {
				mu.Lock()
				verified++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if verified > 5 {
		t.Fatalf("%d parallel guesses were verified, want at most 5", verified)
	}
	if _, _, err := s.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "secret", KindWeb); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("after the burst: %v, want ErrLockedOut", err)
	}
}

func TestTemporaryPasswordGoesToItsWriterOnly(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var out, logs bytes.Buffer
	_, err = New(ctx, Config{LocalLogin: true, TempPasswordOut: &out}, st, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "temporary admin password") {
		t.Fatalf("no password written: %q", out.String())
	}
	pw := strings.TrimSpace(out.String()[strings.LastIndex(out.String(), ":")+1:])
	if pw == "" || strings.Contains(logs.String(), pw) {
		t.Fatalf("password %q leaked into the log: %s", pw, logs.String())
	}
}

func TestParallelBasicWithTheRightPasswordDoesntLockOut(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for range 12 {
		wg.Go(func() {
			if _, err := s.Basic(ctx, httptest.NewRequest(http.MethodGet, "/", nil), "admin", "secret"); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if failed != 0 {
		t.Fatalf("%d of 12 parallel requests with the right password failed", failed)
	}
}

func TestLimiterKeys(t *testing.T) {
	l := newLoginLimiter(2, time.Minute)
	now := time.Now()
	// Addresses in one IPv6 /64 share a count; IPv4 addresses don't.
	if !l.begin("2001:db8::1", now) || !l.begin("2001:db8::2", now) || l.begin("2001:db8::ffff", now) {
		t.Fatal("a /64 wasn't counted together")
	}
	if !l.locked("2001:db8::abcd", now) || l.locked("2001:db8:0:1::1", now) {
		t.Fatal("locked the wrong /64")
	}
	if !l.begin("192.0.2.1", now) || !l.begin("192.0.2.2", now) {
		t.Fatal("IPv4 addresses were counted together")
	}
	// Private IPv6 addresses, as on a LAN or a container network, count
	// one by one.
	for range 2 {
		l.begin("fd00::1", now)
	}
	if !l.locked("fd00::1", now) || !l.begin("fd00::2", now) || l.locked("fd00::3", now) {
		t.Fatal("private IPv6 addresses were counted together")
	}
	// A full table keeps the addresses it has and lets new ones through
	// untracked, instead of locking out everyone.
	for i := range maxTracked {
		l.fails[fmt.Sprintf("x%d", i)] = []time.Time{now}
	}
	if !l.begin("198.51.100.1", now) || l.locked("198.51.100.1", now) || !l.locked("2001:db8::1", now) {
		t.Fatal("a full table forgot or refused")
	}
	// Once the entries expire they're swept and tracking resumes.
	later := now.Add(2 * time.Minute)
	if !l.begin("198.51.100.1", later) || len(l.fails) != 1 {
		t.Fatalf("expired entries weren't swept: %d left", len(l.fails))
	}
}

func TestPasswordChangeEndsAdminSessions(t *testing.T) {
	s, st := newService(t)
	ctx := context.Background()
	token, p, err := s.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "secret", KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := s.CreateAPIKey(ctx, p, "radarr", ScopeQBittorrent)
	if err != nil {
		t.Fatal(err)
	}
	restart := func(hash string) *Service {
		t.Helper()
		s, err := New(ctx, Config{AdminUsername: "admin", AdminPasswordHash: hash, LocalLogin: true}, st, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	// The same hash keeps the session.
	if _, err := restart(s.cfg.AdminPasswordHash).Session(ctx, token); err != nil {
		t.Fatalf("same hash: %v", err)
	}
	h, err := HashPassword("rotated")
	if err != nil {
		t.Fatal(err)
	}
	s2 := restart(h)
	if _, err := s2.Session(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("after rotation: %v", err)
	}
	// Keys are left for the admin to review: the *arrs would break.
	if _, err := s2.Key(ctx, key); err != nil {
		t.Fatalf("key after rotation: %v", err)
	}
}

func TestDeleteUser(t *testing.T) {
	s, st := newService(t)
	ctx := context.Background()
	_, admin, err := s.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "secret", KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(ctx, store.CreateUserParams{Username: "sso", DisplayName: "SSO", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.OpenSession(ctx, httptest.NewRequest(http.MethodGet, "/", nil), u.ID, KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	userP, err := s.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := s.CreateAPIKey(ctx, userP, "k", ScopeFull)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, admin, admin.UserID); !errors.Is(err, ErrKeepUser) {
		t.Fatalf("deleting the local admin: %v", err)
	}
	if err := s.DeleteUser(ctx, admin, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("session after delete: %v", err)
	}
	if _, err := s.Key(ctx, key); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("key after delete: %v", err)
	}
	if err := s.DeleteUser(ctx, admin, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting again: %v", err)
	}
}
