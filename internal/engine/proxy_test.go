package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"

	"github.com/sebdanielsson/piecemeal/internal/proxy/proxytest"
	"github.com/sebdanielsson/piecemeal/internal/settings"
)

// withProxy points the harness's proxy settings at p, with more settings
// in extra.
func withProxy(p *proxytest.Server, extra map[string]any) func(*harness) {
	return func(h *harness) {
		host, port, _ := net.SplitHostPort(p.Addr)
		n, _ := strconv.Atoi(port)
		values := map[string]any{"proxyType": p.Type, "proxyHost": host, "proxyPort": n}
		for k, v := range extra {
			values[k] = v
		}
		if _, err := h.settings.Set(context.Background(), values); err != nil {
			h.t.Fatal(err)
		}
	}
}

// withTrackers lets the engine announce to trackers on loopback.
func withTrackers(h *harness) {
	h.clientConfig = func(tc *torrent.ClientConfig) {
		offline(tc)
		tc.DisableTrackers = false
	}
}

func requested(p *proxytest.Server, want string) bool {
	return slices.Contains(p.Requests(), want)
}

func TestProxyPeerConnections(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 256 << 10})
	p := proxytest.Start(t, "socks5", proxytest.WithAuth("user", "pass"))
	h := newHarness(t, withProxy(p, map[string]any{"proxyUsername": "user", "proxyPassword": "pass"}))

	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	h.checkContent(s, "payload")
	if !requested(p, "tcp "+s.Addr) {
		t.Fatalf("the peer connection didn't go through the proxy: %v", p.Requests())
	}
}

// TestProxyFailsClosed checks that peers aren't dialled directly when the
// proxy is down.
func TestProxyFailsClosed(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	p := proxytest.Start(t, "socks5")
	p.Close()
	h := newHarness(t, withProxy(p, nil))

	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if st, _ := h.engine.Get(hash); st.Peers+st.Seeds > 0 || st.Downloaded > 0 {
		t.Fatalf("connected without the proxy: %+v", st)
	}
	for _, tt := range s.Client.Torrents() {
		if n := len(tt.PeerConns()); n > 0 {
			t.Fatalf("the seeder has %d connections", n)
		}
	}

	// Turning the proxy off connects directly again.
	if _, err := h.settings.Set(ctx, map[string]any{"proxyType": "none"}); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
}

// TestProxyHTTPTracker announces to an HTTP tracker through an HTTP proxy,
// with peers connected directly.
func TestProxyHTTPTracker(t *testing.T) {
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = bencode.NewEncoder(w).Encode(map[string]any{"interval": 1800, "peers": compactPeer(t, s.Addr)})
	}))
	t.Cleanup(tracker.Close)
	p := proxytest.Start(t, "http")
	h := newHarness(t, withTrackers, withProxy(p, map[string]any{"proxyPeerConnections": false}))

	hash := addViaTracker(t, h, s, tracker.URL+"/announce")
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	u, _ := url.Parse(tracker.URL)
	if !requested(p, "tcp "+u.Host) {
		t.Fatalf("the announce didn't go through the proxy: %v", p.Requests())
	}
	if requested(p, "tcp "+s.Addr) {
		t.Fatalf("the peer connection went through the proxy: %v", p.Requests())
	}
}

// TestProxyUDPTracker announces to a UDP tracker through a SOCKS5 proxy's
// UDP relay, and connects to the peer it names through the proxy.
func TestProxyUDPTracker(t *testing.T) {
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	tracker := udpTracker(t, s.Addr)
	p := proxytest.Start(t, "socks5")
	h := newHarness(t, withTrackers, withProxy(p, nil))

	hash := addViaTracker(t, h, s, "udp://"+tracker+"/announce")
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	if !requested(p, "udp "+tracker) || !requested(p, "tcp "+s.Addr) {
		t.Fatalf("requests: %v", p.Requests())
	}
}

// TestProxyClientConfig checks what can't go through the proxy is off
// while peers go through it, and only then.
func TestProxyClientConfig(t *testing.T) {
	e := &Engine{log: slog.New(slog.DiscardHandler)}
	s := settings.Defaults(t.TempDir())
	s.ProxyType, s.ProxyHost, s.ProxyPort = "socks5", "127.0.0.1", 1080

	tc := e.clientConfig(0, s)
	if !tc.NoDHT || !tc.DisableUTP || !tc.DisableWebtorrent || tc.DialForPeerConns ||
		tc.TrackerDialContext == nil || tc.HTTPDialContext == nil || tc.TrackerListenPacket == nil || tc.HTTPProxy != nil {
		t.Fatalf("peers through the proxy: %+v", tc)
	}

	s.ProxyPeerConnections = false
	tc = e.clientConfig(0, s)
	if tc.NoDHT || tc.DisableUTP || !tc.DialForPeerConns || tc.TrackerDialContext == nil {
		t.Fatalf("trackers only: %+v", tc)
	}

	s.ProxyBitTorrent = false
	tc = e.clientConfig(0, s)
	if tc.TrackerDialContext != nil || tc.TrackerListenPacket != nil || !tc.DialForPeerConns {
		t.Fatalf("BitTorrent not through the proxy: %+v", tc)
	}
}

// addViaTracker adds s's torrent as a magnet whose only source of peers is
// the tracker.
func addViaTracker(t *testing.T, h *harness, s *seeder, trackerURL string) string {
	t.Helper()
	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&tr=%s", s.Hash, url.QueryEscape(trackerURL))
	hash, err := h.engine.Add(context.Background(), AddParams{Magnet: magnet})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func compactPeer(t *testing.T, addr string) string {
	t.Helper()
	ap := netip.MustParseAddrPort(addr)
	b := ap.Addr().As4()
	return string(binary.BigEndian.AppendUint16(b[:], ap.Port()))
}

// udpTracker runs a UDP tracker (BEP 15) on loopback that answers every
// announce with peer, and returns its address.
func udpTracker(t *testing.T, peer string) string {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	peerBytes := []byte(compactPeer(t, peer))
	connID := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			b := buf[:n]
			if len(b) < 16 {
				continue
			}
			action, tid := binary.BigEndian.Uint32(b[8:]), b[12:16]
			var resp []byte
			switch action {
			case 0: // connect
				resp = binary.BigEndian.AppendUint32(nil, 0)
				resp = append(append(resp, tid...), connID...)
			case 1: // announce
				resp = binary.BigEndian.AppendUint32(nil, 1)
				resp = append(resp, tid...)
				resp = binary.BigEndian.AppendUint32(resp, 1800) // interval
				resp = binary.BigEndian.AppendUint32(resp, 0)    // leechers
				resp = binary.BigEndian.AppendUint32(resp, 1)    // seeders
				resp = append(resp, peerBytes...)
			default:
				continue
			}
			_, _ = pc.WriteToUDPAddrPort(resp, from)
		}
	}()
	return pc.LocalAddr().String()
}
