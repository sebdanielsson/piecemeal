package engine

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"

	"github.com/sebdanielsson/piecemeal/internal/netbind"
	"github.com/sebdanielsson/piecemeal/internal/proxy/proxytest"
)

// netInterfaces returns the loopback interface's name and an up interface
// with addresses that isn't loopback, if there is one.
func netInterfaces(t *testing.T) (loopback, other string) {
	t.Helper()
	ifs, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifi := range ifs {
		addrs, _ := ifi.Addrs()
		switch {
		case ifi.Flags&net.FlagLoopback != 0 && loopback == "":
			loopback = ifi.Name
		case ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagLoopback == 0 && len(addrs) > 0 && other == "":
			other = ifi.Name
		}
	}
	if loopback == "" {
		t.Skip("no loopback interface")
	}
	return loopback, other
}

func withBind(name string) func(*harness) {
	return func(h *harness) {
		b, err := netbind.New(name)
		if err != nil {
			h.t.Skip(err)
		}
		h.bind = b
	}
}

// TestBindDownloads downloads from a seeder on loopback with every socket
// bound to the loopback interface: peers over TCP and uTP, and the
// announce to an HTTP tracker.
func TestBindDownloads(t *testing.T) {
	loopback, _ := netInterfaces(t)
	for _, utp := range []bool{false, true} {
		s := newSeeder(t, map[string]int{"x.bin": 128 << 10})
		var announces atomic.Int32
		tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			announces.Add(1)
			_ = bencode.NewEncoder(w).Encode(map[string]any{"interval": 1800, "peers": compactPeer(t, s.Addr)})
		}))
		t.Cleanup(tracker.Close)
		h := newHarness(t, withTrackers, withBind(loopback), func(h *harness) {
			if _, err := h.settings.Set(context.Background(), map[string]any{"utp": utp}); err != nil {
				t.Fatal(err)
			}
			if utp {
				// Without TCP, the download has to use the uTP dialers.
				h.clientConfig = func(tc *torrent.ClientConfig) {
					offline(tc)
					tc.DisableTrackers = false
					tc.DisableTCP = true
				}
			}
		})
		hash := addViaTracker(t, h, s, tracker.URL+"/announce")
		h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
		if announces.Load() == 0 {
			t.Fatal("no announce")
		}
		if tr := h.engine.Transfer(); tr.Interface.Name != loopback || !tr.Interface.Up ||
			tr.Connection == "disconnected" {
			t.Fatalf("transfer status: %+v %+v", tr, tr.Interface)
		}
	}
}

// TestBindFailsClosed binds to an interface that isn't loopback: the
// seeder and the tracker on loopback can't be reached, though they could
// be without binding.
func TestBindFailsClosed(t *testing.T) {
	_, other := netInterfaces(t)
	if other == "" {
		t.Skip("no interface besides loopback")
	}
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	var announces atomic.Int32
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		announces.Add(1)
		_ = bencode.NewEncoder(w).Encode(map[string]any{"interval": 1800, "peers": compactPeer(t, s.Addr)})
	}))
	t.Cleanup(tracker.Close)
	h := newHarness(t, withTrackers, withBind(other))
	hash := addViaTracker(t, h, s, tracker.URL+"/announce")
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if st, _ := h.engine.Get(hash); st.HasMetadata || st.Peers+st.Seeds > 0 {
		t.Fatalf("connected around the interface: %+v", st)
	}
	if n := announces.Load(); n > 0 {
		t.Fatalf("%d announces around the interface", n)
	}
}

// TestBindInterfaceGoesAway follows the interface going down and coming
// back as a new interface, which restarts the client on it.
func TestBindInterfaceGoesAway(t *testing.T) {
	loopback, _ := netInterfaces(t)
	lo, err := net.InterfaceByName(loopback)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	state := netbind.State{Up: true, Index: lo.Index}
	h := newHarness(t, withBind(loopback), func(h *harness) {
		h.interfaceState = func() netbind.State {
			mu.Lock()
			defer mu.Unlock()
			return state
		}
	})
	set := func(s netbind.State) {
		mu.Lock()
		state = s
		mu.Unlock()
		h.engine.checkInterface()
	}
	client := func() any {
		h.engine.mu.Lock()
		defer h.engine.mu.Unlock()
		return h.engine.client
	}
	before := client()

	set(netbind.State{})
	if name, up := h.engine.BoundInterface(); name != loopback || up {
		t.Fatalf("BoundInterface = %q, %v", name, up)
	}
	h.engine.mu.Lock()
	h.engine.refreshTransferLocked()
	h.engine.mu.Unlock()
	if tr := h.engine.Transfer(); tr.Connection != "disconnected" || tr.Interface.Up {
		t.Fatalf("while down: %+v", tr)
	}

	// Back as the same interface: the sockets are still bound to it.
	set(netbind.State{Up: true, Index: lo.Index})
	if client() != before {
		t.Fatal("restarted for the same interface")
	}
	// Back as a new one: the client restarts to bind to it.
	set(netbind.State{Up: true, Index: lo.Index + 1000})
	if client() == before {
		t.Fatal("not restarted for a new interface")
	}
	if _, up := h.engine.BoundInterface(); !up {
		t.Fatal("still down")
	}
}

// TestBindKeepsIPv6Out checks that with IPv6 off, IPv6 peers aren't
// dialled, bound or through a proxy: the dialers are per address family.
func TestBindKeepsIPv6Out(t *testing.T) {
	loopback, _ := netInterfaces(t)
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback:", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			_ = c.Close()
		}
	}()
	p := proxytest.Start(t, "socks5")
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 16 << 10})
	for name, opts := range map[string][]func(*harness){
		"bound":               {withBind(loopback)},
		"through proxy":       {withProxy(p, nil)},
		"bound through proxy": {withBind(loopback), withProxy(p, nil)},
	} {
		// offline turns IPv6 off.
		h := newHarness(t, opts...)
		hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.engine.AddPeers(ctx, []string{ln.Addr().String()}, hash); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		if n := accepted.Load(); n > 0 {
			t.Fatalf("%s: %d IPv6 connections with IPv6 off", name, n)
		}
		for _, r := range p.Requests() {
			if strings.Contains(r, "::1") {
				t.Fatalf("%s: IPv6 peer through the proxy: %v", name, p.Requests())
			}
		}
	}
}

// TestTransferStatusComparable checks that two statuses without changes
// compare equal, so tick publishes only changes.
func TestTransferStatusComparable(t *testing.T) {
	loopback, _ := netInterfaces(t)
	h := newHarness(t, withBind(loopback))
	h.engine.mu.Lock()
	a, b := h.engine.transferLocked(0, 0, 0), h.engine.transferLocked(0, 0, 0)
	h.engine.mu.Unlock()
	a.FreeSpace, b.FreeSpace = 0, 0
	if a != b {
		t.Fatalf("unchanged statuses differ:\n%+v\n%+v", a, b)
	}
}
