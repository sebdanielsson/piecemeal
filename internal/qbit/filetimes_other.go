//go:build !linux && !darwin

package qbit

import (
	"os"
	"time"
)

// fileTimes returns the modification time for both where the platform's
// creation and access times aren't read.
func fileTimes(fi os.FileInfo) (created, accessed time.Time) {
	return fi.ModTime(), fi.ModTime()
}
