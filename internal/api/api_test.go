package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/logbuf"
	"github.com/sebdanielsson/piecemeal/internal/rss"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"

type testServer struct {
	*httptest.Server
	t      *testing.T
	client *http.Client
	store  *store.Store
	auth   *auth.Service
	save   string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	saveDir := t.TempDir()
	defaults := settings.Defaults(saveDir)
	defaults.PeerPort = 0
	sm, err := settings.Load(ctx, st, defaults, []string{"PIECEMEAL_UPLOAD_LIMIT=100"})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	e, err := engine.New(ctx, engine.Config{
		DataDir: t.TempDir(), AllowedRoots: []string{saveDir}, TickInterval: 50 * time.Millisecond,
		ClientConfig: func(tc *torrent.ClientConfig) {
			tc.ListenHost = func(string) string { return "127.0.0.1" }
			tc.DisableIPv6 = true
			tc.NoDHT, tc.DisableTrackers, tc.NoDefaultPortForwarding, tc.LocalServiceDiscovery = true, true, true, nil
		},
	}, st, sm, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(ctx, auth.Config{AdminPasswordHash: hash, LocalLogin: true}, st, log)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := fetch.New(nil)
	feeds, err := rss.New(ctx, rss.Deps{Store: st, Settings: sm, Fetcher: fetcher, Engine: e, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(mux, Deps{
		Version: "test", StartedAt: time.Now(), Engine: e, Settings: sm, Auth: a, Fetcher: fetcher,
		Logs: logbuf.New(10), RSS: feeds, Store: st, Log: log,
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &testServer{Server: srv, t: t, client: &http.Client{Jar: jar}, store: st, auth: a, save: saveDir}
}

// do sends a request and decodes a JSON response into out. headers are
// name, value pairs.
func (s *testServer) do(method, path string, body any, wantStatus int, out any, headers ...string) {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.URL+Prefix+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		s.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, wantStatus, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			s.t.Fatalf("%s %s: decoding %s: %v", method, path, b, err)
		}
	}
}

func (s *testServer) login() {
	s.t.Helper()
	s.do(http.MethodPost, "/auth/login", LoginRequest{Username: "admin", Password: "secret"}, http.StatusOK, nil)
}

var csrf = []string{CSRFHeader, "piecemeal"}

func TestAuthRequiredAndCSRF(t *testing.T) {
	s := newTestServer(t)
	s.do(http.MethodGet, "/torrents", nil, http.StatusUnauthorized, nil)
	s.do(http.MethodGet, "/auth/config", nil, http.StatusOK, nil)
	s.do(http.MethodPost, "/auth/login", LoginRequest{Username: "admin", Password: "wrong"}, http.StatusUnauthorized, nil)
	s.login()

	var me auth.Principal
	s.do(http.MethodGet, "/auth/me", nil, http.StatusOK, &me)
	if me.Username != "admin" || !me.IsAdmin {
		t.Fatalf("me: %+v", me)
	}
	// Unsafe requests with a session cookie need the CSRF header, and a
	// matching Origin when there is one.
	s.do(http.MethodPut, "/tags/x", nil, http.StatusForbidden, nil)
	s.do(http.MethodPut, "/tags/x", nil, http.StatusForbidden, nil, CSRFHeader, "piecemeal", "Origin", "https://evil.example")
	s.do(http.MethodPut, "/tags/x", nil, http.StatusNoContent, nil, csrf...)

	s.do(http.MethodPost, "/auth/logout", nil, http.StatusNoContent, nil)
	s.do(http.MethodGet, "/auth/me", nil, http.StatusUnauthorized, nil)
}

func TestAPIKeyScopes(t *testing.T) {
	s := newTestServer(t)
	s.login()
	var ro, qb CreatedAPIKey
	s.do(http.MethodPost, "/api-keys", CreateAPIKeyRequest{Name: "grafana", Scope: auth.ScopeReadOnly}, http.StatusCreated, &ro, csrf...)
	s.do(http.MethodPost, "/api-keys", CreateAPIKeyRequest{Name: "radarr", Scope: auth.ScopeQBittorrent}, http.StatusCreated, &qb, csrf...)

	anon := &testServer{Server: s.Server, t: t, client: http.DefaultClient}
	bearer := func(k string) []string { return []string{"Authorization", "Bearer " + k} }
	anon.do(http.MethodGet, "/torrents", nil, http.StatusOK, nil, bearer(ro.Key)...)
	anon.do(http.MethodPut, "/tags/x", nil, http.StatusForbidden, nil, bearer(ro.Key)...)
	anon.do(http.MethodGet, "/torrents", nil, http.StatusForbidden, nil, bearer(qb.Key)...)
	// Admin endpoints need a full-scope key, even for reads.
	anon.do(http.MethodGet, "/log", nil, http.StatusForbidden, nil, bearer(ro.Key)...)
	// Keys can't mint keys.
	anon.do(http.MethodPost, "/api-keys", CreateAPIKeyRequest{Name: "x", Scope: auth.ScopeFull}, http.StatusForbidden, nil, bearer(ro.Key)...)

	var keys struct{ Items []auth.APIKey }
	s.do(http.MethodGet, "/api-keys", nil, http.StatusOK, &keys)
	if len(keys.Items) != 2 || strings.Contains(keys.Items[0].Prefix, ro.Key) {
		t.Fatalf("keys: %+v", keys)
	}
}

func TestTorrentLifecycle(t *testing.T) {
	s := newTestServer(t)
	s.login()
	s.do(http.MethodPut, "/categories/tv", map[string]string{"savePath": ""}, http.StatusOK, nil, csrf...)
	var cat engine.Category
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "", "seedingTimeLimit": 90, "seedGoalAction": "remove"},
		http.StatusOK, &cat, csrf...)
	// Goals left out keep their values.
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "", "ratioLimit": 1.5}, http.StatusOK, &cat, csrf...)
	if cat.RatioLimit != 1.5 || cat.SeedingTimeLimit != 90 || cat.InactiveSeedingTimeLimit != -2 ||
		cat.SeedGoalAction != engine.ActionRemove || cat.SeedGoalMode != engine.ModeDefault {
		t.Fatalf("category: %+v", cat)
	}
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "", "seedGoalAction": "pause"}, http.StatusUnprocessableEntity, nil, csrf...)
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "", "ratioLimit": -3}, http.StatusUnprocessableEntity, nil, csrf...)

	stopped := true
	var added engine.Torrent
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{
		Magnet: "magnet:?xt=urn:btih:" + testHash + "&dn=Some+Show", Category: "tv", Tags: []string{"hd"}, Stopped: &stopped,
	}, http.StatusCreated, &added, csrf...)
	if added.Hash != testHash || added.Name != "Some Show" || added.State != engine.StateStopped || added.Category != "tv" {
		t.Fatalf("added: %+v", added)
	}
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{Magnet: "magnet:?xt=urn:btih:" + testHash}, http.StatusConflict, nil, csrf...)
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{}, http.StatusUnprocessableEntity, nil, csrf...)
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("1", 40), SavePath: "/etc"}, http.StatusUnprocessableEntity, nil, csrf...)

	var list TorrentList
	s.do(http.MethodGet, "/torrents?filter=stopped&category=tv&search=show", nil, http.StatusOK, &list)
	if list.Total != 1 {
		t.Fatalf("list: %+v", list)
	}
	s.do(http.MethodGet, "/torrents?filter=seeding", nil, http.StatusOK, &list)
	if list.Total != 0 {
		t.Fatalf("seeding list: %+v", list)
	}

	name := "Renamed"
	ratio := 2.0
	var updated engine.Torrent
	s.do(http.MethodPatch, "/torrents/"+testHash, UpdateTorrentRequest{Name: &name, RatioLimit: &ratio, Tags: &[]string{"4k"}}, http.StatusOK, &updated, csrf...)
	if updated.Name != "Renamed" || updated.RatioLimit != 2 || len(updated.Tags) != 1 || updated.Tags[0] != "4k" {
		t.Fatalf("updated: %+v", updated)
	}
	if updated.SeedGoalAction != engine.ActionDefault || updated.SeedGoals != (engine.SeedGoals{
		RatioLimit: 2, SeedingTimeLimit: 90, InactiveSeedingTimeLimit: -1, Action: engine.ActionRemove, Mode: engine.ModeAny,
	}) {
		t.Fatalf("seed goals: %s %+v", updated.SeedGoalAction, updated.SeedGoals)
	}
	action, mode := engine.ActionRemoveWithData, engine.ModeAll
	s.do(http.MethodPatch, "/torrents/"+testHash, UpdateTorrentRequest{SeedGoalAction: &action, SeedGoalMode: &mode}, http.StatusOK, &updated, csrf...)
	if updated.SeedGoalAction != action || updated.SeedGoalMode != mode || updated.RatioLimit != 2 ||
		updated.SeedGoals.Action != action || updated.SeedGoals.Mode != mode {
		t.Fatalf("after setting the action and mode: %+v", updated)
	}
	bad := "superSeed"
	s.do(http.MethodPatch, "/torrents/"+testHash, UpdateTorrentRequest{SeedGoalAction: &bad}, http.StatusUnprocessableEntity, nil, csrf...)

	s.do(http.MethodPost, "/torrents/"+testHash+"/start", nil, http.StatusOK, &updated, csrf...)
	if updated.State != engine.StateMetadata {
		t.Fatalf("started: %+v", updated)
	}
	s.do(http.MethodPost, "/torrents/actions", BulkAction{Hashes: []string{testHash}, Action: "stop"}, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodGet, "/torrents/"+testHash, nil, http.StatusOK, &updated)
	if !updated.Stopped {
		t.Fatalf("after bulk stop: %+v", updated)
	}

	s.do(http.MethodDelete, "/categories/tv", nil, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodGet, "/torrents/"+testHash, nil, http.StatusOK, &updated)
	if updated.Category != "" {
		t.Fatalf("category not cleared: %+v", updated)
	}
	// A running magnet without metadata has no peers and no pieces yet.
	s.do(http.MethodPost, "/torrents/"+testHash+"/start", nil, http.StatusOK, nil, csrf...)
	var peers struct{ Items []engine.Peer }
	s.do(http.MethodGet, "/torrents/"+testHash+"/peers", nil, http.StatusOK, &peers)
	s.do(http.MethodGet, "/torrents/"+testHash+"/pieces", nil, http.StatusOK, nil)
	s.do(http.MethodPost, "/torrents/"+testHash+"/reannounce", nil, http.StatusOK, nil, csrf...)

	s.do(http.MethodDelete, "/torrents/"+testHash, nil, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodGet, "/torrents/"+testHash, nil, http.StatusNotFound, nil)
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{Magnet: "magnet:?xt=urn:btih:" + testHash, Tags: []string{" padded"}}, http.StatusUnprocessableEntity, nil, csrf...)
}

