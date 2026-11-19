package netbind

import (
	"strings"

	"golang.org/x/sys/unix"
)

const supported = true

// bind sets IP_BOUND_IF or IPV6_BOUND_IF to the interface's index.
func (b *Binder) bind(fd uintptr, network string) error {
	ifi, err := b.lookup(b.name)
	if err != nil {
		return err
	}
	if strings.HasSuffix(network, "6") {
		return unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, ifi.Index)
	}
	return unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, ifi.Index)
}
