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
		t.Fatalf("add movie (needs Radarr's metadata service): %d", s)
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
	var single map[string]any
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
		t.Fatalf("release push returned no decision: %v", single)
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
		if qbStatus != pmStatus {
			failures = append(failures, fmt.Sprintf("%s: status qBittorrent %d, piecemeal %d", c.name, qbStatus, pmStatus))
		}
		if c.name == "add magnet stopped" {
			// qBittorrent adds magnets in the background; give it a moment.
			time.Sleep(2 * time.Second)
		}
		if !c.shape {
			continue
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