func TestSettings(t *testing.T) {
	s := newTestServer(t)
	s.login()
	var got SettingsResponse
	s.do(http.MethodGet, "/settings", nil, http.StatusOK, &got)
	if got.Settings.UploadLimit != 100 || got.Locked["uploadLimit"] != "PIECEMEAL_UPLOAD_LIMIT" {
		t.Fatalf("settings: %+v", got)
	}
	s.do(http.MethodPatch, "/settings", map[string]any{"downloadLimit": 5000}, http.StatusOK, &got, csrf...)
	if got.Settings.DownloadLimit != 5000 {
		t.Fatalf("after patch: %+v", got)
	}
	s.do(http.MethodPatch, "/settings", map[string]any{"seedGoalMode": "all"}, http.StatusOK, &got, csrf...)
	if got.Settings.SeedGoalMode != "all" {
		t.Fatalf("after patching seedGoalMode: %+v", got)
	}
	s.do(http.MethodPatch, "/settings", map[string]any{"seedGoalMode": "most"}, http.StatusUnprocessableEntity, nil, csrf...)
	s.do(http.MethodPatch, "/settings", map[string]any{"uploadLimit": 1}, http.StatusUnprocessableEntity, nil, csrf...)
	s.do(http.MethodPatch, "/settings", map[string]any{"defaultSavePath": "/etc"}, http.StatusUnprocessableEntity, nil, csrf...)
	var tr engine.TransferStatus
	s.do(http.MethodGet, "/transfer", nil, http.StatusOK, &tr)
	if tr.DownloadLimit != 5000 || tr.UploadLimit != 100 {
		t.Fatalf("transfer: %+v", tr)
	}
}

