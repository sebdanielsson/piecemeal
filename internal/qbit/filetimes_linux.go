package qbit

import (
	"os"
	"syscall"
	"time"
)

// fileTimes returns a file's creation and last access times. Linux's stat
// has no creation time; the inode change time is the closest it reports.
func fileTimes(fi os.FileInfo) (created, accessed time.Time) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.ModTime(), fi.ModTime()
	}
	return time.Unix(st.Ctim.Unix()), time.Unix(st.Atim.Unix())
}
