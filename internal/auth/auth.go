// Package auth authenticates people and tools: the local admin's password,
// session cookies for the web UI and for qBittorrent clients (SID), and
// named API keys sent as Bearer tokens to either API.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/sync/singleflight"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Scope limits what an API key can do.
type Scope string

// API key scopes.
const (
	// ScopeFull allows everything the key's owner can do, on both APIs.
	ScopeFull Scope = "full"
	// ScopeQBittorrent allows only the qBittorrent API, for Radarr, Sonarr,
	// Prowlarr and other qBittorrent clients.
	ScopeQBittorrent Scope = "qbittorrent"
	// ScopeReadOnly allows only reads, on both APIs, and /metrics.
	ScopeReadOnly Scope = "read-only"
)

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool {
	return s == ScopeFull || s == ScopeQBittorrent || s == ScopeReadOnly
}

// Session kinds.
const (
	KindWeb         = "web"
	KindQBittorrent = "qbittorrent"
)

// Cookie names.
const (
	CookieWeb = "piecemeal_session"
	CookieSID = "SID"
)

// Principal is who made a request.
type Principal struct {
	UserID   int64  `json:"userId"`
	Username string `json:"username"`
	Name     string `json:"name"`
	IsAdmin  bool   `json:"isAdmin"`
	// Method is "session" or "apiKey".
	Method string `json:"method"`
	Scope  Scope  `json:"scope"`
	// SessionKind is web or qbittorrent for sessions.
	SessionKind string `json:"sessionKind,omitempty"`
	APIKeyID    int64  `json:"apiKeyId,omitempty"`
	APIKeyName  string `json:"apiKeyName,omitempty"`
	sessionHash []byte
}

// CanWrite reports whether the principal may change things.
func (p *Principal) CanWrite() bool { return p.Scope != ScopeReadOnly }

// Config configures the service.
type Config struct {
	// AdminUsername and AdminPasswordHash define the local admin. An empty
	// hash makes New generate a one-time password and log it.
	AdminUsername     string
	AdminPasswordHash string
	// LocalLogin allows password sign-in; turn it off once OIDC works.
	LocalLogin bool
	// SecureCookies sets the Secure attribute; turn it off only for plain
	// HTTP other than localhost.
	SecureCookies bool
	// SessionTTL is the absolute session lifetime; IdleTTL ends sessions
	// unused for that long.
	SessionTTL time.Duration
	IdleTTL    time.Duration
	// TrustedProxies may set X-Forwarded-For.
	TrustedProxies []netip.Prefix
	// TempPasswordOut receives the temporary password when no hash is
	// configured. Default: stderr.
	TempPasswordOut io.Writer
}

// Service authenticates requests.
type Service struct {
	cfg     Config
	store   *store.Store
	log     *slog.Logger
	limiter *loginLimiter
	// verifying bounds concurrent password verifications.
	verifying chan struct{}

	basicMu     sync.Mutex
	basicOK     map[[32]byte]time.Time
	basicSalt   []byte
	basicFlight singleflight.Group
	now         func() time.Time

	adminID int64

	touchMu sync.Mutex
	touched map[string]time.Time
}

// Errors.
var (
	ErrUnauthenticated = errors.New("not signed in")
	ErrBadCredentials  = errors.New("wrong username or password")
	ErrLockedOut       = errors.New("too many failed sign-ins; try again later")
	ErrLocalDisabled   = errors.New("password sign-in is turned off")
	ErrNotFound        = errors.New("not found")
)

