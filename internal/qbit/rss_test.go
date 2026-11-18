package qbit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
)

// feedServer serves an RSS feed whose items link to two torrents.
func feedServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	torrents := map[string][]byte{
		"e01": enginetest.NewSeeder(t, "Show e01", map[string]int{"v.mkv": 1 << 10}).Metainfo,
		"e02": enginetest.NewSeeder(t, "Show e02", map[string]int{"v.mkv": 1 << 10}).Metainfo,
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := strings.CutPrefix(r.URL.Path, "/t/"); ok {
			_, _ = w.Write(torrents[name])
			return
		}
		item := func(title, guid, t, date string) string {
			return fmt.Sprintf(`<item><title>%s</title><guid>%s</guid><pubDate>%s</pubDate><category>TV</category>
<link>%s/page/%s</link><enclosure url="%s/t/%s" type="application/x-bittorrent"/></item>`, title, guid, date, srv.URL, guid, srv.URL, t)
		}
		fmt.Fprintf(w, `<rss><channel><title>Show feed</title><lastBuildDate>Tue, 17 Nov 2026 12:00:00 +0000</lastBuildDate>%s%s</channel></rss>`,
			item("Show.S01E01.1080p", "g1", "e01", "Mon, 16 Nov 2026 10:00:00 +0200"),
			item("Show.S01E02.1080p", "g2", "e02", "Tue, 17 Nov 2026 10:00:00 +0000"))
	}))
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/feed.xml"
}

func (e *env) items(withData bool) map[string]any {
	e.t.Helper()
	var out map[string]any
	e.get(fmt.Sprintf("rss/items?withData=%v", withData)).json(e.t, &out)
	return out
}

