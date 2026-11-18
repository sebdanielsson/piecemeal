package qbit

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
)

func TestTransferEndpoints(t *testing.T) {
	e := newEnv(t)
	e.login()
	if r := e.get("transfer/speedLimitsMode"); string(r.body) != "0" {
		t.Fatalf("mode %q", r.body)
	}
	expect(t, "setDownloadLimit", e.post("transfer/setDownloadLimit", url.Values{"limit": {"5000"}}), http.StatusOK)
	if r := e.get("transfer/downloadLimit"); string(r.body) != "5000" {
		t.Fatalf("download limit %q", r.body)
	}
	expect(t, "toggle", e.post("transfer/toggleSpeedLimitsMode", nil), http.StatusOK)
	if r := e.get("transfer/speedLimitsMode"); string(r.body) != "1" {
		t.Fatalf("mode after toggle %q", r.body)
	}
	// In alternative mode the limit endpoints work on the alternative limit.
	expect(t, "setUploadLimit", e.post("transfer/setUploadLimit", url.Values{"limit": {"100"}}), http.StatusOK)
	if r := e.get("transfer/uploadLimit"); string(r.body) != "100" {
		t.Fatalf("alt upload limit %q", r.body)
	}
	expect(t, "banPeers", e.post("transfer/banPeers", url.Values{"peers": {"203.0.113.5:6881|bad|[2001:db8::1]:51413"}}), http.StatusOK)
	var p map[string]any
	e.get("app/preferences").json(t, &p)
	if p["banned_IPs"] != "203.0.113.5\n2001:db8::1" {
		t.Fatalf("banned IPs %q", p["banned_IPs"])
	}
	var log []map[string]any
	e.get("log/main?last_known_id=-1").json(t, &log)
	if r := e.get("log/peers"); r.status != http.StatusOK {
		t.Fatalf("log/peers %d", r.status)
	}
}

