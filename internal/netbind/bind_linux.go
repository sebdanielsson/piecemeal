package netbind

import "golang.org/x/sys/unix"

const supported = true

// bind sets SO_BINDTODEVICE, which needs no privileges since Linux 5.7.
// The kernel resolves the name now, so a socket stays on this instance of
// the interface: if it is created again, the socket can't send anymore.
func (b *Binder) bind(fd uintptr, _ string) error {
	return unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, b.name)
}