// New creates the service and makes sure the local admin user exists.
func New(ctx context.Context, cfg Config, st *store.Store, log *slog.Logger) (*Service, error) {
	if cfg.AdminUsername == "" {
		cfg.AdminUsername = "admin"
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.IdleTTL == 0 {
		cfg.IdleTTL = 7 * 24 * time.Hour
	}
	if cfg.AdminPasswordHash == "" && cfg.LocalLogin {
		pw := randomToken(12)
		h, err := HashPassword(pw)
		if err != nil {
			return nil, err
		}
		cfg.AdminPasswordHash = h
		// Straight to stderr, not through the logger: log records are kept
		// in memory and served by the log endpoints.
		out := cfg.TempPasswordOut
		if out == nil {
			out = os.Stderr
		}
		_, _ = fmt.Fprintf(out, "\npiecemeal: temporary admin password for %q until restart: %s\n\n", cfg.AdminUsername, pw)
		log.Warn("no admin password hash is configured; a temporary password was printed to stderr. " +
			"Set PIECEMEAL_ADMIN_PASSWORD_HASH (see `piecemeal hash-password`).")
	} else if cfg.AdminPasswordHash != "" {
		if _, err := VerifyPassword(cfg.AdminPasswordHash, ""); errors.Is(err, ErrBadHash) {
			return nil, fmt.Errorf("admin password hash: %w", err)
		}
	}
	s := &Service{
		cfg: cfg, store: st, log: log, limiter: newLoginLimiter(5, 15*time.Minute), now: time.Now,
		verifying: make(chan struct{}, 2), basicOK: map[[32]byte]time.Time{}, basicSalt: []byte(randomToken(16)),
		touched: map[string]time.Time{},
	}
	u, err := st.GetUserByUsername(ctx, cfg.AdminUsername)
	switch {
	case store.IsNotFound(err):
		u, err = st.CreateUser(ctx, store.CreateUserParams{
			Username: cfg.AdminUsername, DisplayName: cfg.AdminUsername, IsAdmin: true, CreatedAt: s.now().Unix(),
		})
		if err != nil {
			return nil, fmt.Errorf("create admin user: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("load admin user: %w", err)
	case !u.IsAdmin:
		if err := st.UpdateUserProfile(ctx, store.UpdateUserProfileParams{
			ID: u.ID, DisplayName: u.DisplayName, Email: u.Email, IsAdmin: true,
		}); err != nil {
			return nil, err
		}
	}
	s.adminID = u.ID
	return s, nil
}

// Config returns the service's configuration.
func (s *Service) Config() Config { return s.cfg }

// Login checks the local admin's password and opens a session. It returns
// the session token to set as a cookie.
func (s *Service) Login(ctx context.Context, r *http.Request, username, password, kind string) (string, *Principal, error) {
	if err := s.checkPassword(ctx, r, username, password); err != nil {
		return "", nil, err
	}
	return s.OpenSession(ctx, r, s.adminID, kind)
}

// Basic authenticates HTTP Basic credentials, as qBittorrent 5.2 accepts
// them. Successes are remembered for a few minutes, so clients that send
// Basic on every request don't pay for a password hash each time.
func (s *Service) Basic(ctx context.Context, r *http.Request, username, password string) (*Principal, error) {
	// The cache key is a cheap argon2id hash with a per-process salt, so the
	// cache never holds anything a password could be recovered from.
	key := [32]byte(argon2.IDKey([]byte(username+"\x00"+password), s.basicSalt, 1, 4*1024, 1, 32))
	now := s.now()
	s.basicMu.Lock()
	exp, ok := s.basicOK[key]
	s.basicMu.Unlock()
	if !ok || now.After(exp) {
		// Parallel requests with the same credentials share one check, so a
		// burst of correct ones can't trip the lockout.
		_, err, _ := s.basicFlight.Do(string(key[:]), func() (any, error) {
			if err := s.checkPassword(ctx, r, username, password); err != nil {
				return nil, err
			}
			s.basicMu.Lock()
			if len(s.basicOK) > 1000 {
				clear(s.basicOK)
			}
			s.basicOK[key] = now.Add(5 * time.Minute)
			s.basicMu.Unlock()
			return nil, nil
		})
		if err != nil {
			return nil, err
		}
	}
	u, err := s.store.GetUser(ctx, s.adminID)
	if err != nil {
		return nil, err
	}
	return &Principal{
		UserID: u.ID, Username: u.Username, Name: u.DisplayName, IsAdmin: u.IsAdmin, Method: "basic", Scope: ScopeFull,
	}, nil
}

// checkPassword verifies the local admin's credentials, with the lockout.
func (s *Service) checkPassword(ctx context.Context, r *http.Request, username, password string) error {
	ip := s.ClientIP(r)
	if !s.cfg.LocalLogin {
		return ErrLocalDisabled
	}
	// The attempt counts as a failure until it succeeds, so parallel
	// guesses can't all pass the check before any of them is recorded.
	if !s.limiter.begin(ip, s.now()) {
		s.audit(ctx, username, "login.lockedOut", "ip="+ip)
		return ErrLockedOut
	}
	// Each verification takes 64 MiB; bound how many run at once.
	select {
	case s.verifying <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.cfg.AdminUsername)) == 1
	pwOK, err := VerifyPassword(s.cfg.AdminPasswordHash, password)
	<-s.verifying
	if err != nil {
		return err
	}
	if !userOK || !pwOK {
		s.audit(ctx, username, "login.failed", "ip="+ip)
		return ErrBadCredentials
	}
	s.limiter.reset(ip)
	return nil
}

// OpenSession starts a session for a user who has signed in.
func (s *Service) OpenSession(ctx context.Context, r *http.Request, userID int64, kind string) (string, *Principal, error) {
	u, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return "", nil, fmt.Errorf("load user: %w", err)
	}
	token := randomToken(32)
	now := s.now()
	ua := r.UserAgent()
	if len(ua) > 256 {
		ua = ua[:256]
	}
	err = s.store.CreateSession(ctx, store.CreateSessionParams{
		TokenHash: hashToken(token), UserID: u.ID, Kind: kind, CreatedAt: now.Unix(),
		ExpiresAt: now.Add(s.cfg.SessionTTL).Unix(), Ip: s.ClientIP(r), UserAgent: ua,
	})
	if err != nil {
		return "", nil, fmt.Errorf("create session: %w", err)
	}
	if err := s.store.SetUserLastLogin(ctx, store.SetUserLastLoginParams{At: sql.NullInt64{Int64: now.Unix(), Valid: true}, ID: u.ID}); err != nil {
		s.log.Warn("recording sign-in time", "error", err)
	}
	s.audit(ctx, u.Username, "login", "kind="+kind+" ip="+s.ClientIP(r))
	return token, &Principal{
		UserID: u.ID, Username: u.Username, Name: u.DisplayName, IsAdmin: u.IsAdmin, Method: "session",
		Scope: ScopeFull, SessionKind: kind,
	}, nil
}

// Logout ends the session the request was made with, if any.
func (s *Service) Logout(ctx context.Context, p *Principal) error {
	if p == nil || p.sessionHash == nil {
		return nil
	}
	return s.store.DeleteSession(ctx, p.sessionHash)
}

// Authenticate identifies the request's principal from a Bearer API key or
// a session cookie. cookie is the cookie name to look for.
func (s *Service) Authenticate(r *http.Request, cookie string) (*Principal, error) {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, ok := strings.Cut(h, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return nil, ErrUnauthenticated
		}
		return s.authenticateKey(ctx, strings.TrimSpace(token))
	}
	c, err := r.Cookie(cookie)
	if err != nil || c.Value == "" {
		return nil, ErrUnauthenticated
	}
	return s.authenticateSession(ctx, c.Value)
}

