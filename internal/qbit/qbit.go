// Package qbit serves the qBittorrent WebAPI v2 under /api/v2 on top of the
// engine, so Radarr, Sonarr, Prowlarr and other qBittorrent clients can use
// piecemeal unchanged.
//
// It is written from qBittorrent's API documentation and observed
// behaviour, not from qBittorrent's source code. docs/qbittorrent-compat.md
// lists what is implemented.
package qbit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/settings"
)

// The qBittorrent and WebAPI versions piecemeal reports. Clients choose
// code paths by version; these are the versions whose behaviour piecemeal
// follows on the endpoints it implements.
const (
	AppVersion    = "v5.3.0"
	WebAPIVersion = "2.16.2"
)

// Server is the qBittorrent API.
type Server struct {
	engine   *engine.Engine
	settings *settings.Manager
	auth     *auth.Service
	fetcher  *fetch.Fetcher
	log      *slog.Logger
	// port is the HTTP listen port, which qBittorrent puts in its session
	// cookie's name.
	port   int
	routes map[string]route
}

// Deps are the services the API uses.
type Deps struct {
	Engine   *engine.Engine
	Settings *settings.Manager
	Auth     *auth.Service
	Fetcher  *fetch.Fetcher
	Log      *slog.Logger
	// HTTPPort is the port piecemeal serves HTTP on.
	HTTPPort int
}

type route struct {
	method string
	// public routes need no authentication.
	public bool
	h      func(w http.ResponseWriter, r *http.Request)
}

// Prefix is where the API is served.
const Prefix = "/api/v2/"

// New creates the API.
func New(d Deps) *Server {
	s := &Server{
		engine: d.Engine, settings: d.Settings, auth: d.Auth, fetcher: d.Fetcher, log: d.Log, port: d.HTTPPort,
	}
	get, post := http.MethodGet, http.MethodPost
	s.routes = map[string]route{
		"auth/login":  {post, true, s.login},
		"auth/logout": {post, false, s.logout},

		"app/version":         {get, false, s.text(AppVersion)},
		"app/webapiVersion":   {get, false, s.text(WebAPIVersion)},
		"app/buildInfo":       {get, false, s.buildInfo},
		"app/preferences":     {get, false, s.preferences},
		"app/defaultSavePath": {get, false, s.defaultSavePath},

		"transfer/info": {get, false, s.transferInfo},

		"torrents/info":        {get, false, s.info},
		"torrents/properties":  {get, false, s.properties},
		"torrents/files":       {get, false, s.files},
		"torrents/trackers":    {get, false, s.trackers},
		"torrents/pieceStates": {get, false, s.pieceStates},
		"torrents/categories":  {get, false, s.categories},
		"torrents/tags":        {get, false, s.tags},

		"torrents/add":                      {post, false, s.add},
		"torrents/delete":                   {post, false, s.delete},
		"torrents/start":                    {post, false, s.hashesAction(s.engine.Start)},
		"torrents/stop":                     {post, false, s.hashesAction(s.engine.Stop)},
		"torrents/resume":                   {post, false, s.hashesAction(s.engine.Start)},
		"torrents/pause":                    {post, false, s.hashesAction(s.engine.Stop)},
		"torrents/recheck":                  {post, false, s.hashesAction(s.engine.Recheck)},
		"torrents/reannounce":               {post, false, s.hashesAction(s.engine.Reannounce)},
		"torrents/setForceStart":            {post, false, s.setForceStart},
		"torrents/setShareLimits":           {post, false, s.setShareLimits},
		"torrents/setDownloadLimit":         {post, false, s.setLimit(true)},
		"torrents/setUploadLimit":           {post, false, s.setLimit(false)},
		"torrents/toggleSequentialDownload": {post, false, s.toggle(true)},
		"torrents/toggleFirstLastPiecePrio": {post, false, s.toggle(false)},
		"torrents/filePrio":                 {post, false, s.filePrio},
		"torrents/rename":                   {post, false, s.rename},
		"torrents/topPrio":                  {post, false, s.queueing},
		"torrents/bottomPrio":               {post, false, s.queueing},
		"torrents/increasePrio":             {post, false, s.queueing},
		"torrents/decreasePrio":             {post, false, s.queueing},
		"torrents/setCategory":              {post, false, s.setCategory},
		"torrents/createCategory":           {post, false, s.createCategory},
		"torrents/editCategory":             {post, false, s.editCategory},
		"torrents/removeCategories":         {post, false, s.removeCategories},
		"torrents/addTags":                  {post, false, s.addTags},
		"torrents/removeTags":               {post, false, s.removeTags},
		"torrents/createTags":               {post, false, s.createTags},
		"torrents/deleteTags":               {post, false, s.deleteTags},
	}
	return s
}

// ServeHTTP serves the routes under Prefix.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, Prefix)
	rt, ok := s.routes[name]
	if !ok {
		http.Error(w, "Endpoint does not exist", http.StatusNotFound)
		return
	}
	if r.Method != rt.method {
		w.Header().Set("Allow", rt.method)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if rt.public {
		rt.h(w, r)
		return
	}
	p, status := s.authenticate(r)
	if p == nil {
		http.Error(w, http.StatusText(status), status)
		return
	}
	if r.Method != http.MethodGet && !p.CanWrite() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	r2 := r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
	rt.h(w, r2)
	// net/http only cleans up a multipart form parsed on the request it
	// passed in, not on this copy: remove spilled temp files here.
	if r2.MultipartForm != nil {
		_ = r2.MultipartForm.RemoveAll()
	}
}

type principalKey struct{}

