package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

// ListenPacket opens a UDP relay through a SOCKS5 proxy (UDP ASSOCIATE):
// datagrams written to the returned conn go to their destination through
// the proxy, and their replies come back through it. Destinations must be
// *net.UDPAddr. The relay lasts until the conn is closed or the proxy
// ends it; reads then fail.
func (s *Server) ListenPacket(ctx context.Context) (net.PacketConn, error) {
	if s.Type != SOCKS5 {
		return nil, ErrNoUDP
	}
	ctrl, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	var relay netip.AddrPort
	err = handshake(ctx, ctrl, func() error {
		// The address we'll send from isn't known (NAT may change it), so
		// it is all zeros, as RFC 1928 says.
		r, err := s.socksRequest(ctrl, cmdUDPAssociate, "0.0.0.0:0")
		relay = r
		return err
	})
	if errors.Is(err, errDomainAddr) {
		err = s.errorf("it named its UDP relay by hostname, which isn't supported")
	}
	if err != nil {
		_ = ctrl.Close()
		return nil, err
	}
	if relay.Addr().IsUnspecified() {
		// The relay is on the proxy's own address.
		if ap, err := netip.ParseAddrPort(ctrl.RemoteAddr().String()); err == nil {
			relay = netip.AddrPortFrom(ap.Addr().Unmap(), relay.Port())
		}
	}
	lc := net.ListenConfig{Control: s.control()}
	conn, err := lc.ListenPacket(ctx, "udp", ":0")
	if err != nil {
		_ = ctrl.Close()
		return nil, s.errorf("opening a UDP socket for its relay: %w", err)
	}
	pc, ok := conn.(*net.UDPConn)
	if !ok {
		_ = conn.Close()
		_ = ctrl.Close()
		return nil, s.errorf("opening a UDP socket for its relay: got %T", conn)
	}
	c := &udpConn{ctrl: ctrl, pc: pc, relay: relay}
	// The relay ends when the proxy closes the control connection: close
	// the socket too, so reads fail and the caller opens a new relay.
	go func() {
		_, _ = io.Copy(io.Discard, ctrl)
		_ = c.Close()
	}()
	return c, nil
}

// udpConn is a SOCKS5 UDP relay.
type udpConn struct {
	ctrl  net.Conn
	pc    *net.UDPConn
	relay netip.AddrPort

	readMu sync.Mutex
	buf    [65536]byte

	closeOnce sync.Once
}

// header is the SOCKS5 UDP request header: reserved, fragment 0, address.
func header(b []byte, to netip.AddrPort) []byte {
	return appendAddrPort(append(b, 0, 0, 0), to)
}

func (c *udpConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("proxy: can't send UDP to %T", addr)
	}
	to := ua.AddrPort()
	pkt := header(make([]byte, 0, 22+len(p)), to)
	pkt = append(pkt, p...)
	if _, err := c.pc.WriteToUDPAddrPort(pkt, c.relay); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *udpConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		n, from, err := c.pc.ReadFromUDPAddrPort(c.buf[:])
		if err != nil {
			return 0, nil, err
		}
		if from.Addr().Unmap() != c.relay.Addr() || from.Port() != c.relay.Port() {
			// Only the relay may send to this socket.
			continue
		}
		b := c.buf[:n]
		// Reserved, then the fragment number: fragments aren't supported,
		// and RFC 1928 lets a client drop them.
		if len(b) < 3 || b[2] != 0 {
			continue
		}
		src, payload, err := parseAddr(b[3:])
		if err != nil {
			continue
		}
		return copy(p, payload), net.UDPAddrFromAddrPort(src), nil
	}
}

func (c *udpConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = errors.Join(c.pc.Close(), c.ctrl.Close())
	})
	return err
}

func (c *udpConn) LocalAddr() net.Addr                { return c.pc.LocalAddr() }
func (c *udpConn) SetDeadline(t time.Time) error      { return c.pc.SetDeadline(t) }
func (c *udpConn) SetReadDeadline(t time.Time) error  { return c.pc.SetReadDeadline(t) }
func (c *udpConn) SetWriteDeadline(t time.Time) error { return c.pc.SetWriteDeadline(t) }
