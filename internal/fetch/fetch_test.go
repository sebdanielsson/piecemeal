package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestAllowed(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.100.100.100": false, "::1": false, "fe80::1": false, "fd00::1": false, "0.0.0.0": false,
		"::ffff:127.0.0.1": false,
		// Special-purpose IPv4 ranges.
		"0.1.2.3": false, "192.0.0.8": false, "198.18.0.1": false, "240.0.0.1": false, "255.255.255.255": false,
		"fec0::1": false, "64:ff9b:1::a00:5": false,
		// IPv6 that embeds IPv4: NAT64, IPv4-compatible, 6to4 and Teredo.
		"64:ff9b::a00:5": false, "64:ff9b::808:808": true,
		"::10.0.0.5": false, "::8.8.8.8": true,
		"2002:a00:5::1": false, "2002:808:808::1": true,
		"2001:0:4136:e378:8000:63bf:f5ff:fffa": false, // client 10.0.0.5
		"2001:0:4136:e378:8000:63bf:f7f7:f7f7": true,  // client 8.8.8.8
	} {
		if got := Allowed(netip.MustParseAddr(addr), nil); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", addr, got, want)
		}
	}
	for _, addr := range []string{"10.1.2.3", "64:ff9b::a01:203"} {
		if !Allowed(netip.MustParseAddr(addr), []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}) {
			t.Errorf("%s in an allowed network refused", addr)
		}
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

func TestErrorsHideSecrets(t *testing.T) {
	f := New(nil)
	for _, u := range []string{
		"http://127.0.0.1:1/download?apikey=SECRET&link=x",
		"http://user:SECRET@127.0.0.1:1/x",
		"ftp://host/?passkey=SECRET",
	} {
		_, err := f.Get(context.Background(), u)
		if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "127.0.0.1:1:") {
			t.Errorf("%s: %v", u, err)
		}
	}
	if got := Redact(&url.URL{Scheme: "https", Host: "t.example", Path: "/rss", RawQuery: "passkey=x"}); got != "https://t.example/rss?…" {
		t.Errorf("Redact: %s", got)
	}
}
