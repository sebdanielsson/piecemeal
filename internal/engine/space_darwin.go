package engine

import "golang.org/x/sys/unix"

// freeSpace returns the bytes available to unprivileged users on the
// filesystem holding path, or -1.
func freeSpace(path string) int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize) //nolint:gosec // block counts fit in int64
}
