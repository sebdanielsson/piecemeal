package api

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
)

func TestTorrentEditing(t *testing.T) {
	s := newTestServer(t)
	s.login()
	seed := enginetest.NewSeeder(t, "Show", map[string]int{"e01.mkv": 64 << 10, "e02.mkv": 64 << 10})
	stopped := true
	var added engine.Torrent
	s.do(http.MethodPost, "/torrents", AddTorrentRequest{TorrentFile: seed.Metainfo, Stopped: &stopped, ContentLayout: "NoSubfolder"},
		http.StatusCreated, &added, csrf...)
	if added.RootPath != "" {
		t.Fatalf("NoSubfolder kept a root folder: %+v", added)
	}
	base := "/torrents/" + seed.Hash

	var trackers trackersOutput
	s.do(http.MethodPut, base+"/trackers", map[string]any{"tiers": [][]string{{"udp://a.example:1", "udp://b.example:1"}, {}, {"https://c.example/announce"}}},
		http.StatusOK, &trackers.Body, csrf...)
	if len(trackers.Body.Items) != 3 || trackers.Body.Items[2].Tier != 1 {
		t.Fatalf("trackers: %+v", trackers.Body.Items)
	}
	s.do(http.MethodPut, base+"/trackers", map[string]any{"tiers": [][]string{{"ftp://x"}}}, http.StatusUnprocessableEntity, nil, csrf...)

	var seeds webSeedsBody
	s.do(http.MethodPut, base+"/web-seeds", webSeedsBody{URLs: []string{"https://ws.example/"}}, http.StatusOK, &seeds, csrf...)
	s.do(http.MethodGet, base+"/web-seeds", nil, http.StatusOK, &seeds)
	if len(seeds.URLs) != 1 {
		t.Fatalf("web seeds: %v", seeds)
	}

	s.do(http.MethodPost, base+"/files/rename", map[string]string{"oldPath": "e01.mkv", "newPath": "S01/E01.mkv"}, http.StatusNoContent, nil, csrf...)
	var files filesOutput
	s.do(http.MethodGet, base+"/files", nil, http.StatusOK, &files.Body)
	if files.Body.Items[0].Path != "S01/E01.mkv" {
		t.Fatalf("files: %+v", files.Body.Items)
	}
	s.do(http.MethodPost, base+"/files/rename", map[string]string{"oldPath": "../x", "newPath": "y"}, http.StatusUnprocessableEntity, nil, csrf...)

	req, _ := http.NewRequest(http.MethodGet, s.URL+Prefix+base+"/torrent-file", nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	mi, err := metainfo.Load(bytes.NewReader(b))
	if err != nil || resp.Header.Get("Content-Disposition") != "attachment; filename=Show.torrent" || len(mi.UrlList) != 1 {
		t.Fatalf("torrent file: %v %q %+v", err, resp.Header.Get("Content-Disposition"), mi)
	}

	// Queue actions need queueing.
	s.do(http.MethodPost, "/torrents/actions", BulkAction{Hashes: []string{seed.Hash}, Action: "queueTop"}, http.StatusUnprocessableEntity, nil, csrf...)

	dest := filepath.Join(s.save, "moved")
	var updated engine.Torrent
	s.do(http.MethodPatch, base, UpdateTorrentRequest{SavePath: &dest}, http.StatusOK, &updated, csrf...)
	outside := "/etc"
	s.do(http.MethodPatch, base, UpdateTorrentRequest{SavePath: &outside}, http.StatusUnprocessableEntity, nil, csrf...)
	deadline := time.Now().Add(10 * time.Second)
	for updated.SavePath != dest || updated.State == engine.StateMoving {
		if time.Now().After(deadline) {
			t.Fatalf("not moved: %+v", updated)
		}
		time.Sleep(20 * time.Millisecond)
		s.do(http.MethodGet, base, nil, http.StatusOK, &updated)
	}
	s.do(http.MethodPost, "/torrents/actions", BulkAction{Hashes: []string{"all"}, Action: "move", SavePath: s.save}, http.StatusNoContent, nil, csrf...)
}
