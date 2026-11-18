package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

func (e *env) setPreferences(prefs map[string]any) response {
	e.t.Helper()
	b, err := json.Marshal(prefs)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.post("app/setPreferences", url.Values{"json": {string(b)}})
}

func (e *env) preferences() map[string]any {
	e.t.Helper()
	var p map[string]any
	e.get("app/preferences").json(e.t, &p)
	return p
}

func TestSetPreferences(t *testing.T) {
	e := newEnv(t, "PIECEMEAL_RATIO_LIMIT=2")
	e.login()
	before := e.preferences()
	if len(before) < 240 {
		t.Fatalf("only %d preferences", len(before))
	}

	// Writing back what was read changes nothing.
	expect(t, "read-back", e.setPreferences(before), http.StatusOK)
	if after := e.preferences(); !maps.EqualFunc(before, after, func(a, b any) bool { return jsonEqual(t, a, b) }) {
		t.Fatalf("read-back changed preferences:\n%v\n%v", before, after)
	}

	expect(t, "set", e.setPreferences(map[string]any{
		"dl_limit": 5000, "alt_up_limit": 1, "queueing_enabled": true, "max_active_downloads": 2,
		"encryption": 1, "schedule_from_hour": 6, "scheduler_days": 1, "slow_torrent_dl_rate_threshold": 4,
		"start_paused_enabled": true, "unknown_key": "ignored", "max_seeding_time": 60,
		// Pinned by the environment, so ignored.
		"max_ratio": 3,
	}), http.StatusOK)
	p := e.preferences()
	for k, want := range map[string]any{
		"dl_limit": 4096.0, "alt_up_limit": 1024.0, "queueing_enabled": true, "max_active_downloads": 2.0,
		"encryption": 1.0, "schedule_from_hour": 6.0, "schedule_from_min": 0.0, "scheduler_days": 1.0,
		"slow_torrent_dl_rate_threshold": 4.0, "add_stopped_enabled": true, "max_ratio": 2.0,
		"max_seeding_time_enabled": true, "max_seeding_time": 60.0,
	} {
		if p[k] != want {
			t.Errorf("%s = %v, want %v", k, p[k], want)
		}
	}

	// qbit_manage's write: the flags alone.
	expect(t, "flags", e.setPreferences(map[string]any{
		"max_ratio_act": 0, "max_seeding_time_enabled": false, "max_inactive_seeding_time_enabled": false,
	}), http.StatusOK)
	if p := e.preferences(); p["max_seeding_time_enabled"] != false || p["max_inactive_seeding_time_enabled"] != false {
		t.Fatalf("flags not applied: %v %v", p["max_seeding_time_enabled"], p["max_inactive_seeding_time_enabled"])
	}

	// A bad value fails the whole request.
	expect(t, "bad value", e.setPreferences(map[string]any{"queueing_enabled": false, "dl_limit": "fast"}), http.StatusBadRequest)
	if p := e.preferences(); p["queueing_enabled"] != true {
		t.Fatal("a failed request applied a key")
	}
	expect(t, "bad hour", e.setPreferences(map[string]any{"schedule_to_hour": 24}), http.StatusBadRequest)
	expect(t, "encryption as a string", e.setPreferences(map[string]any{"encryption": "1"}), http.StatusBadRequest)
	expect(t, "max_ratio_act as a fraction", e.setPreferences(map[string]any{"max_ratio_act": 1.5}), http.StatusBadRequest)
	expect(t, "no json", e.post("app/setPreferences", nil), http.StatusBadRequest)
	// qBittorrent 5.3.0 ignores a value that isn't an object.
	expect(t, "not an object", e.post("app/setPreferences", url.Values{"json": {"[1]"}}), http.StatusOK)
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

func TestAPIKeyRotation(t *testing.T) {
	e := newEnv(t)
	e.login()
	if m := e.preferences()["web_ui_api_key"]; m != "" {
		t.Fatalf("marker before a key: %q", m)
	}
	var out map[string]string
	r := e.post("app/rotateAPIKey", nil)
	expect(t, "rotate", r, http.StatusOK)
	r.json(t, &out)
	key := out["apiKey"]
	anon := newEnvClient(e)
	expect(t, "new key", anon.get("app/version", "Authorization", "Bearer "+key), http.StatusOK)
	marker, _ := e.preferences()["web_ui_api_key"].(string)
	if marker == "" || strings.Contains(marker, key) {
		t.Fatalf("marker %q for key %q", marker, key)
	}
	expect(t, "rotate with the key", anon.post("app/rotateAPIKey", nil, "Authorization", "Bearer "+key), http.StatusForbidden)

	e.post("app/rotateAPIKey", nil).json(t, &out)
	expect(t, "old key", anon.get("app/version", "Authorization", "Bearer "+key), http.StatusForbidden)
	expect(t, "delete", e.post("app/deleteAPIKey", nil), http.StatusNoContent)
	expect(t, "deleted key", anon.get("app/version", "Authorization", "Bearer "+out["apiKey"]), http.StatusForbidden)
	if m := e.preferences()["web_ui_api_key"]; m != "" {
		t.Fatalf("marker after delete: %q", m)
	}
}

func TestClientData(t *testing.T) {
	e := newEnv(t)
	e.login()
	load := func(keys string) map[string]any {
		var out map[string]any
		q := url.Values{}
		if keys != "" {
			q.Set("keys", keys)
		}
		e.get("clientdata/load?"+q.Encode()).json(t, &out)
		return out
	}
	expect(t, "store", e.post("clientdata/store", url.Values{"data": {`{"a": 1, "b": {"c": [2]}}`}}), http.StatusNoContent)
	if got := load(""); len(got) != 2 {
		t.Fatalf("load all: %v", got)
	}
	if got := load(`["a", "z"]`); len(got) != 1 || got["a"] != 1.0 {
		t.Fatalf("load keys: %v", got)
	}
	expect(t, "delete", e.post("clientdata/store", url.Values{"data": {`{"a": null}`}}), http.StatusNoContent)
	if got := load(""); len(got) != 1 || got["b"] == nil {
		t.Fatalf("after delete: %v", got)
	}
	expect(t, "not an object", e.post("clientdata/store", url.Values{"data": {`"x"`}}), http.StatusBadRequest)
	expect(t, "bad keys", e.get("clientdata/load?keys=nope"), http.StatusBadRequest)
	big := `{"big": "` + strings.Repeat("x", clientDataLimit) + `"}`
	expect(t, "too large", e.post("clientdata/store", url.Values{"data": {big}}), http.StatusConflict)
}

func TestCookies(t *testing.T) {
	e := newEnv(t)
	e.login()
	var cs []map[string]any
	e.get("app/cookies").json(t, &cs)
	if cs == nil || len(cs) != 0 {
		t.Fatalf("cookies: %v", cs)
	}
	jar := `[{"name": "uid", "domain": ".tracker.example", "path": "/", "value": "42", "expirationDate": 0}]`
	expect(t, "setCookies", e.post("app/setCookies", url.Values{"cookies": {jar}}), http.StatusOK)
	e.get("app/cookies").json(t, &cs)
	if len(cs) != 1 || cs[0]["value"] != "42" {
		t.Fatalf("cookies after set: %v", cs)
	}
	expect(t, "not an array", e.post("app/setCookies", url.Values{"cookies": {`{}`}}), http.StatusBadRequest)
}

func TestAppLeftovers(t *testing.T) {
	e := newEnv(t)
	e.login()
	var info map[string]int64
	// Reads accept POST.
	e.post("app/processInfo", nil).json(t, &info)
	if time.Since(time.Unix(info["launch_time"], 0)) > time.Minute {
		t.Fatalf("processInfo: %v", info)
	}
	r := e.get("app/getFreeSpaceAtPath?path=" + url.QueryEscape(filepath.Join(e.save, "not", "yet")))
	expect(t, "free space", r, http.StatusOK)
	if r.body[0] == '-' {
		t.Fatalf("free space %s", r.body)
	}
	expect(t, "free space outside", e.get("app/getFreeSpaceAtPath?path=/etc"), http.StatusNotFound)
	expect(t, "free space no path", e.get("app/getFreeSpaceAtPath"), http.StatusBadRequest)
	r = e.get("app/setPreferences")
	expect(t, "GET on a POST endpoint", r, http.StatusMethodNotAllowed)
	if r.header.Get("Allow") != http.MethodPost {
		t.Fatalf("Allow %q", r.header.Get("Allow"))
	}
	expect(t, "shutdown", e.post("app/shutdown", nil), http.StatusConflict)
	var build map[string]any
	e.get("app/buildInfo").json(t, &build)
	if !slices.Contains([]any{"linux", "macos", "windows", "unknown"}, build["platform"]) {
		t.Fatalf("platform %v", build["platform"])
	}
	if lib, _ := build["libtorrent"].(string); !strings.HasPrefix(lib, "anacrolix/torrent v") {
		t.Fatalf("libtorrent %q", lib)
	}
}

func TestDownloadFile(t *testing.T) {
	e := newEnv(t)
	e.login()
	seed := enginetest.NewSeeder(t, "Show", map[string]int{"e01.mkv": 64 << 10, "e02.mkv": 64 << 10})
	for p, b := range seed.Content {
		full := filepath.Join(e.save, seed.Name, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expect(t, "add", e.postMultipart("torrents/add", map[string]string{"skip_checking": "true"}, seed.Metainfo), http.StatusOK)
	q := func(file string) string {
		return "torrents/downloadFile?" + url.Values{"hash": {seed.Hash}, "file": {file}}.Encode()
	}
	deadline := time.Now().Add(10 * time.Second)
	var r response
	for {
		if r = e.get(q("Show/e02.mkv")); r.status == http.StatusOK || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	expect(t, "by path", r, http.StatusOK)
	if !bytes.Equal(r.body, seed.Content["e02.mkv"]) || r.header.Get("Content-Disposition") != `attachment; filename=e02.mkv` {
		t.Fatalf("by path: %d bytes, %q", len(r.body), r.header.Get("Content-Disposition"))
	}
	r = e.get(q("0"), "Range", "bytes=0-9")
	expect(t, "range", r, http.StatusPartialContent)
	if !bytes.Equal(r.body, seed.Content["e01.mkv"][:10]) {
		t.Fatal("range content")
	}
	expect(t, "no such file", e.get(q("5")), http.StatusConflict)
	expect(t, "no file", e.get("torrents/downloadFile?hash="+seed.Hash), http.StatusBadRequest)
	expect(t, "unknown torrent", e.get("torrents/downloadFile?file=0&hash="+strings.Repeat("0", 40)), http.StatusNotFound)

	r = e.get("torrents/export?hash=" + seed.Hash)
	if r.header.Get("Content-Disposition") != `attachment; filename=Show.torrent` {
		t.Fatalf("export: %q", r.header.Get("Content-Disposition"))
	}
	var ssl map[string]string
	e.get("torrents/SSLParameters?hash="+seed.Hash).json(t, &ssl)
	if len(ssl) != 3 || ssl["ssl_certificate"] != "" {
		t.Fatalf("SSL parameters: %v", ssl)
	}
	expect(t, "setSSLParameters", e.post("torrents/setSSLParameters", url.Values{"hash": {seed.Hash}}), http.StatusConflict)
}

// TestPreferenceTypes checks app/preferences against qBittorrent 5.3.0's key
// list: clients that decode into typed fields fail on a wrong type, and
// VueTorrent tells qBittorrent forks apart by extra keys.
func TestPreferenceTypes(t *testing.T) {
	e := newEnv(t)
	e.login()
	p := e.preferences()
	for k, v := range p {
		var ok bool
		switch typ := preferenceTypes[k]; typ {
		case "int":
			f, isNum := v.(float64)
			ok = isNum && f == float64(int64(f))
		case "number":
			_, ok = v.(float64)
		case "bool":
			_, ok = v.(bool)
		case "string":
			_, ok = v.(string)
		case "object":
			_, ok = v.(map[string]any)
		case "array":
			_, ok = v.([]any)
		case "":
			t.Errorf("%s is not a qBittorrent preference", k)
			continue
		}
		if !ok {
			t.Errorf("%s = %#v, want a %s", k, v, preferenceTypes[k])
		}
	}
	for k := range preferenceTypes {
		if _, ok := p[k]; !ok && k != "web_ui_password" {
			t.Errorf("%s missing", k)
		}
	}
}

// TestNonAdminWrites checks that users who aren't admins can change the
// speed limits through the qBittorrent API but not instance-wide settings.
func TestNonAdminWrites(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	u, err := e.store.CreateUser(ctx, store.CreateUserParams{Username: "bob", CreatedAt: time.Now().Unix()})
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := e.auth.CreateAPIKey(ctx, &auth.Principal{UserID: u.ID, Username: "bob"}, "bob's arr", auth.ScopeQBittorrent)
	if err != nil {
		t.Fatal(err)
	}
	bearer := []string{"Authorization", "Bearer " + key}
	b, _ := json.Marshal(map[string]any{"dl_limit": 2048, "dht": true, "banned_IPs": "203.0.113.1"})
	expect(t, "setPreferences", e.post("app/setPreferences", url.Values{"json": {string(b)}}, bearer...), http.StatusOK)
	expect(t, "banPeers", e.post("transfer/banPeers", url.Values{"peers": {"203.0.113.2:1"}}, bearer...), http.StatusForbidden)
	expect(t, "setCookies", e.post("app/setCookies", url.Values{"cookies": {"[]"}}, bearer...), http.StatusForbidden)
	expect(t, "cookies", e.get("app/cookies", bearer...), http.StatusForbidden)
	expect(t, "log/main", e.get("log/main", bearer...), http.StatusForbidden)
	e.login()
	p := e.preferences()
	if p["dl_limit"] != 2048.0 || p["dht"] != false || p["banned_IPs"] != "" {
		t.Fatalf("after a user's writes: dl_limit %v, dht %v, banned %q", p["dl_limit"], p["dht"], p["banned_IPs"])
	}
}

// TestAdminEndpointsNeedFullScope checks that keys an admin made for an
// *arr app or a dashboard can't use admin endpoints, as in the native API.
func TestAdminEndpointsNeedFullScope(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, p, err := e.auth.Login(ctx, httptest.NewRequest(http.MethodPost, "/", nil), "admin", "adminadmin", auth.KindWeb)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[auth.Scope]string{}
	for _, scope := range []auth.Scope{auth.ScopeFull, auth.ScopeQBittorrent, auth.ScopeReadOnly} {
		_, k, err := e.auth.CreateAPIKey(ctx, p, string(scope), scope)
		if err != nil {
			t.Fatal(err)
		}
		keys[scope] = k
	}
	routes := []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodGet, "app/cookies", nil},
		{http.MethodPost, "app/setCookies", url.Values{"cookies": {"[]"}}},
		{http.MethodGet, "log/main", nil},
		{http.MethodPost, "transfer/banPeers", url.Values{"peers": {"203.0.113.9:1"}}},
		{http.MethodPost, "rss/addFolder", url.Values{"path": {"F"}}},
		{http.MethodPost, "rss/setRule", url.Values{"ruleName": {"r"}, "ruleDef": {"{}"}}},
	}
	for scope, key := range keys {
		bearer := []string{"Authorization", "Bearer " + key}
		for _, rt := range routes {
			var r response
			if rt.method == http.MethodGet {
				r = e.get(rt.path, bearer...)
			} else {
				r = e.post(rt.path, rt.form, bearer...)
			}
			allowed := r.status != http.StatusForbidden
			if allowed != (scope == auth.ScopeFull) {
				t.Errorf("%s key, %s: status %d", scope, rt.path, r.status)
			}
		}
		// setPreferences takes only the speed limits from keys that aren't
		// full.
		b, _ := json.Marshal(map[string]any{"dht": true})
		e.post("app/setPreferences", url.Values{"json": {string(b)}}, bearer...)
	}
	e.login()
	if p := e.preferences(); p["dht"] != true {
		t.Fatalf("a full key couldn't change DHT: %v", p["dht"])
	}
	b, _ := json.Marshal(map[string]any{"dht": false})
	e.post("app/setPreferences", url.Values{"json": {string(b)}}, "Authorization", "Bearer "+keys[auth.ScopeQBittorrent])
	if p := e.preferences(); p["dht"] != true {
		t.Fatal("a qbittorrent key changed DHT")
	}
}