// Session authenticates a session token from a cookie.
func (s *Service) Session(ctx context.Context, token string) (*Principal, error) {
	return s.authenticateSession(ctx, token)
}

// Key authenticates an API key.
func (s *Service) Key(ctx context.Context, key string) (*Principal, error) {
	return s.authenticateKey(ctx, key)
}

func (s *Service) authenticateSession(ctx context.Context, token string) (*Principal, error) {
	h := hashToken(token)
	sess, err := s.store.GetSession(ctx, h)
	if store.IsNotFound(err) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if now.Unix() >= sess.ExpiresAt || now.Sub(time.Unix(sess.LastSeenAt, 0)) >= s.cfg.IdleTTL {
		_ = s.store.DeleteSession(ctx, h)
		return nil, ErrUnauthenticated
	}
	u, err := s.store.GetUser(ctx, sess.UserID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	s.touch(ctx, h, now)
	return &Principal{
		UserID: u.ID, Username: u.Username, Name: u.DisplayName, IsAdmin: u.IsAdmin, Method: "session",
		Scope: ScopeFull, SessionKind: sess.Kind, sessionHash: h,
	}, nil
}

// touch records session use at most once a minute, so requests don't each
// write to the database.
func (s *Service) touch(ctx context.Context, h []byte, now time.Time) {
	key := string(h)
	s.touchMu.Lock()
	last, ok := s.touched[key]
	if ok && now.Sub(last) < time.Minute {
		s.touchMu.Unlock()
		return
	}
	s.touched[key] = now
	if len(s.touched) > 10000 {
		clear(s.touched)
	}
	s.touchMu.Unlock()
	if err := s.store.TouchSession(ctx, store.TouchSessionParams{At: now.Unix(), TokenHash: h}); err != nil {
		s.log.Warn("touching session", "error", err)
	}
}

const keyPrefix = "pm_"

func (s *Service) authenticateKey(ctx context.Context, key string) (*Principal, error) {
	if !strings.HasPrefix(key, keyPrefix) {
		return nil, ErrUnauthenticated
	}
	k, err := s.store.GetAPIKeyByHash(ctx, hashToken(key))
	if store.IsNotFound(err) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	u, err := s.store.GetUser(ctx, k.UserID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	now := s.now()
	if !k.LastUsedAt.Valid || now.Unix()-k.LastUsedAt.Int64 >= 60 {
		if err := s.store.TouchAPIKey(ctx, store.TouchAPIKeyParams{At: sql.NullInt64{Int64: now.Unix(), Valid: true}, ID: k.ID}); err != nil {
			s.log.Warn("touching API key", "error", err)
		}
	}
	return &Principal{
		UserID: u.ID, Username: u.Username, Name: u.DisplayName, IsAdmin: u.IsAdmin, Method: "apiKey",
		Scope: Scope(k.Scope), APIKeyID: k.ID, APIKeyName: k.Name,
	}, nil
}

// APIKey is an API key without its secret.
type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix" doc:"The first characters of the key, to tell keys apart."`
	Scope      Scope      `json:"scope" enum:"full,qbittorrent,read-only"`
	UserID     int64      `json:"userId"`
	Username   string     `json:"username" doc:"The owner's username."`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

func apiKeyFrom(k store.ApiKey) APIKey {
	out := APIKey{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, Scope: Scope(k.Scope), UserID: k.UserID,
		CreatedAt: time.Unix(k.CreatedAt, 0).UTC(),
	}
	if k.LastUsedAt.Valid {
		t := time.Unix(k.LastUsedAt.Int64, 0).UTC()
		out.LastUsedAt = &t
	}
	return out
}

// CreateAPIKey creates a key for a user and returns it with its secret,
// which is not stored and can't be shown again.
func (s *Service) CreateAPIKey(ctx context.Context, by *Principal, name string, scope Scope) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return APIKey{}, "", errors.New("name must be 1 to 100 characters")
	}
	if !scope.Valid() {
		return APIKey{}, "", fmt.Errorf("unknown scope %q", scope)
	}
	secret := keyPrefix + randomToken(32)
	k, err := s.store.CreateAPIKey(ctx, store.CreateAPIKeyParams{
		Name: name, Prefix: secret[:len(keyPrefix)+6], KeyHash: hashToken(secret), Scope: string(scope),
		UserID: by.UserID, CreatedAt: s.now().Unix(),
	})
	if err != nil {
		return APIKey{}, "", fmt.Errorf("create API key: %w", err)
	}
	s.audit(ctx, by.Username, "apiKey.create", fmt.Sprintf("id=%d name=%q scope=%s", k.ID, name, scope))
	key := apiKeyFrom(k)
	key.Username = by.Username
	return key, secret, nil
}

