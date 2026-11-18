package engine

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

func TestFetchMetadataThenAdd(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 64 << 10, "b.bin": 1 << 10})
	h := newHarness(t)

	hash, mi, err := h.engine.FetchMetadata(s.Magnet())
	if err != nil || hash != s.Hash || mi != nil {
		t.Fatalf("first call: %s %d bytes %v", hash, len(mi), err)
	}
	if h.engine.FetchedMetadata(hash) != nil {
		t.Fatal("metadata before it arrived")
	}
	deadline := time.Now().Add(20 * time.Second)
	for mi == nil {
		if time.Now().After(deadline) {
			t.Fatal("no metadata")
		}
		time.Sleep(50 * time.Millisecond)
		_, mi, err = h.engine.FetchMetadata(s.Magnet())
		if err != nil {
			t.Fatal(err)
		}
	}
	parsed, err := metainfo.Load(bytes.NewReader(mi))
	if err != nil || parsed.HashInfoBytes().HexString() != s.Hash {
		t.Fatalf("fetched torrent file: %v", err)
	}
	if !bytes.Equal(h.engine.FetchedMetadata(hash), mi) {
		t.Fatal("FetchedMetadata differs")
	}
	// Fetching wasn't adding: piecemeal doesn't list it, and wrote nothing.
	if _, ok := h.engine.Get(hash); ok {
		t.Fatal("the fetch added the torrent")
	}

	// Adding the magnet now uses the fetched metadata and drops the fetch.
	added, err := h.engine.Add(ctx, AddParams{Magnet: s.Magnet()})
	if err != nil || added != hash {
		t.Fatalf("add: %s %v", added, err)
	}
	if st, _ := h.engine.Get(hash); !st.HasMetadata {
		t.Fatalf("added without the fetched metadata: %+v", st)
	}
	h.engine.mu.Lock()
	left := len(h.engine.metaFetches)
	h.engine.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d fetches left after adding", left)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	h.checkContent(s, "payload")

	// A torrent piecemeal has answers with its own file.
	if _, mi, err := h.engine.FetchMetadata(s.Magnet()); err != nil || mi == nil {
		t.Fatalf("fetch of an added torrent: %d bytes %v", len(mi), err)
	}
	if _, _, err := h.engine.FetchMetadata("magnet:?nothing"); err == nil {
		t.Fatal("a bad magnet was accepted")
	}
}
