package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

// OIDCConfig configures sign-in through an OpenID Connect provider such as
// Pocket ID.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is this server's callback URL, registered at the provider.
	RedirectURL string
	// Name labels the sign-in button.
	Name string
	// GroupsClaim names the ID token claim with the user's groups.
	GroupsClaim string
	// AllowedGroups, when set, limits sign-in to members of these groups.
	AllowedGroups []string
	// AdminGroups make their members admins; everyone else isn't.
	AdminGroups []string
}

// Enabled reports whether OIDC is configured.
func (c OIDCConfig) Enabled() bool { return c.Issuer != "" && c.ClientID != "" }

// OIDC signs people in with an OpenID Connect provider: authorization code
// flow with PKCE, state and nonce. Provider discovery happens on first use
// and is retried, so a provider that is down at startup doesn't stop
// piecemeal, and the local admin can still sign in.
type OIDC struct {
	cfg OIDCConfig
	svc *Service

	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config

	// seal encrypts sign-ins in progress into the browser's state cookie,
	// so they take no memory on the server until they finish.
	seal cipher.AEAD
}

// oidcFlow is a sign-in in progress, sealed into the state cookie.
type oidcFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
	Expires  int64  `json:"e"`
}

// flowLifetime is how long a sign-in may take.
const flowLifetime = 10 * time.Minute

// Errors from the OIDC flow.
var (
	ErrOIDCDisabled = errors.New("OIDC sign-in is not configured")
	ErrOIDCState    = errors.New("the sign-in attempt expired or was not started here; try again")
	ErrNotAllowed   = errors.New("your account is not in a group allowed to use piecemeal")
)

