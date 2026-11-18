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
	"time"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/logbuf"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
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
	store    *store.Store
	logs     *logbuf.Buffer
	log      *slog.Logger
	// port is the HTTP listen port, which qBittorrent puts in its session
	// cookie's name.
	port   int
	routes map[string]route
	// altWebUI is set when piecemeal hosts an alternative qBittorrent web
	// UI such as VueTorrent.
	altWebUI bool
	started  time.Time

	mainSync syncStore[mainSnapshot]
	peerSync syncStore[peerSnapshot]
}

// Deps are the services the API uses.
type Deps struct {
	Engine   *engine.Engine
	Settings *settings.Manager
	Auth     *auth.Service
	Fetcher  *fetch.Fetcher
	Store    *store.Store
	Logs     *logbuf.Buffer
	Log      *slog.Logger
	// HTTPPort is the port piecemeal serves HTTP on.
	HTTPPort int
	// AltWebUI is set when an alternative web UI is hosted.
	AltWebUI bool
}

type route struct {
	// method is POST for endpoints that change something and GET for
	// reads, which accept POST too: some clients send every request as a
	// POST.
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
		engine: d.Engine, settings: d.Settings, auth: d.Auth, fetcher: d.Fetcher, store: d.Store, logs: d.Logs, log: d.Log,
		port: d.HTTPPort, altWebUI: d.AltWebUI, started: time.Now(),
	}
	if err := s.loadCookies(context.Background()); err != nil {
		s.log.Warn("loading the download cookies", "error", err)
	}
	get, post := http.MethodGet, http.MethodPost
	s.routes = map[string]route{
		"auth/login":  {post, true, s.login},
		"auth/logout": {post, false, s.logout},

		"app/version":                     {get, false, s.text(AppVersion)},
		"app/webapiVersion":               {get, false, s.text(WebAPIVersion)},
		"app/buildInfo":                   {get, false, s.buildInfo},
		"app/preferences":                 {get, false, s.preferences},
		"app/setPreferences":              {post, false, s.setPreferences},
		"app/processInfo":                 {get, false, s.processInfo},
		"app/getFreeSpaceAtPath":          {get, false, s.getFreeSpaceAtPath},
		"app/cookies":                     {get, false, s.cookies},
		"app/setCookies":                  {post, false, s.setCookies},
		"app/rotateAPIKey":                {post, false, s.rotateAPIKey},
		"app/deleteAPIKey":                {post, false, s.deleteAPIKey},
		"app/defaultSavePath":             {get, false, s.defaultSavePath},
		"app/shutdown":                    {post, false, s.notSupported},
		"app/sendTestEmail":               {post, false, s.notSupported},
		"app/networkInterfaceList":        {get, false, s.networkInterfaceList},
		"app/networkInterfaceAddressList": {get, false, s.networkInterfaceAddressList},
		"app/getDirectoryContent":         {post, false, s.getDirectoryContent},

		"clientdata/load":  {get, false, s.clientDataLoad},
		"clientdata/store": {post, false, s.clientDataStore},

		"sync/maindata":     {get, false, s.maindata},
		"sync/torrentPeers": {get, false, s.torrentPeers},

		"log/main":  {get, false, s.logMain},
		"log/peers": {get, false, s.logPeers},

		"transfer/info":                  {get, false, s.transferInfo},
		"transfer/speedLimitsMode":       {get, false, s.speedLimitsMode},
		"transfer/toggleSpeedLimitsMode": {post, false, s.toggleSpeedLimitsMode},
		"transfer/setSpeedLimitsMode":    {post, false, s.setSpeedLimitsMode},
		"transfer/downloadLimit":         {get, false, s.globalLimit(true)},
		"transfer/uploadLimit":           {get, false, s.globalLimit(false)},
		"transfer/setDownloadLimit":      {post, false, s.setGlobalLimit(true)},
		"transfer/setUploadLimit":        {post, false, s.setGlobalLimit(false)},
		"transfer/getSpeedLimits":        {get, false, s.getSpeedLimits},
		"transfer/setSpeedLimits":        {post, false, s.setSpeedLimits},
		"transfer/banPeers":              {post, false, s.banPeers},
		"transfer/pauseSession":          {post, false, s.notSupported},
		"transfer/resumeSession":         {post, false, s.notSupported},

		"torrentcreator/addTask":     {post, false, s.creatorAddTask},
		"torrentcreator/status":      {get, false, s.creatorStatus},
		"torrentcreator/torrentFile": {get, false, s.creatorTorrentFile},
		"torrentcreator/deleteTask":  {post, false, s.creatorDeleteTask},

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
		"torrents/topPrio":                  {post, false, s.queue("top")},
		"torrents/bottomPrio":               {post, false, s.queue("bottom")},
		"torrents/increasePrio":             {post, false, s.queue("up")},
		"torrents/decreasePrio":             {post, false, s.queue("down")},
		"torrents/webseeds":                 {get, false, s.webseeds},
		"torrents/addWebSeeds":              {post, false, s.editWebSeeds("add")},
		"torrents/editWebSeed":              {post, false, s.editWebSeeds("edit")},
		"torrents/removeWebSeeds":           {post, false, s.editWebSeeds("remove")},
		"torrents/addTrackers":              {post, false, s.addTrackers},
		"torrents/editTracker":              {post, false, s.editTracker},
		"torrents/removeTrackers":           {post, false, s.removeTrackers},
		"torrents/addPeers":                 {post, false, s.addPeers},
		"torrents/setLocation":              {post, false, s.setLocation("location")},
		"torrents/setSavePath":              {post, false, s.setLocation("path")},
		"torrents/renameFile":               {post, false, s.renamePath},
		"torrents/renameFolder":             {post, false, s.renamePath},
		"torrents/downloadLimit":            {get, false, s.torrentLimits(true)},
		"torrents/uploadLimit":              {get, false, s.torrentLimits(false)},
		"torrents/count":                    {get, false, s.count},
		"torrents/export":                   {get, false, s.export},
		"torrents/pieceHashes":              {get, false, s.pieceHashes},
		"torrents/downloadFile":             {get, false, s.downloadFile},
		"torrents/SSLParameters":            {get, false, s.sslParameters},
		"torrents/setSSLParameters":         {post, false, s.notSupported},
		"torrents/setTags":                  {post, false, s.setTags},
		"torrents/setAutoManagement":        {post, false, s.acceptIgnored},
		"torrents/setDownloadPath":          {post, false, s.acceptIgnored},
		"torrents/setSuperSeeding":          {post, false, s.notSupported},
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
	if r.Method != rt.method && (rt.method != http.MethodGet || r.Method != http.MethodPost) {
		allow := rt.method
		if allow == http.MethodGet {
			allow = "GET, POST"
		}
		w.Header().Set("Allow", allow)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !rt.public {
		p, status := s.authenticate(r)
		if p == nil {
			http.Error(w, http.StatusText(status), status)
			return
		}
		if rt.method != http.MethodGet && !p.CanWrite() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
	}
	// Bodies are read only once the caller is known.
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	rt.h(w, r)
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

// parseForm reads the query string and a POST body, which qBittorrent
// clients send urlencoded or, when a value is long or a file is attached,
// multipart. Bodies are capped at 64 MiB, room for a few large torrent
// files. ServeHTTP parses every request, so handlers just read r.Form;
// parsing again is a no-op.
func parseForm(w http.ResponseWriter, r *http.Request) error {
	if r.Form != nil {
		return nil
	}
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