func TestEventsStartWithSnapshot(t *testing.T) {
	s := newTestServer(t)
	s.login()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+Prefix+"/events", nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1<<20)
	var event string
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			event = v
		}
		if v, ok := strings.CutPrefix(line, "data: "); ok {
			if event != "snapshot" {
				t.Fatalf("first event %q", event)
			}
			var snap SnapshotEvent
			if err := json.Unmarshal([]byte(v), &snap); err != nil {
				t.Fatal(err)
			}
			if snap.Torrents == nil || snap.Tags == nil {
				t.Fatalf("snapshot: %+v", snap)
			}
			break
		}
	}

	// A change arrives as a torrents event.
	stopped := true
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{Magnet: "magnet:?xt=urn:btih:" + testHash, Stopped: &stopped}, http.StatusCreated, nil, csrf...)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "data: "); ok && event == "torrents" {
			var ev TorrentsEvent
			if err := json.Unmarshal([]byte(v), &ev); err != nil {
				t.Fatal(err)
			}
			if len(ev.Updated) == 1 && ev.Updated[0].Hash == testHash {
				return
			}
		} else if v, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			event = v
		}
	}
	t.Fatal("no torrents event for the added torrent")
}

func TestTransferLimitsForUsers(t *testing.T) {
	s := newTestServer(t)
	s.login()
	var tr engine.TransferStatus
	alt := true
	limit := int64(2000)
	s.do(http.MethodPatch, "/transfer", TransferLimits{AltSpeedEnabled: &alt, AltDownloadLimit: &limit}, http.StatusOK, &tr, csrf...)
	if !tr.AltSpeedEnabled || tr.DownloadLimit != 2000 {
		t.Fatalf("transfer: %+v", tr)
	}
	// uploadLimit is pinned by PIECEMEAL_UPLOAD_LIMIT in this server.
	up := int64(5)
	s.do(http.MethodPatch, "/transfer", TransferLimits{UploadLimit: &up}, http.StatusUnprocessableEntity, nil, csrf...)

	var keys struct{ Items []auth.APIKey }
	var created CreatedAPIKey
	s.do(http.MethodPost, "/api-keys", CreateAPIKeyRequest{Name: "k", Scope: auth.ScopeFull}, http.StatusCreated, &created, csrf...)
	if created.Username != "admin" {
		t.Fatalf("created key's username %q", created.Username)
	}
	s.do(http.MethodGet, "/api-keys", nil, http.StatusOK, &keys)
	var sessions struct{ Items []auth.Session }
	s.do(http.MethodGet, "/sessions", nil, http.StatusOK, &sessions)
	if keys.Items[0].Username != "admin" || sessions.Items[0].Username != "admin" {
		t.Fatalf("usernames: %+v %+v", keys.Items, sessions.Items)
	}
}

