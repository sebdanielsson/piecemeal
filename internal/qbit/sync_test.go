package qbit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

type maindata struct {
	Rid              int                       `json:"rid"`
	FullUpdate       *bool                     `json:"full_update"`
	Torrents         map[string]map[string]any `json:"torrents"`
	TorrentsRemoved  []string                  `json:"torrents_removed"`
	Categories       map[string]map[string]any `json:"categories"`
	Tags             []string                  `json:"tags"`
	TagsRemoved      []string                  `json:"tags_removed"`
	Trackers         map[string][]string       `json:"trackers"`
	TrackersRemoved  []string                  `json:"trackers_removed"`
	ServerState      map[string]any            `json:"server_state"`
	CategoriesRemove []string                  `json:"categories_removed"`
}

func (e *env) maindata(rid int) maindata {
	e.t.Helper()
	r := e.get(fmt.Sprintf("sync/maindata?rid=%d", rid))
	expect(e.t, "maindata", r, http.StatusOK)
	var m maindata
	if err := json.Unmarshal(r.body, &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

func TestMaindata(t *testing.T) {
	e := newEnv(t)
	e.login()
	const h1 = "1111111111111111111111111111111111111111"
	const h2 = "2222222222222222222222222222222222222222"
	add := func(hash, extra string) {
		t.Helper()
		expect(t, "add", e.post("torrents/add", url.Values{
			"urls": {"magnet:?xt=urn:btih:" + hash + "&dn=t" + hash[:1] + extra}, "stopped": {"true"},
		}), http.StatusOK)
	}
	add(h1, "&tr=udp://a.example:1")

	full := e.maindata(0)
	if full.FullUpdate == nil || !*full.FullUpdate || full.Rid != 1 || len(full.Torrents) != 1 {
		t.Fatalf("full update: %+v", full)
	}
	if _, ok := full.Torrents[h1]["hash"]; ok {
		t.Fatal("torrent objects carry hash")
	}
	if full.Torrents[h1]["name"] != "t1" || full.ServerState["use_alt_speed_limits"] != false || full.Tags == nil {
		t.Fatalf("full update torrent/server: %+v", full)
	}
	if hs := full.Trackers["udp://a.example:1"]; len(hs) != 1 || hs[0] != h1 {
		t.Fatalf("trackers: %v", full.Trackers)
	}

	// Nothing changed: an empty partial update.
	empty := e.maindata(full.Rid)
	if empty.FullUpdate != nil || len(empty.Torrents) != 0 || empty.Rid != full.Rid+1 {
		t.Fatalf("no-change update: %+v", empty)
	}

	// Changes: a new torrent whole, the changed field of the other, a tag,
	// the alternative speed mode.
	add(h2, "")
	expect(t, "addTags", e.post("torrents/addTags", url.Values{"hashes": {h1}, "tags": {"hd"}}), http.StatusOK)
	expect(t, "toggle", e.post("transfer/toggleSpeedLimitsMode", nil), http.StatusOK)
	part := e.maindata(empty.Rid)
	if part.FullUpdate != nil || len(part.Torrents[h2]) < 60 || part.Torrents[h1]["tags"] != "hd" || len(part.Torrents[h1]) > 3 {
		t.Fatalf("partial update torrents: %+v", part.Torrents)
	}
	if len(part.Tags) != 1 || part.ServerState["use_alt_speed_limits"] != true {
		t.Fatalf("partial update tags/server: %+v / %+v", part.Tags, part.ServerState)
	}

	// The client lost that response and asks again with the older rid: it
	// gets the same changes.
	again := e.maindata(empty.Rid)
	if again.Torrents[h1]["tags"] != "hd" || len(again.Torrents[h2]) < 60 {
		t.Fatalf("retry with the older rid: %+v", again.Torrents)
	}

	// Removing a torrent and its tracker.
	expect(t, "delete", e.post("torrents/delete", url.Values{"hashes": {h1}}), http.StatusOK)
	expect(t, "deleteTags", e.post("torrents/deleteTags", url.Values{"tags": {"hd"}}), http.StatusOK)
	gone := e.maindata(again.Rid)
	if len(gone.TorrentsRemoved) != 1 || gone.TorrentsRemoved[0] != h1 || len(gone.TrackersRemoved) != 1 || len(gone.TagsRemoved) != 1 {
		t.Fatalf("removals: %+v", gone)
	}
	if _, both := gone.Torrents[h1]; both {
		t.Fatal("a removed torrent is also in torrents")
	}

	// An unknown rid gets a full update.
	if m := e.maindata(424242); m.FullUpdate == nil || len(m.Torrents) != 1 {
		t.Fatalf("unknown rid: %+v", m)
	}

	// Another client has its own rids.
	other := newEnvClient(e)
	other.login()
	if m := other.maindata(gone.Rid); m.FullUpdate == nil {
		t.Fatalf("another session's rid: %+v", m)
	}
}

func TestTorrentPeersFullUpdateHasPeers(t *testing.T) {
	e := newEnv(t)
	e.login()
	const h = "3333333333333333333333333333333333333333"
	expect(t, "add", e.post("torrents/add", url.Values{"urls": {"magnet:?xt=urn:btih:" + h}, "stopped": {"true"}}), http.StatusOK)
	r := e.get("sync/torrentPeers?hash=" + h)
	expect(t, "torrentPeers", r, http.StatusOK)
	var m map[string]any
	r.json(t, &m)
	if _, ok := m["peers"]; !ok || m["full_update"] != true {
		t.Fatalf("torrentPeers: %v", m)
	}
	expect(t, "unknown torrent", e.get("sync/torrentPeers?hash=4444444444444444444444444444444444444444"), http.StatusNotFound)
	expect(t, "no hash", e.get("sync/torrentPeers"), http.StatusBadRequest)
}
