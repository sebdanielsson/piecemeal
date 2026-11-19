package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
)

// SOCKS5 protocol constants (RFC 1928, RFC 1929).
const (
	socksVersion = 5

	methodNone     = 0x00
	methodPassword = 0x02
	methodRefused  = 0xff

	cmdConnect      = 0x01
	cmdUDPAssociate = 0x03

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04
)

var socksReplies = map[byte]string{
	0x01: "general failure",
	0x02: "not allowed by its rules",
	0x03: "network unreachable",
	0x04: "host unreachable",
	0x05: "connection refused",
	0x06: "TTL expired",
	0x07: "command not supported",
	0x08: "address type not supported",
}

// socksRequest authenticates on conn and sends a request; it returns the
// address in the proxy's reply.
func (s *Server) socksRequest(conn net.Conn, cmd byte, addr string) (netip.AddrPort, error) {
	if err := s.socksAuth(conn); err != nil {
		return netip.AddrPort{}, err
	}
	req := []byte{socksVersion, cmd, 0}
	req, err := appendAddr(req, addr)
	if err != nil {
		return netip.AddrPort{}, err
	}
	if _, err := conn.Write(req); err != nil {
		return netip.AddrPort{}, s.errorf("%w", err)
	}
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return netip.AddrPort{}, s.errorf("reading its reply: %w", err)
	}
	if head[0] != socksVersion {
		return netip.AddrPort{}, s.errorf("not a SOCKS5 proxy")
	}
	if head[1] != 0 {
		reason, ok := socksReplies[head[1]]
		if !ok {
			reason = fmt.Sprintf("error %d", head[1])
		}
		if cmd == cmdUDPAssociate && head[1] == 0x07 {
			reason = "it doesn't relay UDP"
		}
		return netip.AddrPort{}, s.errorf("%s: %s", addr, reason)
	}
	bound, err := readAddr(conn, head[3])
	if errors.Is(err, errDomainAddr) && cmd == cmdConnect {
		// Where the proxy connects from doesn't matter.
		return netip.AddrPort{}, nil
	}
	if err != nil {
		return netip.AddrPort{}, s.errorf("reading its reply: %w", err)
	}
	return bound, nil
}

// socksAuth offers no authentication, or username and password when there
// is a username, and runs the method the proxy picks.
func (s *Server) socksAuth(conn net.Conn) error {
	method := byte(methodNone)
	if s.Username != "" {
		method = methodPassword
	}
	if _, err := conn.Write([]byte{socksVersion, 1, method}); err != nil {
		return s.errorf("%w", err)
	}
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return s.errorf("reading its greeting: %w", err)
	}
	switch {
	case reply[0] != socksVersion:
		return s.errorf("not a SOCKS5 proxy")
	case reply[1] == methodRefused && s.Username == "":
		return s.errorf("it wants a username and password")
	case reply[1] == methodRefused:
		return s.errorf("it doesn't take a username and password")
	case reply[1] != method:
		return s.errorf("it picked authentication method %d, which wasn't offered", reply[1])
	case method == methodNone:
		return nil
	}
	if len(s.Username) > 255 || len(s.Password) > 255 {
		return s.errorf("the username and password must be at most 255 bytes each")
	}
	msg := []byte{1, byte(len(s.Username))} //nolint:gosec // at most 255, checked above
	msg = append(msg, s.Username...)
	msg = append(msg, byte(len(s.Password))) //nolint:gosec // at most 255, checked above
	msg = append(msg, s.Password...)
	if _, err := conn.Write(msg); err != nil {
		return s.errorf("%w", err)
	}
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return s.errorf("reading its answer to the username and password: %w", err)
	}
	if reply[1] != 0 {
		return s.errorf("it refused the username or password")
	}
	return nil
}

func (s *Server) errorf(format string, args ...any) error {
	return fmt.Errorf("proxy %s: "+format, append([]any{s.Addr}, args...)...)
}

// appendAddr appends a SOCKS5 address and port: an IP address as such,
// anything else as a domain name for the proxy to look up.
func appendAddr(b []byte, addr string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("bad port in %q", addr)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if ip.Is4() {
			b = append(b, atypIPv4)
		} else {
			b = append(b, atypIPv6)
		}
		b = append(b, ip.AsSlice()...)
	} else {
		if len(host) == 0 || len(host) > 255 {
			return nil, fmt.Errorf("bad host in %q", addr)
		}
		b = append(b, atypDomain, byte(len(host))) //nolint:gosec // at most 255, checked above
		b = append(b, host...)
	}
	return binary.BigEndian.AppendUint16(b, uint16(port)), nil
}

// appendAddrPort appends a SOCKS5 IP address and port.
func appendAddrPort(b []byte, ap netip.AddrPort) []byte {
	ip := ap.Addr().Unmap()
	if ip.Is4() {
		b = append(b, atypIPv4)
	} else {
		b = append(b, atypIPv6)
	}
	b = append(b, ip.AsSlice()...)
	return binary.BigEndian.AppendUint16(b, ap.Port())
}

var errDomainAddr = errors.New("a domain name where an address was expected")

// readAddr reads a SOCKS5 address of type atyp and its port. A domain name
// is read and reported as errDomainAddr.
func readAddr(r io.Reader, atyp byte) (netip.AddrPort, error) {
	var n int
	switch atyp {
	case atypIPv4:
		n = 4
	case atypIPv6:
		n = 16
	case atypDomain:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return netip.AddrPort{}, err
		}
		if _, err := io.ReadFull(r, make([]byte, int(l[0])+2)); err != nil {
			return netip.AddrPort{}, err
		}
		return netip.AddrPort{}, errDomainAddr
	default:
		return netip.AddrPort{}, fmt.Errorf("unknown address type %d", atyp)
	}
	b := make([]byte, n+2)
	if _, err := io.ReadFull(r, b); err != nil {
		return netip.AddrPort{}, err
	}
	ip, _ := netip.AddrFromSlice(b[:n])
	return netip.AddrPortFrom(ip.Unmap(), binary.BigEndian.Uint16(b[n:])), nil
}

// parseAddr parses a SOCKS5 address and port at the start of b and returns
// it and the rest of b.
func parseAddr(b []byte) (netip.AddrPort, []byte, error) {
	if len(b) < 1 {
		return netip.AddrPort{}, nil, errors.New("short address")
	}
	var n int
	switch b[0] {
	case atypIPv4:
		n = 4
	case atypIPv6:
		n = 16
	case atypDomain:
		return netip.AddrPort{}, nil, errDomainAddr
	default:
		return netip.AddrPort{}, nil, fmt.Errorf("unknown address type %d", b[0])
	}
	if len(b) < 1+n+2 {
		return netip.AddrPort{}, nil, errors.New("short address")
	}
	ip, _ := netip.AddrFromSlice(b[1 : 1+n])
	port := binary.BigEndian.Uint16(b[1+n:])
	return netip.AddrPortFrom(ip.Unmap(), port), b[1+n+2:], nil
}