func TestRSSFeeds(t *testing.T) {
	e := newEnv(t)
	e.login()
	_, feedURL := feedServer(t)

	expect(t, "addFeed without path", e.post("rss/addFeed", url.Values{"url": {feedURL}}), http.StatusBadRequest)
	expect(t, "addFolder", e.post("rss/addFolder", url.Values{"path": {"TV"}}), http.StatusOK)
	expect(t, "addFolder again", e.post("rss/addFolder", url.Values{"path": {"TV"}}), http.StatusConflict)
	expect(t, "addFeed", e.post("rss/addFeed", url.Values{"url": {feedURL}, "path": {`TV\Show`}}), http.StatusOK)
	expect(t, "addFeed same URL", e.post("rss/addFeed", url.Values{"url": {feedURL}, "path": {"Other"}}), http.StatusConflict)

	tree := e.items(false)
	show := tree["TV"].(map[string]any)["Show"].(map[string]any)
	if show["url"] != feedURL || !strings.HasPrefix(show["uid"].(string), "{") || len(show) != 2 {
		t.Fatalf("items: %v", tree)
	}
	// The feed is fetched once added.
	var articles []any
	deadline := time.Now().Add(10 * time.Second)
	for len(articles) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no articles: %v", e.items(true))
		}
		time.Sleep(50 * time.Millisecond)
		show = e.items(true)["TV"].(map[string]any)["Show"].(map[string]any)
		articles, _ = show["articles"].([]any)
	}
	if show["title"] != "Show feed" || show["lastBuildDate"] != "Tue, 17 Nov 2026 12:00:00 +0000" || show["hasError"] != false {
		t.Fatalf("feed with data: %v", show)
	}
	newest := articles[0].(map[string]any)
	oldest := articles[1].(map[string]any)
	if newest["id"] != "g2" || oldest["date"] != "16 Nov 2026 08:00:00 +0000" || oldest["category"] != "TV" ||
		!strings.HasSuffix(oldest["torrentURL"].(string), "/t/e01") || oldest["isRead"] != nil {
		t.Fatalf("articles: %v", articles)
	}

	expect(t, "markAsRead", e.post("rss/markAsRead", url.Values{"itemPath": {`TV\Show`}, "articleId": {"g1"}}), http.StatusOK)
	expect(t, "markAsRead unknown", e.post("rss/markAsRead", url.Values{"itemPath": {"Nope"}}), http.StatusNoContent)
	show = e.items(true)["TV"].(map[string]any)["Show"].(map[string]any)
	if read := show["articles"].([]any)[1].(map[string]any)["isRead"]; read != true {
		t.Fatalf("isRead after markAsRead: %v", read)
	}

	expect(t, "refreshItem", e.post("rss/refreshItem", url.Values{"itemPath": {""}}), http.StatusOK)
	expect(t, "bad interval", e.post("rss/setFeedRefreshInterval", url.Values{"path": {`TV\Show`}, "refreshInterval": {"-1"}}), http.StatusBadRequest)
	expect(t, "interval", e.post("rss/setFeedRefreshInterval", url.Values{"path": {`TV\Show`}, "refreshInterval": {"3600"}}), http.StatusNoContent)
	expect(t, "setFeedURL unknown", e.post("rss/setFeedURL", url.Values{"path": {"Nope"}, "url": {"x"}}), http.StatusConflict)
	expect(t, "moveItem", e.post("rss/moveItem", url.Values{"itemPath": {`TV\Show`}, "destPath": {"Renamed"}}), http.StatusOK)
	if r, ok := e.items(false)["Renamed"].(map[string]any); !ok || r["refreshInterval"] != 3600.0 {
		t.Fatalf("after move: %v", e.items(false))
	}
	r := e.post("rss/removeItem", url.Values{"path": {""}})
	expect(t, "removeItem root", r, http.StatusConflict)
	if !strings.Contains(string(r.body), "Cannot delete root folder.") {
		t.Fatalf("root removal: %s", r.body)
	}
	expect(t, "GET on a write", e.get("rss/addFolder?path=x"), http.StatusMethodNotAllowed)

	// Rules, the way VueTorrent saves them, with torrentParams and a name.
	def := `{"name":"Show","enabled":true,"mustContain":"Show 1080p","smartFilter":true,"affectedFeeds":["` + feedURL +
		`"],"torrentParams":{"category":"rss","tags":["b","a"],"stopped":true,"download_limit":-1}}`
	expect(t, "setRule", e.post("rss/setRule", url.Values{"ruleName": {"Show"}, "ruleDef": {def}}), http.StatusOK)
	var rules map[string]map[string]any
	rules = nil
	e.get("rss/rules").json(t, &rules)
	rule := rules["Show"]
	params := rule["torrentParams"].(map[string]any)
	if len(rule) != 16 || rule["addPaused"] != true || rule["assignedCategory"] != "rss" || rule["lastMatch"] != "" ||
		params["operating_mode"] != "AutoManaged" || fmt.Sprint(params["tags"]) != "[a b]" || params["share_limits_mode"] != "Default" {
		t.Fatalf("rule: %v", rule)
	}
	var matching map[string][]string
	e.get("rss/matchingArticles?ruleName=Show").json(t, &matching)
	if len(matching["Renamed"]) != 2 {
		t.Fatalf("matching: %v", matching)
	}

	// Legacy fields, with affectedFeeds as a string, as qbittorrent-api
	// sends them; and a definition that isn't JSON gets the defaults.
	legacy := `{"mustContain":"none","addPaused":false,"assignedCategory":"old","savePath":"/x","torrentContentLayout":null,"lastMatch":null,"affectedFeeds":"` + feedURL + `"}`
	expect(t, "legacy", e.post("rss/setRule", url.Values{"ruleName": {"Legacy"}, "ruleDef": {legacy}}), http.StatusOK)
	expect(t, "bad def", e.post("rss/setRule", url.Values{"ruleName": {"Bad"}, "ruleDef": {"nope"}}), http.StatusOK)
	rules = nil
	e.get("rss/rules").json(t, &rules)
	lp := rules["Legacy"]["torrentParams"].(map[string]any)
	if lp["category"] != "old" || lp["stopped"] != false || lp["use_auto_tmm"] != false || lp["content_layout"] != nil ||
		len(rules["Legacy"]["affectedFeeds"].([]any)) != 1 || rules["Legacy"]["lastMatch"] != "" {
		t.Fatalf("legacy rule: %v", rules["Legacy"])
	}
	if rules["Bad"]["enabled"] != true || rules["Bad"]["mustContain"] != "" {
		t.Fatalf("default rule: %v", rules["Bad"])
	}
	expect(t, "renameRule", e.post("rss/renameRule", url.Values{"ruleName": {"Bad"}, "newRuleName": {"Good"}}), http.StatusOK)
	expect(t, "cloneRule", e.post("rss/cloneRule", url.Values{"sourceName": {"Good"}, "cloneName": {"Copy"}}), http.StatusOK)
	expect(t, "removeRule", e.post("rss/removeRule", url.Values{"ruleName": {"Good"}}), http.StatusOK)

	// Export and import.
	exp := e.get("rss/exportRules")
	if !bytes.Contains(exp.body, []byte("\n    \"")) || !strings.Contains(exp.header.Get("Content-Disposition"), "rss-downloader-rules.json") {
		t.Fatalf("export: %s %v", exp.body[:min(len(exp.body), 80)], exp.header)
	}
	expect(t, "removeRule Legacy", e.post("rss/removeRule", url.Values{"ruleName": {"Legacy"}}), http.StatusOK)
	expect(t, "import", e.importRules(exp.body), http.StatusNoContent)
	rules = nil
	e.get("rss/rules").json(t, &rules)
	if _, ok := rules["Legacy"]; !ok || len(rules) != 3 {
		t.Fatalf("after import: %v", rules)
	}
	expect(t, "import invalid", e.importRules([]byte("{nope")), http.StatusBadRequest)
	expect(t, "import array", e.importRules([]byte("[]")), http.StatusBadRequest)
	expect(t, "import without a file", e.post("rss/importRules", url.Values{"rules": {"{}"}}), http.StatusBadRequest)

	// Turning auto-downloading on takes the unread episode (S01E01 was
	// read above).
	b, _ := json.Marshal(map[string]any{"rss_auto_downloading_enabled": true})
	expect(t, "enable", e.post("app/setPreferences", url.Values{"json": {string(b)}}), http.StatusOK)
	deadline = time.Now().Add(10 * time.Second)
	for {
		var list []Torrent
		e.get("torrents/info").json(t, &list)
		if len(list) == 1 && list[0].Category == "rss" && list[0].Tags == "a, b" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("torrents: %+v", list)
		}
		time.Sleep(50 * time.Millisecond)
	}
	rules = nil
	e.get("rss/rules").json(t, &rules)
	if rules["Show"]["lastMatch"] != "17 Nov 2026 10:00:00 +0000" || fmt.Sprint(rules["Show"]["previouslyMatchedEpisodes"]) != "[1x2]" {
		t.Fatalf("rule after the download: %v", rules["Show"])
	}
}

func (e *env) importRules(body []byte) response {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, _ := mw.CreateFormFile("rules", "rss-downloader-rules.json")
	_, _ = w.Write(body)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v2/rss/importRules", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return e.do(req)
}
