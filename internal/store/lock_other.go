//go:build !unix

package store

import "errors"

// ErrLocked means another piecemeal is using the data directory.
var ErrLocked = errors.New("another piecemeal is using the data directory")

// LockDataDir does nothing where flock isn't available.
func LockDataDir(string) (func(), error) { return func() {}, nil }
