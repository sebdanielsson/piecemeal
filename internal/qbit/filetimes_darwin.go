package qbit

import (
	"os"
	"syscall"
	"time"
)

// fileTimes returns a file's creation and last access times.
func fileTimes(fi os.FileInfo) (created, accessed time.Time) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.ModTime(), fi.ModTime()
	}
	return time.Unix(st.Birthtimespec.Unix()), time.Unix(st.Atimespec.Unix())
}
