package fetch

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/netbind"
	"github.com/sebdanielsson/piecemeal/internal/proxy"
	"github.com/sebdanielsson/piecemeal/internal/proxy/proxytest"
)

// TestBind binds the fetcher to a missing interface: public addresses
// can't be reached, directly or through a proxy, and private addresses on
// the allowed networks are reached directly.
func TestBind(t *testing.T) {
	b, err := netbind.New("pm-missing0")
	if err != nil {
		t.Skip(err)
	}
	srv := torrentServer(t)

	// Private and allowed: not bound.
	f := New(loopback, b)
	if _, err := f.Get(context.Background(), srv.URL); err != nil {
		t.Fatalf("allowed private address: %v", err)
	}

	// Loopback standing in for a public address: bound, so it fails.
	f = New(loopback, b)
	f.public = func(netip.Addr) bool { return true }
	if _, err := f.GetFeed(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "pm-missing0") {
		t.Fatalf("public address around the interface: %v", err)
	}

	// Through a proxy, the connection to the proxy is bound too.
	p := proxytest.Start(t, proxy.SOCKS5)
	f.SetProxy(Proxy{Server: &proxy.Server{Type: proxy.SOCKS5, Addr: p.Addr}, TorrentFiles: true})
	if _, err := f.Get(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "pm-missing0") {
		t.Fatalf("proxy around the interface: %v", err)
	}
	if got := p.Requests(); len(got) != 0 {
		t.Fatalf("requests through the proxy: %v", got)
	}
}
