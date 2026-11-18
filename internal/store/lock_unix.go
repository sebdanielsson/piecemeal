//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrLocked means another piecemeal is using the data directory.
var ErrLocked = errors.New("another piecemeal is using the data directory")

// LockDataDir takes an exclusive lock on the data directory, held until
// unlock is called or the process exits.
func LockDataDir(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "piecemeal.lock"), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the data directory
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, dir)
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
