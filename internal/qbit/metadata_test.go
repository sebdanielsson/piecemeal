package qbit

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
)

type metadataObj struct {
	Hash string `json:"hash"`
	Info struct {
		Name  string `json:"name"`
		Files []struct {
			Length int64  `json:"length"`
			Path   string `json:"path"`
		} `json:"files"`
		Length    int64 `json:"length"`
		PiecesNum int   `json:"pieces_num"`
	} `json:"info"`
	Trackers []map[string]any `json:"trackers"`
	Webseeds []string         `json:"webseeds"`
}

// pollMetadata posts fetchMetadata until it answers 200.
func (e *env) pollMetadata(source string) metadataObj {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		r := e.post("torrents/fetchMetadata", url.Values{"source": {source}})
		switch r.status {
		case http.StatusOK:
			var m metadataObj
			r.json(e.t, &m)
			return m
		case http.StatusAccepted:
		default:
			e.t.Fatalf("fetchMetadata %s: %d %s", source, r.status, r.body)
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("fetchMetadata %s: still pending", source)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMetadataEndpoints(t *testing.T) {
	e := newEnv(t)
	e.login()
	seed := enginetest.NewSeeder(t, "Show", map[string]int{"e01.mkv": 64 << 10, "e02.mkv": 1 << 10})
	var hits atomic.Int32
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(seed.Metainfo)
	}))
	t.Cleanup(files.Close)
	src := files.URL + "/show.torrent"

	expect(t, "GET", e.get("torrents/fetchMetadata?source="+url.QueryEscape(src)), http.StatusMethodNotAllowed)
	expect(t, "no source", e.post("torrents/fetchMetadata", nil), http.StatusBadRequest)
	expect(t, "nonsense", e.post("torrents/fetchMetadata", url.Values{"source": {"nonsense"}}), http.StatusBadRequest)
	expect(t, "save no source", e.get("torrents/saveMetadata"), http.StatusBadRequest)
	expect(t, "save unknown URL", e.get("torrents/saveMetadata?source="+url.QueryEscape(src)), http.StatusNotFound)
	expect(t, "save unknown hash", e.get("torrents/saveMetadata?source=3333333333333333333333333333333333333333"), http.StatusConflict)

	// A URL: 202 with an empty object, then the metadata.
	r := e.post("torrents/fetchMetadata", url.Values{"source": {src}})
	if r.status != http.StatusAccepted && r.status != http.StatusOK {
		t.Fatalf("first fetch: %d %s", r.status, r.body)
	}
	m := e.pollMetadata(src)
	if m.Hash != seed.Hash || m.Info.Name != "Show" || len(m.Info.Files) != 2 || m.Info.Length != 65<<10 ||
		m.Info.Files[0].Path[:5] != "Show/" || m.Trackers == nil || m.Webseeds == nil {
		t.Fatalf("metadata: %+v", m)
	}
	for _, s := range []string{src, seed.Hash} {
		r = e.get("torrents/saveMetadata?source=" + url.QueryEscape(s))
		if r.status != http.StatusOK || r.header.Get("Content-Type") != "application/x-bittorrent" ||
			!bytes.Equal(r.body, seed.Metainfo) ||
			r.header.Get("Content-Disposition") != `attachment; filename="`+seed.Hash+`.torrent"` {
			t.Fatalf("saveMetadata %s: %d %v", s, r.status, r.header)
		}
	}

	// parseMetadata takes uploads.
	var parsed []metadataObj
	e.postMultipart("torrents/parseMetadata", nil, seed.Metainfo).json(t, &parsed)
	if len(parsed) != 1 || parsed[0].Hash != seed.Hash {
		t.Fatalf("parseMetadata: %+v", parsed)
	}
	expect(t, "parse invalid", e.postMultipart("torrents/parseMetadata", nil, []byte("not a torrent")), http.StatusUnsupportedMediaType)

	// Adding the URL uses what was fetched instead of downloading it again.
	before := hits.Load()
	expect(t, "add", e.post("torrents/add", url.Values{"urls": {src}, "stopped": {"true"}}), http.StatusOK)
	if hits.Load() != before {
		t.Fatal("the torrent file was downloaded again")
	}
	expect(t, "saveMetadata after add", e.get("torrents/saveMetadata?source="+url.QueryEscape(src)), http.StatusNotFound)
	expect(t, "delete", e.post("torrents/delete", url.Values{"hashes": {seed.Hash}, "deleteFiles": {"false"}}), http.StatusOK)

	// A magnet: 202 with its hash, then the metadata from the swarm.
	r = e.post("torrents/fetchMetadata", url.Values{"source": {seed.Magnet()}})
	var pending map[string]any
	if r.status == http.StatusAccepted {
		r.json(t, &pending)
		if pending["hash"] != seed.Hash || pending["infohash_v1"] != seed.Hash || pending["infohash_v2"] != "" {
			t.Fatalf("202 body: %v", pending)
		}
	}
	if m = e.pollMetadata(seed.Magnet()); m.Hash != seed.Hash || len(m.Info.Files) != 2 {
		t.Fatalf("magnet metadata: %+v", m)
	}
	expect(t, "add the magnet", e.post("torrents/add", url.Values{"urls": {seed.Magnet()}, "stopped": {"true"}}), http.StatusOK)
	var props map[string]any
	e.get("torrents/properties?hash="+seed.Hash).json(t, &props)
	if props["has_metadata"] != true {
		t.Fatalf("added magnet without the fetched metadata: %v", props["has_metadata"])
	}
}