// NewOIDC creates the OIDC sign-in for svc.
func NewOIDC(cfg OIDCConfig, svc *Service) *OIDC {
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	if cfg.Name == "" {
		cfg.Name = "SSO"
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	block, _ := aes.NewCipher(key)
	seal, _ := cipher.NewGCM(block)
	return &OIDC{cfg: cfg, svc: svc, seal: seal}
}

func (o *OIDC) sealFlow(f oidcFlow) string {
	plain, _ := json.Marshal(f)
	nonce := make([]byte, o.seal.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(o.seal.Seal(nonce, nonce, plain, nil))
}

func (o *OIDC) openFlow(sealed string) (oidcFlow, bool) {
	b, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(b) < o.seal.NonceSize() {
		return oidcFlow{}, false
	}
	plain, err := o.seal.Open(nil, b[:o.seal.NonceSize()], b[o.seal.NonceSize():], nil)
	if err != nil {
		return oidcFlow{}, false
	}
	var f oidcFlow
	if json.Unmarshal(plain, &f) != nil || time.Now().Unix() > f.Expires {
		return oidcFlow{}, false
	}
	return f, true
}

// SafeNext keeps a path to return to after signing in only when it stays
// on this site: it starts with one /, and has no backslash, whitespace or
// control character, which browsers would drop or reinterpret, turning
// "/\t/evil.example" into "//evil.example".
func SafeNext(next string) string {
	bad := func(r rune) bool { return r <= ' ' || r == 0x7f || r == '\\' }
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsFunc(next, bad) {
		return "/"
	}
	return next
}

// Config returns the configuration.
func (o *OIDC) Config() OIDCConfig { return o.cfg }

func (o *OIDC) setup(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil {
		return o.oauth, o.verifier, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, o.cfg.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("OIDC provider %s: %w", o.cfg.Issuer, err)
	}
	scopes := []string{oidc.ScopeOpenID, "profile", "email"}
	if len(o.cfg.AllowedGroups) > 0 || len(o.cfg.AdminGroups) > 0 {
		scopes = append(scopes, "groups")
	}
	o.provider = p
	o.verifier = p.Verifier(&oidc.Config{ClientID: o.cfg.ClientID})
	o.oauth = &oauth2.Config{
		ClientID: o.cfg.ClientID, ClientSecret: o.cfg.ClientSecret, RedirectURL: o.cfg.RedirectURL,
		Endpoint: p.Endpoint(), Scopes: scopes,
	}
	return o.oauth, o.verifier, nil
}

// Begin starts a sign-in and returns the provider URL to redirect to and
// the sealed flow, which the caller keeps in a cookie: it binds the
// sign-in to the browser and holds its state, nonce and PKCE verifier, so
// nothing is kept on the server. next is where to go afterwards; only
// same-site paths are kept.
func (o *OIDC) Begin(ctx context.Context, next string) (redirect, sealed string, err error) {
	oc, _, err := o.setup(ctx)
	if err != nil {
		return "", "", err
	}
	f := oidcFlow{
		State: randomToken(24), Nonce: randomToken(24), Verifier: oauth2.GenerateVerifier(), Next: SafeNext(next),
		Expires: time.Now().Add(flowLifetime).Unix(),
	}
	return oc.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), o.sealFlow(f), nil
}

// Identity is what the provider says about who signed in.
type Identity struct {
	Subject  string
	Username string
	Name     string
	Email    string
	Groups   []string
}

// Finish completes a sign-in: it checks the state against the one the
// browser started with, exchanges the code, verifies the ID token and its
// nonce, and returns the user and where to go next.
func (o *OIDC) Finish(ctx context.Context, cookieState, state, code string) (store.User, string, error) {
	flow, ok := o.openFlow(cookieState)
	if !ok || state == "" || subtle.ConstantTimeCompare([]byte(flow.State), []byte(state)) != 1 {
		return store.User{}, "", ErrOIDCState
	}
	oc, verifier, err := o.setup(ctx)
	if err != nil {
		return store.User{}, "", err
	}
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return store.User{}, "", fmt.Errorf("exchanging the code: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return store.User{}, "", errors.New("the provider returned no ID token")
	}
	idt, err := verifier.Verify(ctx, raw)
	if err != nil {
		return store.User{}, "", fmt.Errorf("verifying the ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(flow.Nonce)) != 1 {
		return store.User{}, "", errors.New("the ID token's nonce doesn't match")
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return store.User{}, "", err
	}
	id := identityFrom(idt.Subject, claims, o.cfg.GroupsClaim)
	if len(o.cfg.AllowedGroups) > 0 && !intersects(id.Groups, o.cfg.AllowedGroups) {
		o.svc.audit(ctx, id.Username, "login.oidc.denied", "subject="+id.Subject)
		return store.User{}, "", ErrNotAllowed
	}
	u, err := o.upsertUser(ctx, idt.Issuer, id)
	if err != nil {
		return store.User{}, "", err
	}
	return u, flow.Next, nil
}

func identityFrom(subject string, claims map[string]any, groupsClaim string) Identity {
	str := func(k string) string {
		s, _ := claims[k].(string)
		return strings.TrimSpace(s)
	}
	id := Identity{Subject: subject, Name: str("name"), Email: str("email"), Username: str("preferred_username")}
	if id.Username == "" {
		id.Username, _, _ = strings.Cut(id.Email, "@")
	}
	if id.Username == "" {
		id.Username = "user-" + subject
	}
	switch g := claims[groupsClaim].(type) {
	case []any:
		for _, v := range g {
			if s, ok := v.(string); ok {
				id.Groups = append(id.Groups, s)
			}
		}
	case string:
		id.Groups = strings.Fields(strings.ReplaceAll(g, ",", " "))
	}
	return id
}

func intersects(a, b []string) bool {
	return slices.ContainsFunc(a, func(s string) bool { return slices.Contains(b, s) })
}

// upsertUser finds the user by issuer and subject, creating them on first
// sign-in, and refreshes their profile and admin flag from the provider.
func (o *OIDC) upsertUser(ctx context.Context, issuer string, id Identity) (store.User, error) {
	admin := intersects(id.Groups, o.cfg.AdminGroups)
	name := id.Name
	if name == "" {
		name = id.Username
	}
	u, err := o.svc.store.GetUserByOIDC(ctx, store.GetUserByOIDCParams{
		Issuer: nullString(issuer), Subject: nullString(id.Subject),
	})
	if err == nil {
		if err := o.svc.store.UpdateUserProfile(ctx, store.UpdateUserProfileParams{
			ID: u.ID, DisplayName: name, Email: id.Email, IsAdmin: admin,
		}); err != nil {
			return store.User{}, err
		}
		u.DisplayName, u.Email, u.IsAdmin = name, id.Email, admin
		return u, nil
	}
	if !store.IsNotFound(err) {
		return store.User{}, err
	}
	// Usernames are unique; the local admin's, or another provider user's,
	// may already be taken.
	username := id.Username
	for i := 2; ; i++ {
		if _, err := o.svc.store.GetUserByUsername(ctx, username); store.IsNotFound(err) {
			break
		} else if err != nil {
			return store.User{}, err
		}
		username = fmt.Sprintf("%s-%d", id.Username, i)
	}
	u, err = o.svc.store.CreateUser(ctx, store.CreateUserParams{
		Username: username, DisplayName: name, Email: id.Email, OidcIssuer: nullString(issuer),
		OidcSubject: nullString(id.Subject), IsAdmin: admin, CreatedAt: o.svc.now().Unix(),
	})
	if err != nil {
		return store.User{}, fmt.Errorf("create user: %w", err)
	}
	o.svc.audit(ctx, username, "user.create", "oidc subject="+id.Subject)
	return u, nil
}

// oidcStateCookie keeps the state of a sign-in in progress.
const oidcStateCookie = "piecemeal_oidc_state"

// StateCookieName is the cookie that binds an OIDC sign-in to the browser
// that started it.
func StateCookieName() string { return oidcStateCookie }

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
