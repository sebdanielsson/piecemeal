package engine

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestDistributedCopies(t *testing.T) {
	for _, c := range []struct {
		counts []int
		want   float64
	}{
		{nil, 0},
		{[]int{0, 0, 0, 0}, 0},
		{[]int{1, 1, 1, 1}, 1},
		{[]int{1, 2, 2, 1}, 1.5},
		{[]int{0, 1, 1, 1}, 0.75},
	} {
		if got := DistributedCopies(c.counts); got != c.want {
			t.Errorf("DistributedCopies(%v) = %v, want %v", c.counts, got, c.want)
		}
	}
	if got := AvailableFraction([]int{0, 2, 1, 0}, 1, 4); got != 2.0/3 {
		t.Errorf("AvailableFraction = %v", got)
	}
	if got := AvailableFraction([]int{1}, 3, 5); got != 0 {
		t.Errorf("AvailableFraction out of range = %v", got)
	}
}

func TestPieceAvailability(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 4 << 20})
	h := newHarness(t)
	// Slow, so the seeder is still connected when counted.
	if _, err := h.settings.Set(ctx, map[string]any{"downloadLimit": int64(64 << 10)}); err != nil {
		t.Fatal(err)
	}
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "the seeder", func(st Torrent) bool { return st.Seeds > 0 })
	counts, err := h.engine.PieceAvailability(hash)
	if err != nil || len(counts) == 0 {
		t.Fatalf("counts %v, err %v", counts, err)
	}
	for i, c := range counts {
		if c != 1 {
			t.Fatalf("piece %d: %d peers have it, want 1 (the seeder)", i, c)
		}
	}
	if DistributedCopies(counts) != 1 {
		t.Fatalf("distributed copies %v", DistributedCopies(counts))
	}
	if _, err := h.engine.PieceAvailability("0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("an unknown torrent had availability")
	}
}

func TestPeerLog(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := newPeerLog()
	l.now = func() time.Time { return now }
	e := &Engine{}
	e.filter.log = l
	l.add(net.ParseIP("192.0.2.1"), "banned")
	l.add(net.ParseIP("192.0.2.1"), "banned") // quiet period
	l.add(net.ParseIP("198.51.100.7"), "")
	l.add(net.ParseIP("203.0.113.9"), "Bad Peer")
	all := e.PeerLog(-1)
	if len(all) != 3 || all[0].Blocked || all[0].Reason != "banned" || !all[1].Blocked ||
		all[1].Reason != "IP filter" || all[2].Reason != "IP filter: Bad Peer" || all[2].ID != 2 {
		t.Fatalf("log: %+v", all)
	}
	if after := e.PeerLog(0); len(after) != 2 || after[0].ID != 1 {
		t.Fatalf("after 0: %+v", after)
	}
	now = now.Add(2 * peerLogQuiet)
	l.add(net.ParseIP("192.0.2.1"), "banned")
	if got := e.PeerLog(2); len(got) != 1 || got[0].IP != "192.0.2.1" {
		t.Fatalf("logged again after the quiet period: %+v", got)
	}
	for i := range peerLogSize + 10 {
		l.add(net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)), "")
	}
	if n := len(e.PeerLog(-1)); n != peerLogSize {
		t.Fatalf("kept %d entries", n)
	}
}

func TestSetCommentPersists(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 1 << 10})
	h := newHarness(t)
	stopped := true
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo, Stopped: &stopped})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.SetComment(ctx, "replaced", hash); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	if st, _ := h.engine.Get(hash); st.Comment != "replaced" {
		t.Fatalf("comment after restart: %q", st.Comment)
	}
}
