package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/logbuf"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

type env struct {
	t      *testing.T
	srv    *httptest.Server
	engine *engine.Engine
	auth   *auth.Service
	client *http.Client
	save   string
	store  *store.Store
}

func newEnv(t *testing.T, environ ...string) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	save := t.TempDir()
	defaults := settings.Defaults(save)
	defaults.PeerPort = 0
	defaults.DHT = false
	sm, err := settings.Load(ctx, st, defaults, environ)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	e, err := engine.New(ctx, engine.Config{
		DataDir: t.TempDir(), AllowedRoots: []string{save}, TickInterval: 50 * time.Millisecond,
		ClientConfig: enginetest.Offline,
	}, st, sm, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	hash, err := auth.HashPassword("adminadmin")
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(ctx, auth.Config{AdminPasswordHash: hash, LocalLogin: true}, st, log)
	if err != nil {
		t.Fatal(err)
	}
	logs := logbuf.New(100)
	srv := httptest.NewServer(New(Deps{Engine: e, Settings: sm, Auth: a, Fetcher: fetch.New(nil), Store: st, Logs: logs, Log: log, HTTPPort: 8080}))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &env{t: t, srv: srv, engine: e, auth: a, client: &http.Client{Jar: jar}, save: save, store: st}
}

type response struct {
	status int
	body   []byte
	header http.Header
}

// newEnvClient is another client of the same server, with its own cookies.
func newEnvClient(e *env) *env {
	jar, _ := cookiejar.New(nil)
	return &env{t: e.t, srv: e.srv, engine: e.engine, auth: e.auth, client: &http.Client{Jar: jar}, save: e.save, store: e.store}
}

func (r response) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decoding %s: %v", r.body, err)
	}
}

func (e *env) do(req *http.Request) response {
	e.t.Helper()
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return response{status: resp.StatusCode, body: b, header: resp.Header}
}

func (e *env) get(path string, header ...string) response {
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/v2/"+path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	return e.do(req)
}

func (e *env) post(path string, form url.Values, header ...string) response {
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v2/"+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	return e.do(req)
}

