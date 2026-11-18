package engine

import (
	"context"
	"testing"
	"time"
)

// TestDialsPeersAddedWhileDownloadLimited checks that peers added while the
// download limit is used up are dialled once it has room again (issue
// #37). anacrolix doesn't open connections while its download rate limiter
// is out of tokens, and nothing makes it try again later.
func TestDialsPeersAddedWhileDownloadLimited(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	h := newHarness(t)
	const limit = 256 << 10
	if _, err := h.settings.Set(ctx, map[string]any{"downloadLimit": limit}); err != nil {
		t.Fatal(err)
	}
	// Use up the limiter's burst and a second's worth more, as another
	// torrent downloading at the limit would. A reservation can't exceed
	// the burst.
	now := time.Now()
	for _, n := range []int{h.engine.down.Burst(), limit} {
		if !h.engine.down.ReserveN(now, n).OK() {
			t.Fatal("reserving download tokens")
		}
	}

	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	h.checkContent(s, "payload")
}
