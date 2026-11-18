// Package api is piecemeal's native REST API under /api/piecemeal/v1,
// built with Huma. The OpenAPI document is generated from these handlers
// and committed as api/openapi.json; the web UI's client is generated from
// that file.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/logbuf"
	"github.com/sebdanielsson/piecemeal/internal/natpmp"
	"github.com/sebdanielsson/piecemeal/internal/rss"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Prefix is where the API is served.
const Prefix = "/api/piecemeal/v1"

// CSRFHeader must be sent with unsafe requests authenticated by a session
// cookie. Browsers can't send custom headers cross-origin without a CORS
// preflight, which piecemeal never allows.
const CSRFHeader = "X-Requested-With"

// Deps are the services the API uses.
type Deps struct {
	Version   string
	StartedAt time.Time
	Engine    *engine.Engine
	Settings  *settings.Manager
	Auth      *auth.Service
	Fetcher   *fetch.Fetcher
	Logs      *logbuf.Buffer
	// NATPMP is nil when port forwarding isn't configured.
	NATPMP *natpmp.Mapper
	// OIDC is nil when OIDC sign-in isn't configured.
	OIDC *auth.OIDC
	RSS  *rss.Manager
	// Store is for backups.
	Store *store.Store
	Log   *slog.Logger
	// System is static information for GET /system.
	System SystemInfo
}

// API is the native API.
type API struct {
	Deps
	huma huma.API
	// done ends the event streams, which would otherwise keep a graceful
	// HTTP shutdown waiting.
	done      chan struct{}
	closeOnce sync.Once
}

// Close ends open event streams. Call it when the HTTP server starts
// shutting down (http.Server.RegisterOnShutdown).
func (a *API) Close() {
	a.closeOnce.Do(func() { close(a.done) })
}

type access int

const (
	accessPublic access = iota
	accessUser
	accessAdmin
)

type principalKey struct{}

// Principal returns the authenticated principal of a request handled by the
// API.
func Principal(ctx context.Context) *auth.Principal {
	p, _ := ctx.Value(principalKey{}).(*auth.Principal)
	return p
}

// New registers the API's routes on mux.
func New(mux *http.ServeMux, d Deps) *API {
	cfg := huma.DefaultConfig("piecemeal", d.Version)
	// No $schema links in responses.
	cfg.CreateHooks = nil
	cfg.OpenAPIPath = "/openapi"
	cfg.DocsPath = "/docs"
	cfg.SchemasPath = "/schemas"
	cfg.Info.Description = "piecemeal's native API. The web UI uses it, and so can anything else: " +
		"authenticate with a session cookie (send `X-Requested-With: piecemeal` on unsafe requests) " +
		"or an API key as a Bearer token."
	cfg.Info.License = &huma.License{Name: "MIT", Identifier: "MIT"}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer":  {Type: "http", Scheme: "bearer", Description: "An API key, `pm_...`."},
		"session": {Type: "apiKey", In: "cookie", Name: auth.CookieWeb},
	}
	a := &API{Deps: d, done: make(chan struct{})}
	a.huma = humago.NewWithPrefix(mux, Prefix, cfg)
	a.huma.UseMiddleware(a.authMiddleware)

	a.registerAuth()
	a.registerOIDC(mux)
	a.registerTorrents()
	a.registerCategories()
	a.registerRSS()
	a.registerSystem()
	a.registerAdmin()
	a.registerBackup()
	a.registerEvents()
	return a
}

// OpenAPI returns the API description.
func (a *API) OpenAPI() *huma.OpenAPI { return a.huma.OpenAPI() }

func (a *API) operation(method, path, id, summary, tag string, acc access) huma.Operation {
	op := huma.Operation{
		OperationID: id,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Tags:        []string{tag},
		Metadata:    map[string]any{"access": acc},
	}
	if acc != accessPublic {
		op.Security = []map[string][]string{{"bearer": {}}, {"session": {}}}
		op.Errors = []int{http.StatusUnauthorized, http.StatusForbidden}
	}
	if acc == accessAdmin {
		op.Description = "Admins only."
	}
	return op
}

func (a *API) authMiddleware(ctx huma.Context, next func(huma.Context)) {
	acc, _ := ctx.Operation().Metadata["access"].(access)
	r, _ := humago.Unwrap(ctx)
	p, err := a.Auth.Authenticate(r, auth.CookieWeb)
	if err != nil && !errors.Is(err, auth.ErrUnauthenticated) {
		a.Log.Error("authenticating request", "error", err)
		_ = huma.WriteErr(a.huma, ctx, http.StatusInternalServerError, "authentication failed")
		return
	}
	if p != nil {
		ctx = huma.WithValue(ctx, principalKey{}, p)
	}
	if acc == accessPublic {
		next(ctx)
		return
	}
	if p == nil {
		_ = huma.WriteErr(a.huma, ctx, http.StatusUnauthorized, "sign in or send an API key")
		return
	}
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case p.Scope == auth.ScopeQBittorrent:
		_ = huma.WriteErr(a.huma, ctx, http.StatusForbidden, "this API key is for the qBittorrent API only")
		return
	case p.Scope == auth.ScopeReadOnly && !safe:
		_ = huma.WriteErr(a.huma, ctx, http.StatusForbidden, "this API key is read-only")
		return
	case acc == accessAdmin && (!p.IsAdmin || p.Scope != auth.ScopeFull):
		_ = huma.WriteErr(a.huma, ctx, http.StatusForbidden, "admins only, signed in or with a full-scope key")
		return
	}
	if p.Method == "session" && !safe {
		if err := checkCSRF(r); err != nil {
			_ = huma.WriteErr(a.huma, ctx, http.StatusForbidden, err.Error())
			return
		}
	}
	next(ctx)
}

// checkCSRF requires the custom header and, when the browser sends an
// Origin, that it is this host.
func checkCSRF(r *http.Request) error {
	if r.Header.Get(CSRFHeader) == "" {
		return errors.New("missing " + CSRFHeader + " header")
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			return errors.New("cross-origin request refused")
		}
	}
	return nil
}

// apiError maps service errors to HTTP problems.
func apiError(err error) error {
	if err == nil {
		return nil
	}
	var ve *settings.ValidationError
	switch {
	case errors.Is(err, engine.ErrNotFound), errors.Is(err, auth.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, engine.ErrExists):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, engine.ErrInvalid), errors.Is(err, engine.ErrPathForbidden), errors.As(err, &ve):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, engine.ErrClosed):
		return huma.Error503ServiceUnavailable(err.Error())
	case errors.Is(err, auth.ErrBadCredentials), errors.Is(err, auth.ErrUnauthenticated):
		return huma.Error401Unauthorized(err.Error())
	case errors.Is(err, auth.ErrLockedOut):
		return huma.Error429TooManyRequests(err.Error())
	case errors.Is(err, auth.ErrLocalDisabled):
		return huma.Error403Forbidden(err.Error())
	default:
		return err
	}
}

func actor(ctx context.Context) string {
	if p := Principal(ctx); p != nil {
		if p.APIKeyID != 0 {
			return fmt.Sprintf("%s (API key %d)", p.Username, p.APIKeyID)
		}
		return p.Username
	}
	return "anonymous"
}