func TestNonAdminCanChangeLimitsButNotSettings(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	u, err := s.store.CreateUser(ctx, store.CreateUserParams{Username: "bob", DisplayName: "Bob", CreatedAt: time.Now().Unix()})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.auth.OpenSession(ctx, httptest.NewRequest(http.MethodPost, "/", nil), u.ID, auth.KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	cookie := []string{"Cookie", auth.CookieWeb + "=" + token, CSRFHeader, "piecemeal"}
	anon := &testServer{Server: s.Server, t: t, client: http.DefaultClient}
	limit := int64(1000)
	anon.do(http.MethodPatch, "/transfer", TransferLimits{DownloadLimit: &limit}, http.StatusOK, nil, cookie...)
	anon.do(http.MethodPatch, "/settings", map[string]any{"downloadLimit": 5}, http.StatusForbidden, nil, cookie...)
	anon.do(http.MethodGet, "/audit", nil, http.StatusForbidden, nil, cookie...)
	anon.do(http.MethodGet, "/admin/backup", nil, http.StatusForbidden, nil, cookie...)
}

func TestBackupDownload(t *testing.T) {
	s := newTestServer(t)
	s.login()
	req, _ := http.NewRequest(http.MethodGet, s.URL+Prefix+"/admin/backup", nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(b, []byte("SQLite format 3\x00")) ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), `attachment; filename="piecemeal-`) {
		t.Fatalf("backup: %d %q %.20q", resp.StatusCode, resp.Header.Get("Content-Disposition"), b)
	}
}

func TestCheckingFilterIncludesQueued(t *testing.T) {
	for state, want := range map[engine.State]bool{
		engine.StateChecking: true, engine.StateQueuedForChecking: true, engine.StateQueued: false,
	} {
		if got := MatchesFilter(engine.Torrent{State: state}, FilterChecking); got != want {
			t.Errorf("checking filter on %s: %v, want %v", state, got, want)
		}
	}
}

