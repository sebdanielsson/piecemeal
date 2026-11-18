//go:build e2e

// Package e2e tests piecemeal against real Radarr, Sonarr, Prowlarr and
// qBittorrent containers. Run it with test/e2e/run.sh, which starts the
// stack in compose.yaml.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

const piecemealURL = "http://127.0.0.1:18080"

// client is an HTTP client with a cookie jar and optional headers.
type client struct {
	t       *testing.T
	base    string
	headers map[string]string
	http    *http.Client
}

func newClient(t *testing.T, base string, headers map[string]string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, headers: headers, http: &http.Client{Jar: jar, Timeout: 60 * time.Second}}
}

func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	status, raw, err := c.raw(method, path, body)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: status %d, decoding %q: %v", method, path, status, raw, err)
		}
	}
	if status >= 400 {
		c.t.Logf("%s %s: status %d: %s", method, path, status, raw)
	}
	return status
}

// raw sends a request and returns the status and body. A string body is
// sent form-encoded, anything else as JSON.
func (c *client) raw(method, path string, body any) (int, []byte, error) {
	var r io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case string:
		r, contentType = strings.NewReader(b), "application/x-www-form-urlencoded"
	default:
		j, _ := json.Marshal(b)
		r, contentType = bytes.NewReader(j), "application/json"
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Second)
	}
}

func password() string {
	if p := os.Getenv("E2E_PIECEMEAL_PASSWORD"); p != "" {
		return p
	}
	return "e2e-admin-password"
}