func TestTorrentEditing(t *testing.T) {
	e := newEnv(t)
	e.login()
	seed := enginetest.NewSeeder(t, "Show", map[string]int{"e01.mkv": 64 << 10, "e02.mkv": 64 << 10})
	expect(t, "add", e.postMultipart("torrents/add", nil, seed.Metainfo), http.StatusOK)
	h := url.Values{"hash": {seed.Hash}}
	with := func(kv ...string) url.Values {
		v := url.Values{"hash": {seed.Hash}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}

	expect(t, "addTrackers", e.post("torrents/addTrackers", with("urls", "https://a.example/announce\nudp://b.example:1")), http.StatusOK)
	expect(t, "editTracker", e.post("torrents/editTracker", with("origUrl", "udp://b.example:1", "newUrl", "udp://c.example:1")), http.StatusOK)
	expect(t, "editTracker missing", e.post("torrents/editTracker", with("origUrl", "udp://x:1", "newUrl", "udp://y:1")), http.StatusConflict)
	expect(t, "removeTrackers", e.post("torrents/removeTrackers", with("urls", "https://a.example/announce")), http.StatusOK)
	var trs []map[string]any
	e.get("torrents/trackers?hash="+seed.Hash).json(t, &trs)
	if len(trs) != 4 || trs[3]["url"] != "udp://c.example:1" {
		t.Fatalf("trackers: %v", trs)
	}

	expect(t, "addWebSeeds", e.post("torrents/addWebSeeds", with("urls", "https://s.example/a|https://s.example/b")), http.StatusOK)
	expect(t, "removeWebSeeds", e.post("torrents/removeWebSeeds", with("urls", "https://s.example/a")), http.StatusOK)
	var seeds []map[string]string
	e.get("torrents/webseeds?hash="+seed.Hash).json(t, &seeds)
	if len(seeds) != 1 || seeds[0]["url"] != "https://s.example/b" {
		t.Fatalf("web seeds: %v", seeds)
	}

	expect(t, "renameFile", e.post("torrents/renameFile", with("oldPath", "Show/e01.mkv", "newPath", "Show/S01E01.mkv")), http.StatusOK)
	expect(t, "renameFolder", e.post("torrents/renameFolder", with("oldPath", "Show", "newPath", "Show (2026)")), http.StatusOK)
	var files []map[string]any
	e.get("torrents/files?hash="+seed.Hash).json(t, &files)
	if files[0]["name"] != "Show (2026)/S01E01.mkv" {
		t.Fatalf("files after renames: %v", files)
	}

	expect(t, "setTags", e.post("torrents/setTags", url.Values{"hashes": {seed.Hash}, "tags": {"a,b"}}), http.StatusOK)
	expect(t, "setTags again", e.post("torrents/setTags", url.Values{"hashes": {seed.Hash}, "tags": {"b"}}), http.StatusOK)
	var list []Torrent
	e.get("torrents/info").json(t, &list)
	if list[0].Tags != "b" || list[0].Priority != -1 {
		t.Fatalf("after setTags: %+v", list[0])
	}
	if r := e.get("torrents/count"); string(r.body) != "1" {
		t.Fatalf("count %q", r.body)
	}
	r := e.get("torrents/export?hash=" + seed.Hash)
	mi, err := metainfo.Load(bytes.NewReader(r.body))
	if err != nil || len(mi.UrlList) != 1 {
		t.Fatalf("export: %v %+v", err, mi)
	}
	var hashes []string
	e.get("torrents/pieceHashes?"+h.Encode()).json(t, &hashes)
	if len(hashes) != 4 || len(hashes[0]) != 40 {
		t.Fatalf("piece hashes: %v", hashes)
	}

	// Queue moves need queueing.
	expect(t, "topPrio without queueing", e.post("torrents/topPrio", url.Values{"hashes": {seed.Hash}}), http.StatusConflict)

	// Move the (still empty) torrent elsewhere under the save path.
	dest := filepath.Join(e.save, "moved")
	expect(t, "setLocation", e.post("torrents/setLocation", url.Values{"hashes": {seed.Hash}, "location": {dest}}), http.StatusOK)
	expect(t, "setLocation outside", e.post("torrents/setLocation", url.Values{"hashes": {seed.Hash}, "location": {"/etc"}}), http.StatusForbidden)
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.get("torrents/info").json(t, &list)
		if list[0].SavePath == dest && list[0].State != "moving" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not moved: %+v", list[0])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTorrentCreator(t *testing.T) {
	e := newEnv(t)
	e.login()
	src := filepath.Join(e.save, "new")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.bin"), bytes.Repeat([]byte{7}, 100<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	r := e.post("torrentcreator/addTask", url.Values{
		"sourcePath": {src}, "private": {"true"}, "trackers": {"https://t.example/a\n\nhttps://t.example/b"},
	})
	expect(t, "addTask", r, http.StatusOK)
	var task map[string]string
	r.json(t, &task)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var st []map[string]any
		e.get("torrentcreator/status?taskID="+task["taskID"]).json(t, &st)
		if st[0]["status"] == "Finished" {
			break
		}
		if st[0]["status"] == "Failed" || time.Now().After(deadline) {
			t.Fatalf("status: %v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	r = e.post("torrentcreator/torrentFile", url.Values{"taskID": {task["taskID"]}})
	mi, err := metainfo.Load(bytes.NewReader(r.body))
	if err != nil || len(mi.AnnounceList) != 2 {
		t.Fatalf("torrent file: %v %+v", err, mi)
	}
	expect(t, "deleteTask", e.post("torrentcreator/deleteTask", url.Values{"taskID": {task["taskID"]}}), http.StatusOK)
	expect(t, "status after delete", e.get("torrentcreator/status?taskID="+task["taskID"]), http.StatusNotFound)
}

func TestDirectoryContent(t *testing.T) {
	e := newEnv(t)
	e.login()
	if err := os.MkdirAll(filepath.Join(e.save, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	var dirs []string
	e.post("app/getDirectoryContent", url.Values{"dirPath": {e.save}, "mode": {"dirs"}}).json(t, &dirs)
	if len(dirs) != 1 || !strings.HasSuffix(dirs[0], "/sub") {
		t.Fatalf("dirs: %v", dirs)
	}
	expect(t, "outside the roots", e.post("app/getDirectoryContent", url.Values{"dirPath": {"/etc"}}), http.StatusNotFound)
	_ = context.Background()
}