// TestPaths covers the incomplete folder and automatic management through
// the native API: categories, adding, changing a torrent and bulk actions.
func TestPaths(t *testing.T) {
	s := newTestServer(t)
	s.login()
	var cat engine.Category
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "shows", "incompletePathMode": "on", "incompletePath": "tv"},
		http.StatusOK, &cat, csrf...)
	if cat.SavePath != "shows" || cat.IncompletePathMode != engine.IncompleteOn || cat.IncompletePath != "tv" {
		t.Fatalf("category: %+v", cat)
	}
	// Left out, the incomplete folder stays; off drops its path.
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "shows"}, http.StatusOK, &cat, csrf...)
	if cat.IncompletePathMode != engine.IncompleteOn || cat.IncompletePath != "tv" {
		t.Fatalf("category after an edit without it: %+v", cat)
	}
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "", "incompletePathMode": "bad"}, http.StatusUnprocessableEntity, nil, csrf...)
	s.do(http.MethodPut, "/categories/tv", map[string]any{"savePath": "", "incompletePathMode": "on", "incompletePath": "/etc"},
		http.StatusUnprocessableEntity, nil, csrf...)

	auto, stopped := true, true
	var added engine.Torrent
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{
		Magnet: "magnet:?xt=urn:btih:" + testHash, Category: "tv", AutoManaged: &auto, Stopped: &stopped,
	}, http.StatusCreated, &added, csrf...)
	temp := filepath.Join(s.save, "temp")
	if !added.AutoManaged || added.SavePath != filepath.Join(s.save, "shows") || added.IncompletePath != filepath.Join(temp, "tv") {
		t.Fatalf("added: %+v", added)
	}
	none := ""
	other := strings.Repeat("1", 40)
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{
		Magnet: "magnet:?xt=urn:btih:" + other, Category: "tv", IncompletePath: &none, Stopped: &stopped,
	}, http.StatusCreated, &added, csrf...)
	if added.AutoManaged || added.SavePath != filepath.Join(s.save, "shows") || added.IncompletePath != "" {
		t.Fatalf("added manually managed: %+v", added)
	}

	// A new incomplete folder, then a save path: automatic management off.
	var updated engine.Torrent
	inc := filepath.Join(s.save, "inc")
	s.do(http.MethodPatch, "/torrents/"+testHash, UpdateTorrentRequest{IncompletePath: &inc}, http.StatusOK, &updated, csrf...)
	if updated.AutoManaged || updated.IncompletePath != inc {
		t.Fatalf("after a new incomplete folder: %+v", updated)
	}
	s.do(http.MethodPatch, "/torrents/"+testHash, UpdateTorrentRequest{AutoManaged: &auto}, http.StatusOK, &updated, csrf...)
	if !updated.AutoManaged || updated.IncompletePath != filepath.Join(temp, "tv") {
		t.Fatalf("automatically managed again: %+v", updated)
	}
	s.do(http.MethodPost, "/torrents/actions", BulkAction{Hashes: []string{testHash, other}, Action: "setIncompletePath", IncompletePath: inc},
		http.StatusNoContent, nil, csrf...)
	s.do(http.MethodPost, "/torrents/actions", BulkAction{Hashes: []string{"all"}, Action: "setAutoManaged", AutoManaged: true},
		http.StatusNoContent, nil, csrf...)
	for _, h := range []string{testHash, other} {
		s.do(http.MethodGet, "/torrents/"+h, nil, http.StatusOK, &updated)
		if !updated.AutoManaged || updated.IncompletePath != filepath.Join(temp, "tv") {
			t.Fatalf("%s after the bulk actions: %+v", h, updated)
		}
	}

	// The settings.
	var res SettingsResponse
	s.do(http.MethodPatch, "/settings", map[string]any{"incompletePathEnabled": true, "autoManagement": true}, http.StatusOK, &res, csrf...)
	if !res.Settings.IncompletePathEnabled || res.Settings.IncompletePath != temp || !res.Settings.AutoManagement {
		t.Fatalf("settings: %+v", res.Settings)
	}
	s.do(http.MethodPatch, "/settings", map[string]any{"incompletePath": "/etc"}, http.StatusUnprocessableEntity, nil, csrf...)
}
