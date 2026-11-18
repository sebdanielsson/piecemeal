package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestAllowed(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.100.100.100": false, "::1": false, "fe80::1": false, "fd00::1": false, "0.0.0.0": false,
		"::ffff:127.0.0.1": false,
	} {
		if got := Allowed(netip.MustParseAddr(addr), nil); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", addr, got, want)
		}
	}
	if !Allowed(netip.MustParseAddr("10.1.2.3"), []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}) {
		t.Error("allowed network refused")
	}
}

func TestGetBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("d4:infod4:name1:xee"))
	}))
	defer srv.Close()

	if _, err := New(nil).Get(context.Background(), srv.URL); !errors.Is(err, ErrForbidden) {
		t.Fatalf("loopback fetch: %v, want ErrForbidden", err)
	}
	res, err := New([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}).Get(context.Background(), srv.URL)
	if err != nil || len(res.Body) == 0 {
		t.Fatalf("allowed fetch: %+v, %v", res, err)
	}
}

func TestGetFollowsMagnetRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "magnet:?xt=urn:btih:abc", http.StatusFound)
	}))
	defer srv.Close()
	res, err := New([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}).Get(context.Background(), srv.URL)
	if err != nil || res.Magnet != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("magnet redirect: %+v, %v", res, err)
	}
}

func TestGetRefusesOtherURLs(t *testing.T) {
	f := New(nil)
	for _, u := range []string{"file:///etc/passwd", "http:///path", "ftp://example.com/x", "gopher://x", "http://", "not a url"} {
		if _, err := f.Get(context.Background(), u); err == nil || errors.Is(err, ErrForbidden) {
			t.Errorf("%q: %v, want a bad URL error", u, err)
		}
	}
	if res, err := f.Get(context.Background(), "MAGNET:?xt=urn:btih:abc"); err != nil || res.Magnet == "" {
		t.Errorf("magnet: %+v %v", res, err)
	}
}
