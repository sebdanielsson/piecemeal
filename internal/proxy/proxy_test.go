package proxy_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"

	"github.com/sebdanielsson/piecemeal/internal/proxy"
	"github.com/sebdanielsson/piecemeal/internal/proxy/proxytest"
)

// echoServer echoes TCP connections on loopback.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

// udpEchoServer echoes UDP datagrams on loopback.
func udpEchoServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteToUDPAddrPort(buf[:n], from)
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr)
}

func checkEcho(t *testing.T, c net.Conn) {
	t.Helper()
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	msg := []byte("hello through the proxy")
	if _, err := c.Write(msg); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("echo: %q", got)
	}
}

func TestConnect(t *testing.T) {
	for _, typ := range []string{proxy.SOCKS5, proxy.HTTP} {
		t.Run(typ, func(t *testing.T) {
			echo := echoServer(t)
			for _, auth := range []bool{false, true} {
				var opts []proxytest.Option
				srv := proxy.Server{Type: typ}
				if auth {
					opts = append(opts, proxytest.WithAuth("user", "pass:word"))
					srv.Username, srv.Password = "user", "pass:word"
				}
				p := proxytest.Start(t, typ, opts...)
				srv.Addr = p.Addr
				c, err := srv.DialContext(context.Background(), "tcp", echo)
				if err != nil {
					t.Fatalf("auth %v: %v", auth, err)
				}
				checkEcho(t, c)
				if got := p.Requests(); !slices.Equal(got, []string{"tcp " + echo}) {
					t.Fatalf("requests: %v", got)
				}
			}
		})
	}
}

func TestCredentialErrors(t *testing.T) {
	for _, typ := range []string{proxy.SOCKS5, proxy.HTTP} {
		t.Run(typ, func(t *testing.T) {
			echo := echoServer(t)
			p := proxytest.Start(t, typ, proxytest.WithAuth("user", "pass"))
			for _, tc := range []struct {
				user, pass, want string
			}{
				{"", "", "it wants a username and password"},
				{"user", "wrong", "it refused the username or password"},
			} {
				srv := proxy.Server{Type: typ, Addr: p.Addr, Username: tc.user, Password: tc.pass}
				_, err := srv.DialContext(context.Background(), "tcp", echo)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("user %q: got %v, want %q", tc.user, err, tc.want)
				}
				if strings.Contains(err.Error(), "wrong") {
					t.Fatalf("the error shows the password: %v", err)
				}
			}
			if got := p.Requests(); len(got) != 0 {
				t.Fatalf("requests without credentials: %v", got)
			}
		})
	}
}

func TestHostnameLookup(t *testing.T) {
	echo := echoServer(t)
	_, port, _ := net.SplitHostPort(echo)
	p := proxytest.Start(t, proxy.SOCKS5)
	srv := proxy.Server{Type: proxy.SOCKS5, Addr: p.Addr}

	// The proxy gets the hostname to look up.
	c, err := srv.DialContext(context.Background(), "tcp", "localhost:"+port)
	if err != nil {
		t.Fatal(err)
	}
	checkEcho(t, c)
	if got := p.Requests(); !slices.Equal(got, []string{"tcp localhost:" + port}) {
		t.Fatalf("requests: %v", got)
	}

	// Looked up locally, the proxy only sees addresses.
	c, err = srv.DialResolved(context.Background(), "tcp", "localhost:"+port)
	if err != nil {
		t.Fatal(err)
	}
	checkEcho(t, c)
	for _, r := range p.Requests()[1:] {
		if strings.Contains(r, "localhost") {
			t.Fatalf("the proxy saw the hostname: %v", p.Requests())
		}
	}
}

func TestConnectRefused(t *testing.T) {
	// A port nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	for _, typ := range []string{proxy.SOCKS5, proxy.HTTP} {
		p := proxytest.Start(t, typ)
		srv := proxy.Server{Type: typ, Addr: p.Addr}
		if _, err := srv.DialContext(context.Background(), "tcp", closed); err == nil {
			t.Fatalf("%s: dialled a closed port", typ)
		}
	}
}

func TestContextEndsHandshake(t *testing.T) {
	// A proxy that accepts and never answers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	srv := proxy.Server{Type: proxy.SOCKS5, Addr: ln.Addr().String()}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = srv.DialContext(ctx, "tcp", "127.0.0.1:1")
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
	if _, err := srv.ListenPacket(ctx); err == nil {
		t.Fatal("UDP relay from a silent proxy")
	}
}

func TestUDPRelay(t *testing.T) {
	echo := udpEchoServer(t)
	p := proxytest.Start(t, proxy.SOCKS5, proxytest.WithAuth("user", "pass"))
	srv := proxy.Server{Type: proxy.SOCKS5, Addr: p.Addr, Username: "user", Password: "pass"}
	pc, err := srv.ListenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	_ = pc.SetDeadline(time.Now().Add(5 * time.Second))
	for _, msg := range []string{"announce", "again"} {
		if _, err := pc.WriteTo([]byte(msg), echo); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 100)
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != msg || from.String() != echo.String() {
			t.Fatalf("got %q from %v", buf[:n], from)
		}
	}
	if got := p.Requests(); !slices.Equal(got, []string{"udp " + echo.String(), "udp " + echo.String()}) {
		t.Fatalf("requests: %v", got)
	}

	// When the proxy ends the relay, reads fail, so the caller opens a
	// new one.
	p.Close()
	buf := make([]byte, 100)
	if _, _, err := pc.ReadFrom(buf); err == nil {
		t.Fatal("read after the relay ended")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read waited for the deadline: %v", err)
	}
}

func TestNoUDP(t *testing.T) {
	p := proxytest.Start(t, proxy.SOCKS5, proxytest.WithoutUDP())
	srv := proxy.Server{Type: proxy.SOCKS5, Addr: p.Addr}
	if _, err := srv.ListenPacket(context.Background()); err == nil || !strings.Contains(err.Error(), "doesn't relay UDP") {
		t.Fatalf("got %v", err)
	}
	srv = proxy.Server{Type: proxy.HTTP, Addr: p.Addr}
	if _, err := srv.ListenPacket(context.Background()); !errors.Is(err, proxy.ErrNoUDP) {
		t.Fatalf("got %v", err)
	}
}

// TestServerWithOtherClients checks the test proxies against clients that
// aren't piecemeal's, so a mistake shared by both sides can't hide.
func TestServerWithOtherClients(t *testing.T) {
	echo := echoServer(t)
	p := proxytest.Start(t, proxy.SOCKS5, proxytest.WithAuth("user", "pass"))
	d, err := xproxy.SOCKS5("tcp", p.Addr, &xproxy.Auth{User: "user", Password: "pass"}, xproxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	c, err := d.Dial("tcp", echo)
	if err != nil {
		t.Fatal(err)
	}
	checkEcho(t, c)

	// net/http tunnels HTTPS through an HTTP proxy with CONNECT.
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(ts.Close)
	hp := proxytest.Start(t, proxy.HTTP, proxytest.WithAuth("user", "pass"))
	tr := ts.Client().Transport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: hp.Addr, User: url.UserPassword("user", "pass")})
	tr.TLSClientConfig = &tls.Config{RootCAs: ts.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
	resp, err := (&http.Client{Transport: tr}).Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("body %q", b)
	}
	if got := hp.Requests(); len(got) != 1 || got[0] != "tcp "+strings.TrimPrefix(ts.URL, "https://") {
		t.Fatalf("requests: %v", got)
	}
}
