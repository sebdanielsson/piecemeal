package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/proxy"
	"github.com/sebdanielsson/piecemeal/internal/proxy/proxytest"
)

var loopback = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}

func torrentServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("d4:infod4:name1:xee"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetThroughProxy(t *testing.T) {
	srv := torrentServer(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, typ := range []string{proxy.SOCKS5, proxy.HTTP} {
		t.Run(typ, func(t *testing.T) {
			p := proxytest.Start(t, typ)
			f := New(loopback, nil)
			// Loopback stands in for a public address.
			f.public = func(netip.Addr) bool { return true }
			f.SetProxy(Proxy{Server: &proxy.Server{Type: typ, Addr: p.Addr}, TorrentFiles: true})

			if _, err := f.Get(context.Background(), srv.URL); err != nil {
				t.Fatal(err)
			}
			if got := p.Requests(); len(got) != 1 || got[0] != "tcp "+host {
				t.Fatalf("requests: %v", got)
			}
			// Feeds aren't set to go through the proxy.
			if _, err := f.GetFeed(context.Background(), srv.URL); err != nil {
				t.Fatal(err)
			}
			if got := p.Requests(); len(got) != 1 {
				t.Fatalf("the feed went through the proxy: %v", got)
			}

			// The hostname is looked up here, so the address is checked,
			// and the proxy gets the address.
			_, port, _ := strings.Cut(host, ":")
			if _, err := f.Get(context.Background(), "http://localhost:"+port+"/x.torrent"); err != nil {
				t.Fatal(err)
			}
			for _, r := range p.Requests() {
				if strings.Contains(r, "localhost") {
					t.Fatalf("the proxy got the hostname: %v", p.Requests())
				}
			}

			// Turning it off goes direct.
			f.SetProxy(Proxy{})
			n := len(p.Requests())
			if _, err := f.Get(context.Background(), srv.URL); err != nil {
				t.Fatal(err)
			}
			if len(p.Requests()) != n {
				t.Fatalf("went through the proxy after it was turned off: %v", p.Requests())
			}
		})
	}
}

// TestProxyKeepsAddressCheck checks that private addresses are refused
// through the proxy as they are without it, and those allowed are reached
// directly.
func TestProxyKeepsAddressCheck(t *testing.T) {
	srv := torrentServer(t)
	p := proxytest.Start(t, proxy.SOCKS5)
	px := Proxy{Server: &proxy.Server{Type: proxy.SOCKS5, Addr: p.Addr}, TorrentFiles: true, Feeds: true}

	f := New(nil, nil)
	f.SetProxy(px)
	if _, err := f.Get(context.Background(), srv.URL); !errors.Is(err, ErrForbidden) {
		t.Fatalf("loopback through the proxy: %v, want ErrForbidden", err)
	}

	f = New(loopback, nil)
	f.SetProxy(px)
	if _, err := f.GetFeed(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := p.Requests(); len(got) != 0 {
		t.Fatalf("requests: %v", got)
	}
}

func TestProxyDown(t *testing.T) {
	p := proxytest.Start(t, proxy.SOCKS5)
	p.Close()
	f := New(append(loopback, netip.MustParsePrefix("::1/128")), nil)
	f.public = func(netip.Addr) bool { return true }
	// localhost has two addresses; the second isn't tried once the proxy
	// turns out to be unreachable.
	_, err := f.dialProxied(context.Background(), &proxy.Server{Type: proxy.SOCKS5, Addr: p.Addr}, "tcp", "localhost:80")
	if !errors.Is(err, proxy.ErrUnreachable) || strings.Count(err.Error(), "can't reach") != 1 {
		t.Fatalf("got %v", err)
	}
}

// TestSetProxyNewConnections checks that a connection opened before the
// proxy changed, kept alive or still in use, isn't reused afterwards.
func TestSetProxyNewConnections(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-release
		}
		_, _ = w.Write([]byte("d4:infod4:name1:xee"))
	}))
	t.Cleanup(srv.Close)
	p := proxytest.Start(t, proxy.SOCKS5)
	f := New(loopback, nil)
	f.public = func(netip.Addr) bool { return true }

	// A request in flight on a direct connection while the proxy is set.
	done := make(chan error, 1)
	go func() {
		_, err := f.Get(context.Background(), srv.URL+"/slow")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	f.SetProxy(Proxy{Server: &proxy.Server{Type: proxy.SOCKS5, Addr: p.Addr}, TorrentFiles: true})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := f.Get(context.Background(), srv.URL); err != nil {
			t.Fatal(err)
		}
	}
	if got := p.Requests(); len(got) != 1 {
		t.Fatalf("requests through the proxy: %v", got)
	}
}
