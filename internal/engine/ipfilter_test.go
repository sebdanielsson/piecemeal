package engine

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/iplist"
)

func TestParseIPFilter(t *testing.T) {
	in := `# comment
001.002.003.000 - 001.002.003.255 , 000 , eMule range
010.000.000.000 - 010.255.255.255 , 200 , allowed, skipped
Some org:192.168.1.0-192.168.1.255
203.0.113.0/24
198.51.100.7
2001:db8::/32
not an entry
`
	ranges, bad, err := ParseIPFilter(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 || len(ranges) != 5 {
		t.Fatalf("ranges %d (%v), bad %d", len(ranges), ranges, bad)
	}
	list := iplist.New(ranges)
	for ip, blocked := range map[string]bool{
		"1.2.3.4": true, "1.2.4.1": false, "10.1.1.1": false, "192.168.1.200": true,
		"203.0.113.255": true, "198.51.100.7": true, "198.51.100.8": false, "2001:db8::1": true, "2001:db9::1": false,
	} {
		if _, got := list.Lookup(net.ParseIP(ip)); got != blocked {
			t.Errorf("%s: blocked %v, want %v", ip, got, blocked)
		}
	}
}

func TestBlocklistMergesOverlaps(t *testing.T) {
	ranges, bad, err := ParseIPFilter(strings.NewReader("T-Mobile USA, Inc.:1.2.3.0-1.2.3.255\n5.0.0.0-5.0.0.9 , 000 , a\n5.0.0.10-5.0.0.20 , 000 , b\n"))
	if err != nil || bad != 0 || len(ranges) != 3 {
		t.Fatalf("ranges %v, bad %d, err %v", ranges, bad, err)
	}
	banned, _, _ := ParseIPFilter(strings.NewReader("1.2.3.4\n2001:db8::1\n"))
	list := iplist.New(mergeRanges(append(ranges, banned...)))
	for ip, blocked := range map[string]bool{
		"1.2.3.4": true, "1.2.3.5": true, "1.2.3.255": true, "1.2.4.0": false,
		"5.0.0.15": true, "5.0.0.21": false, "2001:db8::1": true, "2001:db8::2": false,
	} {
		if _, ok := list.Lookup(net.ParseIP(ip)); ok != blocked {
			t.Errorf("%s: blocked %v, want %v", ip, ok, blocked)
		}
	}
	if n := list.NumRanges(); n != 3 {
		t.Errorf("%d ranges after merging, want 3", n)
	}
}

// TestBanDropsPeersWithoutRestart checks that banning an address closes
// connections from it and keeps the client running.
func TestBanDropsPeersWithoutRestart(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 8 << 20})
	h := newHarness(t)
	// Slow, so the download is still running when the ban comes.
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
	h.waitFor(hash, "the seeder", func(st Torrent) bool { return st.Seeds+st.Peers > 0 })
	h.engine.mu.Lock()
	client := h.engine.client
	h.engine.mu.Unlock()

	host, _, _ := net.SplitHostPort(s.Addr)
	if _, err := h.settings.Set(ctx, map[string]any{"bannedIPs": host}); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "the banned seeder to go", func(st Torrent) bool { return st.Seeds+st.Peers == 0 })
	h.engine.mu.Lock()
	same := h.engine.client == client
	h.engine.mu.Unlock()
	if !same {
		t.Fatal("banning restarted the client")
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if st, _ := h.engine.Get(hash); st.Seeds+st.Peers != 0 {
		t.Fatalf("connected to a banned peer: %+v", st)
	}
}
