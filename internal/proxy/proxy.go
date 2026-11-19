// Package proxy connects through a SOCKS5 or HTTP proxy server: TCP
// connections with SOCKS5 CONNECT (RFC 1928, with RFC 1929 username and
// password) or HTTP CONNECT, and UDP datagrams through a SOCKS5 proxy's UDP
// relay (UDP ASSOCIATE).
//
// Nothing here falls back to a direct connection: when the proxy can't
// carry something, the call fails.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// Proxy types.
const (
	SOCKS5 = "socks5"
	HTTP   = "http"
)

// Server is a proxy server and the credentials to use it. Without a
// username, no credentials are sent.
type Server struct {
	// Type is SOCKS5 or HTTP.
	Type string
	// Addr is the proxy's host:port.
	Addr     string
	Username string
	Password string
	// Bind, when set, adjusts the sockets to the proxy before they
	// connect, such as binding them to a network interface.
	Bind Controller
}

// Controller adjusts a socket before it connects, as net.Dialer's Control.
type Controller interface {
	Control(network, address string, c syscall.RawConn) error
}

// control is the Control function for sockets to the proxy.
func (s *Server) control() func(network, address string, c syscall.RawConn) error {
	if s.Bind == nil {
		return nil
	}
	return s.Bind.Control
}

// ErrNoUDP is returned for UDP through an HTTP proxy, which only carries
// TCP.
var ErrNoUDP = errors.New("an HTTP proxy can't carry UDP")

// ErrUnreachable is returned when the proxy itself can't be reached, so
// trying other destinations through it is pointless.
var ErrUnreachable = errors.New("can't reach the proxy")

// handshakeTimeout bounds connecting to the proxy and its reply, when the
// context has no earlier deadline.
const handshakeTimeout = 30 * time.Second

// DialContext connects to addr, a host:port, through the proxy. A hostname
// in addr is passed to the proxy, which looks it up. network must be tcp,
// tcp4 or tcp6.
func (s *Server) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("proxy: can't dial %s", network)
	}
	// As net.Dialer does, an address of the other family fails tcp4 and
	// tcp6: callers such as anacrolix keep a family out that way.
	if err := checkFamily(network, addr); err != nil {
		return nil, err
	}
	conn, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	err = handshake(ctx, conn, func() error {
		switch s.Type {
		case SOCKS5:
			_, err := s.socksRequest(conn, cmdConnect, addr)
			return err
		case HTTP:
			c, err := s.httpConnect(conn, addr)
			conn = c
			return err
		default:
			return fmt.Errorf("proxy: unknown type %q", s.Type)
		}
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// DialResolved is DialContext, but looks hostnames up locally and passes
// the proxy an address, trying each until one connects.
func (s *Server) DialResolved(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return s.DialContext(ctx, network, addr)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, ipNetwork(network), host)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, ip := range ips {
		c, err := s.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return c, nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil || errors.Is(err, ErrUnreachable) {
			break
		}
	}
	return nil, errors.Join(errs...)
}

// checkFamily fails an IP address that network's family excludes.
func checkFamily(network, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return nil // a hostname, which the proxy looks up
	}
	if (network == "tcp4" && !ip.Unmap().Is4()) || (network == "tcp6" && ip.Unmap().Is4()) {
		return fmt.Errorf("proxy: %s isn't a %s address", host, network)
	}
	return nil
}

func ipNetwork(network string) string {
	switch network {
	case "tcp4", "udp4":
		return "ip4"
	case "tcp6", "udp6":
		return "ip6"
	}
	return "ip"
}

// connect opens a TCP connection to the proxy.
func (s *Server) connect(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: handshakeTimeout, Control: s.control()}
	conn, err := d.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrUnreachable, s.Addr, err)
	}
	return conn, nil
}

// handshake runs fn, which talks to the proxy over conn, within the
// context's deadline and the handshake timeout, and gives up when the
// context ends.
func handshake(ctx context.Context, conn net.Conn, fn func() error) error {
	deadline := time.Now().Add(handshakeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	err := fn()
	if !stop() {
		// The context ended, and the deadline may be moving to the past
		// right now: the caller has to close conn.
		return errors.Join(ctx.Err(), err)
	}
	_ = conn.SetDeadline(time.Time{})
	return err
}