// ListAPIKeys lists keys: every key for admins, otherwise the caller's own.
func (s *Service) ListAPIKeys(ctx context.Context, by *Principal) ([]APIKey, error) {
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	names, err := s.usernames(ctx)
	if err != nil {
		return nil, err
	}
	out := []APIKey{}
	for _, k := range keys {
		if by.IsAdmin || k.UserID == by.UserID {
			key := apiKeyFrom(k)
			key.Username = names[k.UserID]
			out = append(out, key)
		}
	}
	return out, nil
}

// DeleteAPIKey deletes a key the caller owns, or any key for admins.
func (s *Service) DeleteAPIKey(ctx context.Context, by *Principal, id int64) error {
	keys, err := s.ListAPIKeys(ctx, by)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(keys, func(k APIKey) bool { return k.ID == id }) {
		return ErrNotFound
	}
	if _, err := s.store.DeleteAPIKey(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, by.Username, "apiKey.delete", fmt.Sprintf("id=%d", id))
	return nil
}

// Session is an open session.
type Session struct {
	ID         string    `json:"id"`
	UserID     int64     `json:"userId"`
	Username   string    `json:"username"`
	Kind       string    `json:"kind" enum:"web,qbittorrent"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"userAgent"`
	Current    bool      `json:"current" doc:"The session this request was made with."`
}

// ListSessions lists sessions: all for admins, otherwise the caller's.
func (s *Service) ListSessions(ctx context.Context, by *Principal) ([]Session, error) {
	var rows []store.Session
	var err error
	if by.IsAdmin {
		rows, err = s.store.ListSessions(ctx)
	} else {
		rows, err = s.store.ListUserSessions(ctx, by.UserID)
	}
	if err != nil {
		return nil, err
	}
	names, err := s.usernames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, Session{
			ID: sessionID(r.TokenHash), UserID: r.UserID, Username: names[r.UserID], Kind: r.Kind, CreatedAt: time.Unix(r.CreatedAt, 0).UTC(),
			LastSeenAt: time.Unix(r.LastSeenAt, 0).UTC(), ExpiresAt: time.Unix(r.ExpiresAt, 0).UTC(), IP: r.Ip,
			UserAgent: r.UserAgent, Current: subtle.ConstantTimeCompare(r.TokenHash, by.sessionHash) == 1,
		})
	}
	return out, nil
}