// cookieNames are the session cookies accepted, newest first: qBittorrent
// 5.2 names it after the WebUI port, earlier versions SID. The web UI's own
// session works too.
func (s *Server) cookieNames() []string {
	return []string{s.cookieName(), auth.CookieSID, auth.CookieWeb}
}

func (s *Server) cookieName() string {
	return "QBT_SID_" + strconv.Itoa(s.port)
}

// authenticate returns the principal, or nil and the status to answer.
// qBittorrent answers 403 to anything unauthenticated, and the *arrs rely on
// that: they log in again after a 403 but give up on a 401.
func (s *Server) authenticate(r *http.Request) (*auth.Principal, int) {
	ctx := r.Context()
	h := r.Header.Get("Authorization")
	scheme, cred, _ := strings.Cut(h, " ")
	if strings.EqualFold(scheme, "Bearer") {
		p, err := s.auth.Key(ctx, strings.TrimSpace(cred))
		if err != nil || p.Scope == "" {
			return nil, http.StatusForbidden
		}
		return p, 0
	}
	for _, name := range s.cookieNames() {
		c, err := r.Cookie(name)
		if err != nil || c.Value == "" {
			continue
		}
		p, err := s.auth.Session(ctx, c.Value)
		if err != nil {
			continue
		}
		if !sameOrigin(r) {
			return nil, http.StatusUnauthorized
		}
		return p, 0
	}
	// qBittorrent 5.2 accepts Basic credentials when there is no session.
	if user, pass, ok := r.BasicAuth(); ok {
		p, err := s.auth.Basic(ctx, r, user, pass)
		if err != nil {
			if !errors.Is(err, auth.ErrBadCredentials) && !errors.Is(err, auth.ErrLockedOut) &&
				!errors.Is(err, auth.ErrLocalDisabled) {
				s.log.Error("checking Basic credentials", "error", err)
			}
			return nil, http.StatusForbidden
		}
		if !sameOrigin(r) {
			return nil, http.StatusUnauthorized
		}
		return p, 0
	}
	return nil, http.StatusForbidden
}

// sameOrigin is qBittorrent's CSRF check: an Origin or Referer, when sent,
// must name the host the request was sent to.
func sameOrigin(r *http.Request) bool {
	for _, h := range []string{"Origin", "Referer"} {
		v := r.Header.Get(h)
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token, _, err := s.auth.Login(r.Context(), r, r.FormValue("username"), r.FormValue("password"), auth.KindQBittorrent)
	switch {
	case errors.Is(err, auth.ErrBadCredentials):
		http.Error(w, "Fails.", http.StatusUnauthorized)
		return
	case errors.Is(err, auth.ErrLockedOut):
		http.Error(w, "Your IP address has been banned after too many failed authentication attempts.", http.StatusForbidden)
		return
	case errors.Is(err, auth.ErrLocalDisabled):
		http.Error(w, "Password sign-in is turned off; use an API key.", http.StatusForbidden)
		return
	case err != nil:
		s.log.Error("qBittorrent login", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Both names: qBittorrent 5.2 and later use QBT_SID_<port>, older
	// clients may only know SID.
	for _, name := range []string{s.cookieName(), auth.CookieSID} {
		http.SetCookie(w, s.auth.Cookie(name, token, s.auth.SessionMaxAge()))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Method == "session" {
		if err := s.auth.Logout(r.Context(), p); err != nil {
			s.log.Error("qBittorrent logout", "error", err)
		}
		for _, name := range []string{s.cookieName(), auth.CookieSID} {
			http.SetCookie(w, s.auth.Cookie(name, "", -1))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func principal(r *http.Request) *auth.Principal {
	p, _ := r.Context().Value(principalKey{}).(*auth.Principal)
	return p
}

func (s *Server) text(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		_, _ = w.Write([]byte(body))
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("writing qBittorrent response", "error", err)
	}
}

// done answers an action that has nothing to return.
func done(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
}

// parseForm reads a POST body, which qBittorrent clients send urlencoded or,
// when a value is long or a file is attached, multipart. Bodies are capped
// at 64 MiB, room for a few large torrent files.
func parseForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "multipart/form-data" {
		return r.ParseMultipartForm(32 << 20) //nolint:gosec // the body is capped by MaxBytesReader above
	}
	return r.ParseForm()
}

// formBool parses a boolean the way .NET and JavaScript clients send them:
// True, true, 1.
func formBool(r *http.Request, name string) (value, present bool) {
	v := r.FormValue(name)
	if v == "" {
		return false, false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b, true
}

// hashes parses a |-separated list of hashes, or "all".
func hashes(r *http.Request, name string) []string {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return nil
	}
	if strings.EqualFold(v, "all") {
		return []string{"all"}
	}
	var out []string
	for _, h := range strings.Split(v, "|") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// known keeps the hashes of torrents that exist; qBittorrent ignores the
// others. "all" stays.
func (s *Server) known(hs []string) []string {
	if len(hs) == 1 && hs[0] == "all" {
		return hs
	}
	var out []string
	for _, h := range hs {
		if _, ok := s.engine.Get(h); ok {
			out = append(out, h)
		}
	}
	return out
}

// list splits a comma-separated list, trimming spaces.
func list(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// engineError answers an engine error with qBittorrent's usual codes.
func (s *Server) engineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrNotFound):
		http.Error(w, "Not Found", http.StatusNotFound)
	case errors.Is(err, engine.ErrExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, engine.ErrInvalid), errors.Is(err, engine.ErrPathForbidden):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		s.log.Error("qBittorrent API", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
