// Package proxytest runs SOCKS5 and HTTP CONNECT proxies for tests, and
// records every destination they are asked to reach.
package proxytest

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"testing"
)

// Server is a running test proxy.
type Server struct {
	// Addr is where it listens, host:port.
	Addr string
	// Type is "socks5" or "http".
	Type string

	username, password string
	noUDP              bool

	ln net.Listener
	mu sync.Mutex
	// requests are "tcp host:port" for each connection and "udp
	// host:port" for each datagram.
	requests []string
	conns    map[net.Conn]struct{}
	closed   bool
	wg       sync.WaitGroup
}

// Option configures a Server.
type Option func(*Server)

// WithAuth requires a username and password.
func WithAuth(username, password string) Option {
	return func(s *Server) { s.username, s.password = username, password }
}

// WithoutUDP makes a SOCKS5 proxy refuse UDP ASSOCIATE, as many do.
func WithoutUDP() Option {
	return func(s *Server) { s.noUDP = true }
}

// Start runs a proxy of type "socks5" or "http" on loopback until the test
// ends.
func Start(t testing.TB, typ string, opts ...Option) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Addr: ln.Addr().String(), Type: typ, ln: ln, conns: map[net.Conn]struct{}{}}
	for _, o := range opts {
		o(s)
	}
	s.wg.Go(s.serve)
	t.Cleanup(s.Close)
	return s
}

// Requests returns the destinations asked for so far.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// Close stops the proxy and ends every connection through it.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	_ = s.ln.Close()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) record(r string) {
	s.mu.Lock()
	s.requests = append(s.requests, r)
	s.mu.Unlock()
}

// track registers c to be closed with the server; false if it is closed
// already.
func (s *Server) track(c io.Closer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = c.Close()
		return false
	}
	if nc, ok := c.(net.Conn); ok {
		s.conns[nc] = struct{}{}
	}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	_ = c.Close()
}

func (s *Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		if !s.track(c) {
			return
		}
		s.wg.Go(func() {
			defer s.untrack(c)
			if s.Type == "http" {
				s.serveHTTP(c)
			} else {
				s.serveSOCKS(c)
			}
		})
	}
}

// pipe copies between a and b until either side ends.
func (s *Server) pipe(a, b net.Conn) {
	if !s.track(b) {
		return
	}
	defer s.untrack(b)
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
}

func (s *Server) serveHTTP(c net.Conn) {
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		_, _ = io.WriteString(c, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
		return
	}
	if s.username != "" {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(s.username+":"+s.password))
		if req.Header.Get("Proxy-Authorization") != want {
			_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic\r\nContent-Length: 0\r\n\r\n")
			return
		}
	}
	s.record("tcp " + req.Host)
	up, err := net.Dial("tcp", req.Host)
	if err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		_ = up.Close()
		return
	}
	client := c
	if br.Buffered() > 0 {
		client = &readerConn{Conn: c, r: br}
	}
	s.pipe(client, up)
}

type readerConn struct {
	net.Conn
	r io.Reader
}