// DeleteSession ends a session by ID: any for admins, otherwise the
// caller's own.
func (s *Service) DeleteSession(ctx context.Context, by *Principal, id string) error {
	sessions, err := s.ListSessions(ctx, by)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess.ID != id {
			continue
		}
		h, _ := hex.DecodeString(id)
		if _, err := s.store.DeleteSessionForUser(ctx, store.DeleteSessionForUserParams{TokenHash: h, UserID: sess.UserID}); err != nil {
			return err
		}
		s.audit(ctx, by.Username, "session.delete", "user="+fmt.Sprint(sess.UserID))
		return nil
	}
	return ErrNotFound
}

// PruneSessions deletes expired and idle sessions.
func (s *Service) PruneSessions(ctx context.Context) error {
	now := s.now()
	_, err := s.store.DeleteExpiredSessions(ctx, store.DeleteExpiredSessionsParams{
		Now: now.Unix(), IdleBefore: now.Add(-s.cfg.IdleTTL).Unix(),
	})
	return err
}

// Cookie returns a session cookie. An empty token with maxAge -1 deletes
// it. qBittorrent clients expect the name SID.
func (s *Service) Cookie(name, token string, maxAge int) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure is configurable, for plain-HTTP setups
		Name: name, Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	}
}

// SessionMaxAge is the session cookie's Max-Age in seconds.
func (s *Service) SessionMaxAge() int { return int(s.cfg.SessionTTL / time.Second) }

// ClientIP returns the request's client address, honouring
// X-Forwarded-For from trusted proxies.
func (s *Service) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if !s.trusted(addr) {
		return addr.Unmap().String()
	}
	// Walk X-Forwarded-For from the right, skipping trusted proxies.
	xff := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(xff) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(xff[i]))
		if err != nil {
			break
		}
		if !s.trusted(a) {
			return a.Unmap().String()
		}
	}
	return addr.Unmap().String()
}

func (s *Service) trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Audit records an event in the audit log.
func (s *Service) Audit(ctx context.Context, actor, action, detail string) {
	s.audit(ctx, actor, action, detail)
}

func (s *Service) audit(ctx context.Context, actor, action, detail string) {
	if err := s.store.InsertAudit(ctx, store.InsertAuditParams{
		At: s.now().Unix(), Actor: actor, Action: action, Detail: detail,
	}); err != nil {
		s.log.Warn("writing audit log", "error", err)
	}
}

// AuditEntry is one audit log entry.
type AuditEntry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// ListAudit returns up to limit entries older than before (0 for newest).
func (s *Service) ListAudit(ctx context.Context, before int64, limit int) ([]AuditEntry, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.store.ListAudit(ctx, store.ListAuditParams{Before: before, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, len(rows))
	for i, r := range rows {
		out[i] = AuditEntry{ID: r.ID, At: time.Unix(r.At, 0).UTC(), Actor: r.Actor, Action: r.Action, Detail: r.Detail}
	}
	return out, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

func sessionID(h []byte) string { return hex.EncodeToString(h) }

// loginLimiter locks an address out after too many failed sign-ins.
type loginLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
}

func newLoginLimiter(maxFails int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: maxFails, window: window, fails: map[string][]time.Time{}}
}

func (l *loginLimiter) recent(ip string, now time.Time) []time.Time {
	f := slices.DeleteFunc(l.fails[ip], func(t time.Time) bool { return now.Sub(t) >= l.window })
	if len(f) == 0 {
		delete(l.fails, ip)
	} else {
		l.fails[ip] = f
	}
	return f
}

// begin records an attempt and reports whether it may go ahead.
func (l *loginLimiter) begin(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.recent(ip, now)
	if len(f) >= l.max {
		return false
	}
	l.fails[ip] = append(f, now)
	if len(l.fails) > 100000 {
		clear(l.fails)
	}
	return true
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

// usernames maps user IDs to usernames.
func (s *Service) usernames(ctx context.Context) (map[int64]string, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(users))
	for _, u := range users {
		m[u.ID] = u.Username
	}
	return m, nil
}

// Users lists every user.
func (s *Service) Users(ctx context.Context) ([]store.User, error) {
	return s.store.ListUsers(ctx)
}

// ClientKey identifies who is calling, for state kept per client such as
// qBittorrent's sync: the session, the API key, or the Basic user.
func (p *Principal) ClientKey() string {
	switch {
	case p.sessionHash != nil:
		return "s:" + sessionID(p.sessionHash)
	case p.APIKeyID != 0:
		return fmt.Sprintf("k:%d", p.APIKeyID)
	default:
		return "u:" + p.Username
	}
}