// postMultipart sends fields and torrent files the way the *arrs do: one
// part per field, and files named torrents with application/octet-stream.
func (e *env) postMultipart(path string, fields map[string]string, files ...[]byte) response {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for i, f := range files {
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="torrents"; filename="release%d.torrent"`, i)}
		h["Content-Type"] = []string{"application/octet-stream"}
		w, _ := mw.CreatePart(h)
		_, _ = w.Write(f)
	}
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v2/"+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return e.do(req)
}

func (e *env) login() {
	e.t.Helper()
	if r := e.post("auth/login", url.Values{"username": {"admin"}, "password": {"adminadmin"}}); r.status != http.StatusNoContent {
		e.t.Fatalf("login: %d %s", r.status, r.body)
	}
}

func expect(t *testing.T, what string, r response, status int) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: status %d, want %d: %s", what, r.status, status, r.body)
	}
}

func TestAuthentication(t *testing.T) {
	e := newEnv(t)

	// Unauthenticated requests get 403, which the *arrs read as "v2 API
	// present, log in".
	expect(t, "probe without auth", e.get("app/webapiVersion"), http.StatusForbidden)
	expect(t, "unknown endpoint", e.get("app/nope"), http.StatusNotFound)
	expect(t, "wrong method", e.get("auth/login"), http.StatusMethodNotAllowed)

	r := e.post("auth/login", url.Values{"username": {"admin"}, "password": {"wrong"}})
	expect(t, "wrong password", r, http.StatusUnauthorized)

	// The *arrs send Basic on every request, the login included.
	r = e.post("auth/login", url.Values{"username": {"admin"}, "password": {"adminadmin"}},
		"Authorization", "Basic "+basic("admin", "adminadmin"))
	expect(t, "login", r, http.StatusNoContent)
	var names []string
	for _, c := range (&http.Response{Header: r.header}).Cookies() {
		names = append(names, c.Name)
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie %s attributes: %+v", c.Name, c)
		}
	}
	if strings.Join(names, ",") != "QBT_SID_8080,SID" {
		t.Fatalf("cookies %v", names)
	}

	r = e.get("app/webapiVersion")
	expect(t, "version with session", r, http.StatusOK)
	if string(r.body) != WebAPIVersion {
		t.Fatalf("webapiVersion %q", r.body)
	}
	if r := e.get("app/version"); string(r.body) != AppVersion {
		t.Fatalf("version %q", r.body)
	}
	// CSRF: an Origin for another host is refused.
	expect(t, "cross-origin", e.get("app/preferences", "Origin", "https://evil.example"), http.StatusUnauthorized)

	// Basic auth without a session, as qBittorrent 5.2 accepts.
	anon := &env{t: t, srv: e.srv, client: http.DefaultClient}
	expect(t, "basic", anon.get("app/preferences", "Authorization", "Basic "+basic("admin", "adminadmin")), http.StatusOK)
	expect(t, "wrong basic", anon.get("app/preferences", "Authorization", "Basic "+basic("admin", "nope")), http.StatusForbidden)
	expect(t, "unknown key", anon.get("app/preferences", "Authorization", "Bearer pm_unknown"), http.StatusForbidden)

	expect(t, "logout", e.post("auth/logout", nil), http.StatusNoContent)
	expect(t, "after logout", e.get("app/preferences"), http.StatusForbidden)
}

func basic(user, pass string) string {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth(user, pass)
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Basic ")
}

func TestAPIKeyScopes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, p, err := e.auth.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "adminadmin", auth.KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	_, qbKey, err := e.auth.CreateAPIKey(ctx, p, "radarr", auth.ScopeQBittorrent)
	if err != nil {
		t.Fatal(err)
	}
	_, roKey, err := e.auth.CreateAPIKey(ctx, p, "grafana", auth.ScopeReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	r := e.get("torrents/info", "Authorization", "Bearer "+qbKey)
	expect(t, "qbittorrent key", r, http.StatusOK)
	if len(r.header.Values("Set-Cookie")) != 0 {
		t.Fatal("a key-authenticated request got a cookie")
	}
	expect(t, "read-only key reads", e.get("torrents/info", "Authorization", "Bearer "+roKey), http.StatusOK)
	expect(t, "read-only key reads by POST", e.post("torrents/info", nil, "Authorization", "Bearer "+roKey), http.StatusOK)
	expect(t, "read-only key writes", e.post("torrents/createTags", url.Values{"tags": {"x"}}, "Authorization", "Bearer "+roKey), http.StatusForbidden)
}

func TestPreferences(t *testing.T) {
	e := newEnv(t, "PIECEMEAL_RATIO_LIMIT=2")
	e.login()
	var p map[string]any
	e.get("app/preferences").json(t, &p)
	if p["save_path"] != e.save || p["max_ratio_enabled"] != true || p["max_ratio"] != 2.0 ||
		p["max_ratio_act"] != 0.0 || p["queueing_enabled"] != false || p["max_seeding_time_enabled"] != false {
		t.Fatalf("preferences: %v", p)
	}
	if _, ok := p["dht"]; !ok {
		t.Fatal("no dht preference; the *arrs read a missing one as off")
	}
}

// TestArrGrabFlow follows what Radarr and Sonarr do: test the connection
// and category, add a torrent file, wait for it, set it up, watch it
// complete and stop at the seed goal, then remove it with its files.
func TestArrGrabFlow(t *testing.T) {
	e := newEnv(t)
	e.login()
	seed := enginetest.NewSeeder(t, "Movie.2024.1080p", map[string]int{"movie.mkv": 300 << 10, "movie.nfo": 1 << 10})

	var cats map[string]map[string]any
	e.get("torrents/categories").json(t, &cats)
	if len(cats) != 0 {
		t.Fatalf("categories: %v", cats)
	}
	expect(t, "createCategory", e.post("torrents/createCategory", url.Values{"category": {"radarr"}}), http.StatusOK)
	expect(t, "createCategory again", e.post("torrents/createCategory", url.Values{"category": {"radarr"}}), http.StatusConflict)
	expect(t, "bad category name", e.post("torrents/createCategory", url.Values{"category": {"/bad"}}), http.StatusConflict)
	e.get("torrents/categories").json(t, &cats)
	if cats["radarr"]["savePath"] != "" || cats["radarr"]["name"] != "radarr" {
		t.Fatalf("categories: %v", cats)
	}
	var list []Torrent
	e.get("torrents/info?category=radarr").json(t, &list)
	if len(list) != 0 {
		t.Fatalf("info: %v", list)
	}

	// Add, with a ratio goal of 0 so it stops as soon as it completes.
	r := e.postMultipart("torrents/add", map[string]string{
		"category": "radarr", "stopped": "False", "ratioLimit": "0", "firstLastPiecePrio": "True",
	}, seed.Metainfo)
	expect(t, "add", r, http.StatusOK)
	var added addResult
	r.json(t, &added)
	if added.Success != 1 || len(added.Added) != 1 || added.Added[0] != seed.Hash {
		t.Fatalf("add result: %+v", added)
	}
	// Synchronous: the torrent is there right away.
	expect(t, "properties right after add", e.get("torrents/properties?hash="+strings.ToUpper(seed.Hash)), http.StatusOK)
	expect(t, "add again", e.postMultipart("torrents/add", map[string]string{"category": "radarr"}, seed.Metainfo), http.StatusConflict)
	expect(t, "add garbage", e.postMultipart("torrents/add", nil, []byte("not a torrent")), http.StatusUnsupportedMediaType)
	expect(t, "add nothing", e.post("torrents/add", url.Values{"category": {"radarr"}}), http.StatusBadRequest)

	expect(t, "topPrio", e.post("torrents/topPrio", url.Values{"hashes": {seed.Hash}}), http.StatusConflict)
	expect(t, "setForceStart", e.post("torrents/setForceStart", url.Values{"hashes": {seed.Hash}, "value": {"true"}}), http.StatusOK)
	expect(t, "addTags", e.post("torrents/addTags", url.Values{"hashes": {seed.Hash}, "tags": {"hd, 4k"}}), http.StatusOK)

	if err := e.engine.AddPeers(context.Background(), []string{seed.Addr}, seed.Hash); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var tor Torrent
	for {
		e.get("torrents/info?category=radarr").json(t, &list)
		if len(list) == 1 && list[0].State == "stoppedUP" {
			tor = list[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not stopped at the seed goal: %+v", list)
		}
		time.Sleep(50 * time.Millisecond)
	}
	root := e.save + "/Movie.2024.1080p"
	if tor.Progress != 1 || tor.ContentPath != root || tor.RootPath != root || tor.SavePath != e.save ||
		tor.RatioLimit != 0 || tor.Tags != "4k, hd" || !tor.FLPiecePrio || tor.Size != 301<<10 || tor.ETA != 0 {
		t.Fatalf("torrent: %+v", tor)
	}

	var files []map[string]any
	e.get("torrents/files?hash="+seed.Hash).json(t, &files)
	if len(files) != 2 || !strings.HasPrefix(files[0]["name"].(string), "Movie.2024.1080p/") {
		t.Fatalf("files: %v", files)
	}
	var props map[string]any
	e.get("torrents/properties?hash="+seed.Hash).json(t, &props)
	if props["save_path"] != e.save || props["pieces_num"].(float64) < 1 {
		t.Fatalf("properties: %v", props)
	}

	expect(t, "setCategory missing", e.post("torrents/setCategory", url.Values{"hashes": {seed.Hash}, "category": {"nope"}}), http.StatusConflict)
	expect(t, "delete", e.post("torrents/delete", url.Values{"hashes": {seed.Hash}, "deleteFiles": {"true"}}), http.StatusOK)
	expect(t, "properties after delete", e.get("torrents/properties?hash="+seed.Hash), http.StatusNotFound)
	expect(t, "delete unknown", e.post("torrents/delete", url.Values{"hashes": {seed.Hash}}), http.StatusOK)
}

func TestLongMagnetArrivesMultipart(t *testing.T) {
	e := newEnv(t)
	e.login()
	// The *arrs switch to multipart when a value is over 1024 bytes.
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x&tr=" + strings.Repeat("a", 1100)
	r := e.postMultipart("torrents/add", map[string]string{"urls": magnet, "stopped": "True"})
	expect(t, "add long magnet", r, http.StatusOK)
	var list []Torrent
	e.get("torrents/info").json(t, &list)
	if len(list) != 1 || list[0].State != "stoppedDL" || list[0].Name != "x" {
		t.Fatalf("info: %+v", list)
	}
}

func TestStates(t *testing.T) {
	for _, tc := range []struct {
		t    engine.Torrent
		want string
	}{
		{engine.Torrent{State: engine.StateMetadata}, "metaDL"},
		{engine.Torrent{State: engine.StateMetadata, Forced: true}, "forcedMetaDL"},
		{engine.Torrent{State: engine.StateStopped}, "stoppedDL"},
		{engine.Torrent{State: engine.StateStopped, Complete: true}, "stoppedUP"},
		{engine.Torrent{State: engine.StateDownloading}, "downloading"},
		{engine.Torrent{State: engine.StateStalledDownloading}, "stalledDL"},
		{engine.Torrent{State: engine.StateStalledDownloading, Forced: true}, "forcedDL"},
		{engine.Torrent{State: engine.StateSeeding, Complete: true}, "uploading"},
		{engine.Torrent{State: engine.StateStalledSeeding, Complete: true}, "stalledUP"},
		{engine.Torrent{State: engine.StateChecking, Complete: true}, "checkingUP"},
		{engine.Torrent{State: engine.StateError}, "error"},
	} {
		if got := State(tc.t); got != tc.want {
			t.Errorf("State(%s, complete=%v, forced=%v) = %s, want %s", tc.t.State, tc.t.Complete, tc.t.Forced, got, tc.want)
		}
	}
}