// piecemeal signs in to the native API.
func piecemeal(t *testing.T) *client {
	c := newClient(t, piecemealURL+"/api/piecemeal/v1", map[string]string{"X-Requested-With": "piecemeal"})
	waitFor(t, "piecemeal", 2*time.Minute, func() bool {
		resp, err := http.Get(piecemealURL + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	if s := c.do(http.MethodPost, "/auth/login", map[string]string{"username": "admin", "password": password()}, nil); s != http.StatusOK {
		t.Fatalf("piecemeal login: %d", s)
	}
	return c
}

type arrApp struct {
	name     string
	url      string
	api      string // API path prefix
	key      string // from the config.xml run.sh writes
	category string
}

var apps = []arrApp{
	{"radarr", "http://127.0.0.1:17878", "/api/v3", "e2e00000000000000000000000007878", "radarr"},
	{"sonarr", "http://127.0.0.1:18989", "/api/v3", "e2e00000000000000000000000008989", "tv-sonarr"},
	{"prowlarr", "http://127.0.0.1:19696", "/api/v1", "e2e00000000000000000000000009696", "prowlarr"},
}

func (a arrApp) client(t *testing.T) *client {
	c := newClient(t, a.url+a.api, map[string]string{"X-Api-Key": a.key})
	waitFor(t, a.name, 3*time.Minute, func() bool {
		status, _, err := c.raw(http.MethodGet, "/system/status", nil)
		return err == nil && status == http.StatusOK
	})
	return c
}

// downloadClient builds a qBittorrent download client from the app's schema.
func downloadClient(t *testing.T, c *client, a arrApp, fields map[string]any) map[string]any {
	t.Helper()
	var schemas []map[string]any
	if s := c.do(http.MethodGet, "/downloadclient/schema", nil, &schemas); s != http.StatusOK {
		t.Fatalf("%s schema: %d", a.name, s)
	}
	for _, sc := range schemas {
		if sc["implementation"] != "QBittorrent" {
			continue
		}
		sc["name"] = "piecemeal"
		sc["enable"] = true
		sc["removeCompletedDownloads"] = true
		sc["removeFailedDownloads"] = true
		sc["priority"] = 1
		sc["tags"] = []any{}
		for _, f := range sc["fields"].([]any) {
			field := f.(map[string]any)
			name := field["name"].(string)
			if v, ok := fields[name]; ok {
				field["value"] = v
			}
			// movieCategory, tvCategory, category: the main category.
			if strings.HasSuffix(strings.ToLower(name), "category") && !strings.Contains(strings.ToLower(name), "imported") {
				field["value"] = a.category
			}
		}
		return sc
	}
	t.Fatalf("%s has no QBittorrent download client", a.name)
	return nil
}

// TestArrTestAction runs each app's "Test" on a download client pointing
// at piecemeal, with a username and password, then with an API key.
func TestArrTestAction(t *testing.T) {
	pm := piecemeal(t)
	var key struct{ Key string }
	if s := pm.do(http.MethodPost, "/api-keys", map[string]string{"name": "arr", "scope": "qbittorrent"}, &key); s != http.StatusCreated {
		t.Fatalf("create API key: %d", s)
	}
	for _, a := range apps {
		t.Run(a.name, func(t *testing.T) {
			c := a.client(t)
			withPassword := downloadClient(t, c, a, map[string]any{
				"host": "piecemeal", "port": 8080, "username": "admin", "password": password(),
			})
			if s := c.do(http.MethodPost, "/downloadclient/test", withPassword, nil); s/100 != 2 {
				t.Fatalf("test with password: %d", s)
			}
			withKey := downloadClient(t, c, a, map[string]any{
				"host": "piecemeal", "port": 8080, "username": "", "password": "", "apiKey": key.Key,
			})
			if s := c.do(http.MethodPost, "/downloadclient/test", withKey, nil); s/100 != 2 {
				t.Fatalf("test with API key: %d", s)
			}
			// Saving runs the test again and keeps the client for the grab test.
			if s := c.do(http.MethodPost, "/downloadclient", withPassword, nil); s != http.StatusCreated {
				t.Fatalf("save download client: %d", s)
			}
		})
	}
}

// TestRadarrGrab pushes a release to Radarr, which sends it to piecemeal.
// piecemeal downloads it from the seeder and stops at its seed goal (ratio
// 0); Radarr imports the file and removes the finished torrent.
func TestRadarrGrab(t *testing.T) {
	pm := piecemeal(t)
	if s := pm.do(http.MethodPatch, "/settings", map[string]any{"ratioLimit": 0}, nil); s != http.StatusOK {
		t.Fatalf("set the seed goal: %d", s)
	}
	a := apps[0]
	c := a.client(t)

	var clients []map[string]any
	c.do(http.MethodGet, "/downloadclient", nil, &clients)
	if len(clients) == 0 {
		if s := c.do(http.MethodPost, "/downloadclient", downloadClient(t, c, a, map[string]any{
			"host": "piecemeal", "port": 8080, "username": "admin", "password": password(),
		}), nil); s != http.StatusCreated {
			t.Fatalf("save download client: %d", s)
		}
	}
	if s := c.do(http.MethodPost, "/rootfolder", map[string]string{"path": "/movies"}, nil); s != http.StatusCreated && s != http.StatusBadRequest {
		t.Fatalf("root folder: %d", s)
	}
	var profiles []map[string]any
	c.do(http.MethodGet, "/qualityprofile", nil, &profiles)
	if len(profiles) == 0 {
		t.Fatal("no quality profiles")
	}
	var movie map[string]any
	s := c.do(http.MethodPost, "/movie", map[string]any{
		"title": "Big Buck Bunny", "tmdbId": 10378, "year": 2008, "qualityProfileId": profiles[0]["id"],
		"rootFolderPath": "/movies", "monitored": true, "minimumAvailability": "released",
		"addOptions": map[string]any{"searchForMovie": false},
	}, &movie)
	if s != http.StatusCreated {
		// This step needs Radarr's metadata service on the internet, not
		// piecemeal; an outage there shouldn't fail piecemeal's CI.
		t.Skipf("adding the movie failed (%d): Radarr's metadata service may be down", s)
	}

	magnet, err := os.ReadFile("tmp/shared/seed.magnet")
	if err != nil {
		t.Fatal(err)
	}
	hashBytes, err := os.ReadFile("tmp/shared/seed.hash")
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(hashBytes))
	var decisions []map[string]any
	body := map[string]any{
		"title": "Big.Buck.Bunny.2008.1080p.BluRay.x264-PIECEMEAL", "magnetUrl": strings.TrimSpace(string(magnet)),
		"protocol": "torrent", "downloadProtocol": "torrent", "publishDate": time.Now().UTC().Format(time.RFC3339),
		"size": int64(8) << 30, "indexer": "e2e", "seeders": 10, "leechers": 0,
	}
	raw := []any{}
	status := c.do(http.MethodPost, "/release/push", body, &raw)
	if status != http.StatusOK {
		t.Fatalf("release push: %d", status)
	}
	for _, d := range raw {
		if m, ok := d.(map[string]any); ok {
			decisions = append(decisions, m)
		}
	}
	if len(decisions) == 0 {
		t.Fatalf("release push returned no decision: %v", raw)
	}
	if rejected, _ := decisions[0]["rejected"].(bool); rejected {
		t.Fatalf("Radarr rejected the release: %v", decisions[0]["rejections"])
	}

	// piecemeal got it, in Radarr's category, and downloads it.
	waitFor(t, "piecemeal to finish the download", 3*time.Minute, func() bool {
		var tor map[string]any
		if pm.do(http.MethodGet, "/torrents/"+hash, nil, &tor) != http.StatusOK {
			return false
		}
		if tor["category"] != "radarr" {
			t.Fatalf("category %v", tor["category"])
		}
		return tor["complete"] == true
	})

	// Radarr polls, imports the movie and removes the torrent, which the
	// seed goal stopped.
	last := ""
	waitFor(t, "Radarr to import the movie", 5*time.Minute, func() bool {
		c.do(http.MethodPost, "/command", map[string]string{"name": "RefreshMonitoredDownloads"}, nil)
		var q struct {
			Records []map[string]any `json:"records"`
		}
		c.do(http.MethodGet, "/queue?includeMovie=true", nil, &q)
		for _, r := range q.Records {
			if strings.EqualFold(fmt.Sprint(r["downloadId"]), hash) {
				state := fmt.Sprintf("queue: status=%v trackedDownloadState=%v trackedDownloadStatus=%v outputPath=%v messages=%v",
					r["status"], r["trackedDownloadState"], r["trackedDownloadStatus"], r["outputPath"], r["statusMessages"])
				if state != last {
					t.Log(state)
					last = state
				}
			}
		}
		var m map[string]any
		c.do(http.MethodGet, fmt.Sprintf("/movie/%v", movie["id"]), nil, &m)
		return m["hasFile"] == true
	})
	waitFor(t, "Radarr to remove the finished torrent", 3*time.Minute, func() bool {
		c.do(http.MethodPost, "/command", map[string]string{"name": "RefreshMonitoredDownloads"}, nil)
		status, _, err := pm.raw(http.MethodGet, "/torrents/"+hash, nil)
		return err == nil && status == http.StatusNotFound
	})
}

// TestContractQBittorrent sends the same calls to qBittorrent 5.3.0 and to
// piecemeal and compares status codes and response shapes.
func TestContractQBittorrent(t *testing.T) {
	qbPass := qbittorrentPassword(t)
	qb := newClient(t, "http://127.0.0.1:18081/api/v2", nil)
	pm := newClient(t, piecemealURL+"/api/v2", nil)
	piecemeal(t)

	type call struct {
		name, method, path, form string
		// shape compares JSON key sets and types, not just the status.
		shape bool
		// required keys the *arrs read, which piecemeal must have.
		required []string
		// differs is why piecemeal deliberately answers differently; the
		// difference is logged, not failed.
		differs string
		// sub compares the shape of this field of the response instead.
		sub string
		// same compares the bodies as text.
		same bool
	}
	const magnet = "magnet:?xt=urn:btih:5555555555555555555555555555555555555555&dn=contract"
	calls := []call{
		{name: "probe without auth", method: http.MethodGet, path: "/app/webapiVersion"},
		{name: "wrong password", method: http.MethodPost, path: "/auth/login", form: "username=admin&password=wrong"},
		{name: "torrents without auth", method: http.MethodGet, path: "/torrents/info"},
		{name: "login", method: http.MethodPost, path: "/auth/login", form: "username=admin&password=%s"},
		{name: "webapiVersion", method: http.MethodGet, path: "/app/webapiVersion"},
		{name: "preferences", method: http.MethodGet, path: "/app/preferences", shape: true, required: []string{
			"save_path", "max_ratio_enabled", "max_ratio", "max_seeding_time_enabled", "max_seeding_time",
			"max_inactive_seeding_time_enabled", "max_inactive_seeding_time", "max_ratio_act", "queueing_enabled", "dht",
		}},
		{name: "categories empty", method: http.MethodGet, path: "/torrents/categories"},
		{name: "createCategory", method: http.MethodPost, path: "/torrents/createCategory", form: "category=contract"},
		{name: "createCategory again", method: http.MethodPost, path: "/torrents/createCategory", form: "category=contract"},
		{name: "categories", method: http.MethodGet, path: "/torrents/categories", shape: true},
		{name: "add magnet stopped", method: http.MethodPost, path: "/torrents/add", form: "urls=" + magnet + "&category=contract&stopped=True"},
		{name: "add duplicate", method: http.MethodPost, path: "/torrents/add", form: "urls=" + magnet + "&category=contract&stopped=True"},
		{name: "info", method: http.MethodGet, path: "/torrents/info?category=contract", shape: true, required: []string{
			"hash", "name", "size", "progress", "eta", "state", "category", "save_path", "content_path",
			"root_path", "ratio", "ratio_limit", "seeding_time", "seeding_time_limit",
			"inactive_seeding_time_limit", "last_activity",
		}},
		{name: "properties", method: http.MethodGet, path: "/torrents/properties?hash=5555555555555555555555555555555555555555", shape: true, required: []string{"save_path", "seeding_time"}},
		{name: "properties unknown", method: http.MethodGet, path: "/torrents/properties?hash=6666666666666666666666666666666666666666"},
		{name: "files without metadata", method: http.MethodGet, path: "/torrents/files?hash=5555555555555555555555555555555555555555"},
		{name: "addTags", method: http.MethodPost, path: "/torrents/addTags", form: "hashes=5555555555555555555555555555555555555555&tags=a,b"},
		{name: "setForceStart", method: http.MethodPost, path: "/torrents/setForceStart", form: "hashes=5555555555555555555555555555555555555555&value=false"},
		{name: "setCategory missing", method: http.MethodPost, path: "/torrents/setCategory", form: "hashes=5555555555555555555555555555555555555555&category=missing"},
		// Phase 4: what VueTorrent and qbit_manage use.
		{name: "maindata", method: http.MethodGet, path: "/sync/maindata?rid=0", shape: true},
		{name: "maindata server_state", method: http.MethodGet, path: "/sync/maindata?rid=0", shape: true, sub: "server_state"},
		{name: "maindata torrents", method: http.MethodGet, path: "/sync/maindata?rid=0", shape: true, sub: "torrents"},
		{name: "torrentPeers", method: http.MethodGet, path: "/sync/torrentPeers?rid=0&hash=5555555555555555555555555555555555555555", shape: true},
		{name: "info by POST", method: http.MethodPost, path: "/torrents/info", form: "category=contract"},
		{name: "addTags by GET", method: http.MethodGet, path: "/torrents/addTags?hashes=all&tags=x"},
		{name: "count", method: http.MethodGet, path: "/torrents/count", same: true},
		{name: "transfer info", method: http.MethodGet, path: "/transfer/info", shape: true},
		{name: "setDownloadLimit", method: http.MethodPost, path: "/transfer/setDownloadLimit", form: "limit=5000"},
		{name: "downloadLimit rounded", method: http.MethodGet, path: "/transfer/downloadLimit", same: true},
		{name: "setSpeedLimits", method: http.MethodPost, path: "/transfer/setSpeedLimits", form: "dl_limit=0&up_limit=0&alt_dl_limit=10240&alt_up_limit=10240"},
		{name: "getSpeedLimits", method: http.MethodGet, path: "/transfer/getSpeedLimits", same: true},
		{name: "setSpeedLimitsMode bad", method: http.MethodPost, path: "/transfer/setSpeedLimitsMode", form: "mode=2"},
		{name: "setPreferences flags", method: http.MethodPost, path: "/app/setPreferences", form: `json={"max_ratio_act":0,"max_ratio_enabled":false,"max_seeding_time_enabled":false,"max_inactive_seeding_time_enabled":false}`},
		{name: "setPreferences bad json", method: http.MethodPost, path: "/app/setPreferences", form: "json=nope"},
		{name: "processInfo", method: http.MethodGet, path: "/app/processInfo", shape: true},
		{name: "buildInfo", method: http.MethodGet, path: "/app/buildInfo", shape: true},
		{name: "getFreeSpaceAtPath", method: http.MethodGet, path: "/app/getFreeSpaceAtPath?path=/downloads"},
		{name: "setCookies", method: http.MethodPost, path: "/app/setCookies", form: "cookies=[]"},
		{name: "cookies", method: http.MethodGet, path: "/app/cookies", same: true},
		{name: "clientdata store", method: http.MethodPost, path: "/clientdata/store", form: `data={"contract":{"a":[1]}}`},
		{name: "clientdata load", method: http.MethodGet, path: "/clientdata/load?keys=%5B%22contract%22%5D", same: true},
		{name: "clientdata bad", method: http.MethodPost, path: "/clientdata/store", form: "data=[1]"},
		{name: "log main", method: http.MethodGet, path: "/log/main?last_known_id=-1", shape: true},
		{name: "addTrackers", method: http.MethodPost, path: "/torrents/addTrackers", form: "hash=5555555555555555555555555555555555555555&urls=udp://tracker.invalid:1337"},
		{name: "trackers", method: http.MethodGet, path: "/torrents/trackers?hash=5555555555555555555555555555555555555555", shape: true},
		{name: "editTracker missing", method: http.MethodPost, path: "/torrents/editTracker", form: "hash=5555555555555555555555555555555555555555&url=udp://none.invalid:1&newUrl=udp://other.invalid:1"},
		{name: "removeTrackers encoded", method: http.MethodPost, path: "/torrents/removeTrackers", form: "hash=5555555555555555555555555555555555555555&urls=" + url.QueryEscape(url.QueryEscape("udp://tracker.invalid:1337"))},
		{name: "webseeds", method: http.MethodGet, path: "/torrents/webseeds?hash=5555555555555555555555555555555555555555", same: true},
		{name: "setTags", method: http.MethodPost, path: "/torrents/setTags", form: "hashes=5555555555555555555555555555555555555555&tags=c"},
		{name: "export without metadata", method: http.MethodGet, path: "/torrents/export?hash=5555555555555555555555555555555555555555"},
		{name: "pieceHashes without metadata", method: http.MethodGet, path: "/torrents/pieceHashes?hash=5555555555555555555555555555555555555555", same: true},
		{name: "SSLParameters", method: http.MethodGet, path: "/torrents/SSLParameters?hash=5555555555555555555555555555555555555555"},
		{name: "downloadLimit per torrent", method: http.MethodGet, path: "/torrents/downloadLimit?hashes=5555555555555555555555555555555555555555", same: true},
		{name: "setSpeedLimitsMode 2", method: http.MethodPost, path: "/transfer/setSpeedLimitsMode", form: "mode=2"},
		{name: "speedLimitsMode after 2", method: http.MethodGet, path: "/transfer/speedLimitsMode", same: true},
		{name: "toggleSpeedLimitsMode", method: http.MethodPost, path: "/transfer/toggleSpeedLimitsMode"},
		{name: "speedLimitsMode after toggle", method: http.MethodGet, path: "/transfer/speedLimitsMode", same: true},
		{name: "banPeers", method: http.MethodPost, path: "/transfer/banPeers", form: "peers=203.0.113.9:6881"},
		{name: "setPreferences no json", method: http.MethodPost, path: "/app/setPreferences"},
		{name: "setPreferences queueing", method: http.MethodPost, path: "/app/setPreferences", form: `json={"queueing_enabled":true}`},
		{name: "topPrio", method: http.MethodPost, path: "/torrents/topPrio", form: "hashes=5555555555555555555555555555555555555555"},
		{name: "stop", method: http.MethodPost, path: "/torrents/stop", form: "hashes=5555555555555555555555555555555555555555"},
		{name: "start", method: http.MethodPost, path: "/torrents/start", form: "hashes=5555555555555555555555555555555555555555"},
		{name: "reannounce", method: http.MethodPost, path: "/torrents/reannounce", form: "hashes=5555555555555555555555555555555555555555"},
		{name: "setShareLimits", method: http.MethodPost, path: "/torrents/setShareLimits", form: "hashes=5555555555555555555555555555555555555555&ratioLimit=-2&seedingTimeLimit=-2&inactiveSeedingTimeLimit=-2&shareLimitAction=Default&shareLimitsMode=Default"},
		{name: "setShareLimits without 5.x fields", method: http.MethodPost, path: "/torrents/setShareLimits", form: "hashes=5555555555555555555555555555555555555555&ratioLimit=-2&seedingTimeLimit=-2&inactiveSeedingTimeLimit=-2", differs: "accepted, for clients older than qBittorrent 5"},
		{name: "setDownloadLimit per torrent", method: http.MethodPost, path: "/torrents/setDownloadLimit", form: "hashes=5555555555555555555555555555555555555555&limit=0"},
		{name: "toggleSequentialDownload", method: http.MethodPost, path: "/torrents/toggleSequentialDownload", form: "hashes=5555555555555555555555555555555555555555"},
		{name: "toggleFirstLastPiecePrio", method: http.MethodPost, path: "/torrents/toggleFirstLastPiecePrio", form: "hashes=5555555555555555555555555555555555555555"},
		{name: "rename", method: http.MethodPost, path: "/torrents/rename", form: "hash=5555555555555555555555555555555555555555&name=renamed"},
		{name: "removeTags", method: http.MethodPost, path: "/torrents/removeTags", form: "hashes=5555555555555555555555555555555555555555&tags=c"},
		{name: "createTags", method: http.MethodPost, path: "/torrents/createTags", form: "tags=x,y"},
		{name: "deleteTags", method: http.MethodPost, path: "/torrents/deleteTags", form: "tags=x,y"},
		{name: "editCategory", method: http.MethodPost, path: "/torrents/editCategory", form: "category=contract&savePath="},
		{name: "editCategory missing", method: http.MethodPost, path: "/torrents/editCategory", form: "category=missing&savePath="},
		{name: "createCategory other", method: http.MethodPost, path: "/torrents/createCategory", form: "category=other"},
		{name: "removeCategories", method: http.MethodPost, path: "/torrents/removeCategories", form: "categories=other"},
		{name: "setAutoManagement", method: http.MethodPost, path: "/torrents/setAutoManagement", form: "hashes=5555555555555555555555555555555555555555&enable=false"},
		{name: "addWebSeeds", method: http.MethodPost, path: "/torrents/addWebSeeds", form: "hash=5555555555555555555555555555555555555555&urls=https://ws.invalid/a"},
		{name: "removeWebSeeds", method: http.MethodPost, path: "/torrents/removeWebSeeds", form: "hash=5555555555555555555555555555555555555555&urls=https://ws.invalid/a"},
		{name: "addPeers", method: http.MethodPost, path: "/torrents/addPeers", form: "hashes=5555555555555555555555555555555555555555&peers=203.0.113.1:6881|bad", same: true},
		{name: "addPeers invalid", method: http.MethodPost, path: "/torrents/addPeers", form: "hashes=5555555555555555555555555555555555555555&peers=nope"},
		{name: "filePrio without metadata", method: http.MethodPost, path: "/torrents/filePrio", form: "hash=5555555555555555555555555555555555555555&id=0&priority=1"},
		{name: "renameFile without metadata", method: http.MethodPost, path: "/torrents/renameFile", form: "hash=5555555555555555555555555555555555555555&oldPath=a&newPath=b"},
		{name: "setLocation", method: http.MethodPost, path: "/torrents/setLocation", form: "hashes=5555555555555555555555555555555555555555&location=/downloads/moved", differs: "qBittorrent's container can't write /downloads"},
		{name: "setLocation empty", method: http.MethodPost, path: "/torrents/setLocation", form: "hashes=5555555555555555555555555555555555555555&location="},
		{name: "rotateAPIKey", method: http.MethodPost, path: "/app/rotateAPIKey", shape: true},
		{name: "deleteAPIKey", method: http.MethodPost, path: "/app/deleteAPIKey"},
		{name: "delete", method: http.MethodPost, path: "/torrents/delete", form: "hashes=5555555555555555555555555555555555555555&deleteFiles=false"},
		{name: "unknown endpoint", method: http.MethodGet, path: "/app/nope"},
	}
	var failures []string
	for _, c := range calls {
		qbForm, pmForm := c.form, c.form
		if strings.Contains(c.form, "%s") {
			qbForm, pmForm = fmt.Sprintf(c.form, qbPass), fmt.Sprintf(c.form, password())
		}
		var qbBody, pmBody any
		qbStatus, qbText := doForm(t, qb, c.method, c.path, qbForm, &qbBody)
		pmStatus, pmText := doForm(t, pm, c.method, c.path, pmForm, &pmBody)
		t.Logf("%s: qBittorrent %d %.120q, piecemeal %d %.120q", c.name, qbStatus, qbText, pmStatus, pmText)
		if c.differs != "" {
			t.Logf("%s: differs on purpose: %s", c.name, c.differs)
			continue
		}
		if qbStatus != pmStatus {
			failures = append(failures, fmt.Sprintf("%s: status qBittorrent %d, piecemeal %d", c.name, qbStatus, pmStatus))
		}
		if c.name == "add magnet stopped" {
			// qBittorrent adds magnets in the background; give it a moment.
			time.Sleep(2 * time.Second)
		}
		if c.same && !sameJSON(qbText, pmText) {
			failures = append(failures, fmt.Sprintf("%s: body qBittorrent %q, piecemeal %q", c.name, qbText, pmText))
		}
		if !c.shape {
			continue
		}
		if c.sub != "" {
			qbBody, pmBody = field(qbBody, c.sub), field(pmBody, c.sub)
		}
		qbShape, pmShape := shape(qbBody), shape(pmBody)
		for _, k := range c.required {
			if _, ok := pmShape[k]; !ok {
				failures = append(failures, fmt.Sprintf("%s: piecemeal lacks %s", c.name, k))
			}
		}
		var missing, extra []string
		for k, typ := range qbShape {
			pt, ok := pmShape[k]
			switch {
			case !ok:
				missing = append(missing, k)
			case pt != typ && pt != "null" && typ != "null":
				failures = append(failures, fmt.Sprintf("%s: %s is %s in qBittorrent, %s in piecemeal", c.name, k, typ, pt))
			}
		}
		for k := range pmShape {
			if _, ok := qbShape[k]; !ok {
				extra = append(extra, k)
			}
		}
		t.Logf("%s: fields only qBittorrent has: %v", c.name, missing)
		t.Logf("%s: fields only piecemeal has: %v", c.name, extra)
	}
	for _, f := range failures {
		t.Error(f)
	}
}

func doForm(t *testing.T, c *client, method, path, form string, out *any) (int, string) {
	t.Helper()
	var body any
	if method == http.MethodPost {
		body = form
	}
	status, raw, err := c.raw(method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if json.Unmarshal(raw, out) != nil {
		*out = nil
	}
	return status, string(raw)
}

// sameJSON compares two bodies as JSON values, or as text when they
// aren't JSON.
func sameJSON(a, b string) bool {
	var va, vb any
	if json.Unmarshal([]byte(a), &va) != nil || json.Unmarshal([]byte(b), &vb) != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	ja, _ := json.Marshal(va)
	jb, _ := json.Marshal(vb)
	return string(ja) == string(jb)
}

// field is a field of a JSON object, or nil.
func field(v any, name string) any {
	if m, ok := v.(map[string]any); ok {
		return m[name]
	}
	return nil
}

// shape flattens a JSON object, or the first element of an array of
// objects, or the first value of a map of objects, to key: type.
func shape(v any) map[string]string {
	out := map[string]string{}
	switch x := v.(type) {
	case []any:
		if len(x) > 0 {
			return shape(x[0])
		}
	case map[string]any:
		for k, val := range x {
			if inner, ok := val.(map[string]any); ok && len(x) == 1 {
				return shape(inner)
			}
			out[k] = jsonType(val)
		}
	}
	return out
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	default:
		return "object"
	}
}

var tempPassword = regexp.MustCompile(`temporary password is provided for this session: (\S+)`)

// qbittorrentPassword reads the temporary WebUI password qBittorrent logs
// on first start.
func qbittorrentPassword(t *testing.T) string {
	var pw string
	waitFor(t, "qBittorrent's temporary password", 2*time.Minute, func() bool {
		out, err := exec.Command("docker", "compose", "logs", "--no-color", "qbittorrent").CombinedOutput()
		if err != nil {
			return false
		}
		if m := tempPassword.FindSubmatch(out); m != nil {
			pw = string(m[1])
			return true
		}
		return false
	})
	return pw
}

// TestVueTorrent checks that piecemeal serves VueTorrent at /qbittorrent/
// and the qBittorrent API under it, which VueTorrent calls relative to its
// own location.
func TestVueTorrent(t *testing.T) {
	piecemeal(t)
	resp, err := http.Get(piecemealURL + "/qbittorrent/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(string(body)), "vuetorrent") {
		t.Fatalf("GET /qbittorrent/: %d %.200q", resp.StatusCode, body)
	}
	vt := newClient(t, piecemealURL+"/qbittorrent/api/v2", nil)
	if s, _, _ := vt.raw(http.MethodPost, "/auth/login", "username=admin&password="+url.QueryEscape(password())); s != http.StatusNoContent {
		t.Fatalf("login: %d", s)
	}
	var version string
	if s, raw, _ := vt.raw(http.MethodGet, "/app/version", nil); s != http.StatusOK {
		t.Fatalf("version: %d", s)
	} else {
		version = string(raw)
	}
	var main map[string]any
	if s := vt.do(http.MethodGet, "/sync/maindata?rid=0", nil, &main); s != http.StatusOK || main["full_update"] != true {
		t.Fatalf("maindata: %d %v", s, main)
	}
	var prefs map[string]any
	if s := vt.do(http.MethodGet, "/app/preferences", nil, &prefs); s != http.StatusOK || prefs["alternative_webui_enabled"] != true {
		t.Fatalf("preferences: %d alternative_webui_enabled=%v", s, prefs["alternative_webui_enabled"])
	}
	t.Logf("VueTorrent sees qBittorrent %s", version)
}