func (c *readerConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func (s *Server) serveSOCKS(c net.Conn) {
	var head [2]byte
	if _, err := io.ReadFull(c, head[:]); err != nil || head[0] != 5 {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	want := byte(0)
	if s.username != "" {
		want = 2
	}
	offered := false
	for _, m := range methods {
		offered = offered || m == want
	}
	if !offered {
		_, _ = c.Write([]byte{5, 0xff})
		return
	}
	if _, err := c.Write([]byte{5, want}); err != nil {
		return
	}
	if want == 2 && !s.socksAuth(c) {
		return
	}

	var req [4]byte
	if _, err := io.ReadFull(c, req[:]); err != nil || req[0] != 5 {
		return
	}
	dst, err := readAddr(c, req[3])
	if err != nil {
		return
	}
	switch req[1] {
	case 1: // CONNECT
		s.record("tcp " + dst)
		up, err := net.Dial("tcp", dst)
		if err != nil {
			_, _ = c.Write(reply(5, netip.AddrPort{}))
			return
		}
		local, _ := netip.ParseAddrPort(up.LocalAddr().String())
		if _, err := c.Write(reply(0, local)); err != nil {
			_ = up.Close()
			return
		}
		s.pipe(c, up)
	case 3: // UDP ASSOCIATE
		if s.noUDP {
			_, _ = c.Write(reply(7, netip.AddrPort{}))
			return
		}
		s.relayUDP(c)
	default:
		_, _ = c.Write(reply(7, netip.AddrPort{}))
	}
}

func (s *Server) socksAuth(c net.Conn) bool {
	var ver [2]byte
	if _, err := io.ReadFull(c, ver[:]); err != nil || ver[0] != 1 {
		return false
	}
	user := make([]byte, ver[1])
	if _, err := io.ReadFull(c, user); err != nil {
		return false
	}
	var l [1]byte
	if _, err := io.ReadFull(c, l[:]); err != nil {
		return false
	}
	pass := make([]byte, l[0])
	if _, err := io.ReadFull(c, pass); err != nil {
		return false
	}
	ok := string(user) == s.username && string(pass) == s.password
	status := byte(1)
	if ok {
		status = 0
	}
	_, _ = c.Write([]byte{1, status})
	return ok
}

// relayUDP relays datagrams between the client and their destinations
// until the control connection c closes.
func (s *Server) relayUDP(c net.Conn) {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_, _ = c.Write(reply(1, netip.AddrPort{}))
		return
	}
	if !s.track(pc) {
		return
	}
	defer s.untrack(pc)
	if _, err := c.Write(reply(0, pc.LocalAddr().(*net.UDPAddr).AddrPort())); err != nil {
		return
	}
	go func() {
		_, _ = io.Copy(io.Discard, c)
		_ = pc.Close()
	}()
	var client netip.AddrPort
	buf := make([]byte, 65536)
	for {
		n, from, err := pc.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		from = netip.AddrPortFrom(from.Addr().Unmap(), from.Port())
		b := buf[:n]
		if !client.IsValid() || from == client {
			// From the client: the header, then the payload.
			if len(b) < 4 || b[2] != 0 {
				continue
			}
			client = from
			dst, rest, err := parseAddr(b[3:])
			if err != nil {
				continue
			}
			s.record("udp " + dst.String())
			_, _ = pc.WriteToUDPAddrPort(rest, dst)
			continue
		}
		// From a destination, back to the client.
		pkt := append([]byte{0, 0, 0}, addrBytes(from)...)
		_, _ = pc.WriteToUDPAddrPort(append(pkt, b...), client)
	}
}

func reply(code byte, bound netip.AddrPort) []byte {
	if !bound.IsValid() {
		bound = netip.AddrPortFrom(netip.IPv4Unspecified(), 0)
	}
	return append([]byte{5, code, 0}, addrBytes(bound)...)
}

func addrBytes(ap netip.AddrPort) []byte {
	ip := ap.Addr().Unmap()
	var b []byte
	if ip.Is4() {
		b = append(b, 1)
	} else {
		b = append(b, 4)
	}
	b = append(b, ip.AsSlice()...)
	return binary.BigEndian.AppendUint16(b, ap.Port())
}

// readAddr reads a SOCKS5 address and port as host:port.
func readAddr(r io.Reader, atyp byte) (string, error) {
	var host string
	switch atyp {
	case 1, 4:
		b := make([]byte, map[byte]int{1: 4, 4: 16}[atyp])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		ip, _ := netip.AddrFromSlice(b)
		host = ip.Unmap().String()
	case 3:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return "", err
		}
		b := make([]byte, l[0])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", errors.New("unknown address type")
	}
	var p [2]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(p[:])))), nil
}

// parseAddr parses an IP address and port at the start of b.
func parseAddr(b []byte) (netip.AddrPort, []byte, error) {
	if len(b) < 1 {
		return netip.AddrPort{}, nil, errors.New("short")
	}
	n := map[byte]int{1: 4, 4: 16}[b[0]]
	if n == 0 || len(b) < 1+n+2 {
		return netip.AddrPort{}, nil, errors.New("bad address")
	}
	ip, _ := netip.AddrFromSlice(b[1 : 1+n])
	return netip.AddrPortFrom(ip.Unmap(), binary.BigEndian.Uint16(b[1+n:])), b[1+n+2:], nil
}
