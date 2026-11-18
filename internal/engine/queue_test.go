package engine

import (
	"context"
	"fmt"
	"testing"
)

func TestQueueing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if _, err := h.settings.Set(ctx, map[string]any{"queueingEnabled": true, "maxActiveDownloads": 1}); err != nil {
		t.Fatal(err)
	}
	var hashes []string
	for i := range 3 {
		hash, err := h.engine.Add(ctx, AddParams{Magnet: fmt.Sprintf("magnet:?xt=urn:btih:%040x", i+1)})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	states := func() []State {
		var out []State
		for _, hash := range hashes {
			st, _ := h.engine.Get(hash)
			out = append(out, st.State)
		}
		return out
	}
	check := func(want ...State) {
		t.Helper()
		h.waitFor(hashes[0], fmt.Sprint("states ", want), func(Torrent) bool {
			got := states()
			for i := range want {
				if got[i] != want[i] {
					return false
				}
			}
			return true
		})
	}
	check(StateMetadata, StateQueued, StateQueued)
	if st, _ := h.engine.Get(hashes[2]); st.QueuePosition != 3 {
		t.Fatalf("third torrent's position %d", st.QueuePosition)
	}

	// Moving the last to the top runs it instead of the first.
	if err := h.engine.QueueMove(ctx, "top", hashes[2]); err != nil {
		t.Fatal(err)
	}
	check(StateQueued, StateQueued, StateMetadata)
	if st, _ := h.engine.Get(hashes[0]); st.QueuePosition != 2 {
		t.Fatalf("first torrent's position after moving the third to the top: %d", st.QueuePosition)
	}

	// Forced torrents run regardless; stopping one frees its slot.
	if err := h.engine.SetForced(ctx, true, hashes[1]); err != nil {
		t.Fatal(err)
	}
	check(StateQueued, StateMetadata, StateMetadata)
	if err := h.engine.Stop(ctx, hashes[2]); err != nil {
		t.Fatal(err)
	}
	check(StateMetadata, StateMetadata, StateStopped)

	// Turning queueing off runs everything that isn't stopped.
	if err := h.engine.Start(ctx, hashes[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := h.settings.Set(ctx, map[string]any{"queueingEnabled": false}); err != nil {
		t.Fatal(err)
	}
	check(StateMetadata, StateMetadata, StateMetadata)
}
