//go:build unix

package engine

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestIPFilterRefusesFIFOWithoutBlocking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := readIPFilter(p)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a FIFO was read as a filter")
		}
	case <-time.After(5 * time.Second):
		// Unblock the open so the goroutine ends.
		if f, err := os.OpenFile(p, os.O_WRONLY, 0); err == nil {
			_ = f.Close()
		}
		t.Fatal("reading a FIFO blocked")
	}
}

func TestIPFilterRefusesLargeFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	// Sparse, so it takes no space.
	if err := f.Truncate(maxIPFilterSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, _, err := readIPFilter(p); err == nil {
		t.Fatal("an oversized filter was read")
	}
}
