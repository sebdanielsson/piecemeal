// Package netbind keeps sockets on one network interface, such as a VPN's
// wg0, so their traffic can't take another route when the interface goes
// away: connecting fails instead. It sets SO_BINDTODEVICE on Linux and
// IP_BOUND_IF on macOS.
package netbind

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// Binder binds sockets to an interface. A nil Binder binds nothing.
type Binder struct {
	name string
	// lookup finds the interface; tests replace it.
	lookup func(string) (*net.Interface, error)
}

// ErrUnsupported is returned on platforms that can't bind to an interface.
var ErrUnsupported = errors.New("binding to a network interface is supported on Linux and macOS only")

// New returns a Binder for the interface name, or nil for "".
func New(name string) (*Binder, error) {
	if name == "" {
		return nil, nil //nolint:nilnil // no interface means no Binder
	}
	if !supported {
		return nil, ErrUnsupported
	}
	return &Binder{name: name, lookup: net.InterfaceByName}, nil
}

// Name is the interface's name.
func (b *Binder) Name() string { return b.name }

// Control binds a socket to the interface; it is a net.Dialer's and a
// net.ListenConfig's Control. It fails when the interface doesn't exist.
func (b *Binder) Control(network, _ string, c syscall.RawConn) error {
	var err error
	cerr := c.Control(func(fd uintptr) { err = b.bind(fd, network) })
	if cerr != nil {
		return cerr
	}
	if err != nil {
		return fmt.Errorf("binding to network interface %s: %w", b.name, err)
	}
	return nil
}

// Dialer returns a dialer bound to the interface.
func (b *Binder) Dialer() *net.Dialer {
	return &net.Dialer{Timeout: 30 * time.Second, Control: b.Control}
}

// DialContext connects through the interface.
func (b *Binder) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return b.Dialer().DialContext(ctx, network, addr)
}

// ListenPacket opens a UDP socket bound to the interface.
func (b *Binder) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := net.ListenConfig{Control: b.Control}
	return lc.ListenPacket(context.Background(), network, addr)
}

// Resolver looks names up through the interface. A name server on
// loopback, such as Docker's or systemd-resolved's stub, is asked
// directly, since a bound socket can't reach it; where it sends the query
// on is up to it.
func (b *Binder) Resolver() *net.Resolver {
	plain := &net.Dialer{Timeout: 10 * time.Second}
	bound := b.Dialer()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if ap, err := netip.ParseAddrPort(addr); err == nil && ap.Addr().IsLoopback() {
				return plain.DialContext(ctx, network, addr)
			}
			return bound.DialContext(ctx, network, addr)
		},
	}
}

// State is whether the interface exists and is up, and its index, which
// changes when the interface is created again.
type State struct {
	Up    bool
	Index int
}

// State reports the interface's state now.
func (b *Binder) State() State {
	ifi, err := b.lookup(b.name)
	if err != nil {
		return State{}
	}
	return State{Up: ifi.Flags&net.FlagUp != 0, Index: ifi.Index}
}
