package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTrackersAndWebSeeds(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hash, err := h.engine.Add(ctx, AddParams{Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&tr=udp://a.example:1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddTrackers(ctx, hash, []string{"https://b.example/announce", "udp://a.example:1"}); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.EditTracker(ctx, hash, "udp://a.example:1", "udp://c.example:2"); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.EditTracker(ctx, hash, "udp://nope:1", "udp://d.example:2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("editing a missing tracker: %v", err)
	}
	if err := h.engine.AddTrackers(ctx, hash, []string{"ftp://x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad scheme: %v", err)
	}
	trs, _ := h.engine.Trackers(hash)
	if len(trs) != 2 || trs[0].URL != "udp://c.example:2" || trs[1].URL != "https://b.example/announce" || trs[1].Tier != 1 {
		t.Fatalf("trackers: %+v", trs)
	}
	if err := h.engine.RemoveTrackers(ctx, hash, []string{"udp://c.example:2"}); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.SetWebSeeds(ctx, hash, []string{"https://seed.example/files/"}); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.SetWebSeeds(ctx, hash, []string{"file:///etc"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad web seed: %v", err)
	}

	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	trs, _ = h.engine.Trackers(hash)
	seeds, _ := h.engine.WebSeeds(hash)
	st, _ := h.engine.Get(hash)
	if len(trs) != 1 || trs[0].URL != "https://b.example/announce" || len(seeds) != 1 || st.Tracker != "https://b.example/announce" || st.Trackers != 1 {
		t.Fatalf("after restart: trackers %+v, web seeds %v, status tracker %q (%d)", trs, seeds, st.Tracker, st.Trackers)
	}
}

func TestMove(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 100 << 10, "sub/b.bin": 50 << 10})
	h := newHarness(t)
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })

	if err := h.engine.Move(ctx, hash, "/etc"); !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("moving outside the roots: %v", err)
	}
	dest := filepath.Join(h.saveDir, "elsewhere")
	if err := h.engine.Move(ctx, hash, dest); err != nil {
		t.Fatal(err)
	}
	st := h.waitFor(hash, "move", func(st Torrent) bool {
		return st.SavePath == dest && (st.State == StateSeeding || st.State == StateStalledSeeding)
	})
	if st.ContentPath != filepath.Join(dest, "payload") || !st.Complete {
		t.Fatalf("after move: %+v", st)
	}
	h.checkContentIn(s, dest)
	if _, err := os.Stat(filepath.Join(h.saveDir, "payload")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old content left behind: %v", err)
	}
}

// TestMoveFilesRollsBack checks that a move that fails partway puts the
// files it already moved back.
func TestMoveFilesRollsBack(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	for _, f := range []string{"x/a", "y/b"} {
		p := filepath.Join(src, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A file where the second one's folder should be makes its move fail.
	if err := os.WriteFile(filepath.Join(dest, "y"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFiles(src, dest, []string{"x/a", "y/b"}); err == nil {
		t.Fatal("no error")
	}
	for _, f := range []string{"x/a", "y/b"} {
		if _, err := os.Stat(filepath.Join(src, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s not back in place: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "x")); !os.IsNotExist(err) {
		t.Errorf("emptied folder left in the destination: %v", err)
	}
}
