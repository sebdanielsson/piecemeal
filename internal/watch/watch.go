// Package watch adds the .torrent and .magnet files dropped into a
// directory. It polls rather than using inotify, which doesn't see changes
// made through bind mounts or by other hosts on network filesystems.
package watch

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// Adder adds a torrent; *engine.Engine implements it.
type Adder interface {
	Add(ctx context.Context, p engine.AddParams) (string, error)
}

// Watcher polls a directory.
type Watcher struct {
	Dir      string
	Interval time.Duration
	// Settle skips files modified more recently than this, which may still
	// be being written.
	Settle time.Duration
	Adder  Adder
	Log    *slog.Logger
}

// Run polls until ctx ends. Added files are deleted; files that fail to add
// are renamed with a .failed suffix so they aren't retried forever.
func (w *Watcher) Run(ctx context.Context) {
	tick := time.NewTicker(w.Interval)
	defer tick.Stop()
	for {
		w.Scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Scan adds every settled file once.
func (w *Watcher) Scan(ctx context.Context) {
	entries, err := os.ReadDir(w.Dir)
	if err != nil {
		w.Log.Warn("reading watch directory", "dir", w.Dir, "error", err)
		return
	}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".torrent" && ext != ".magnet") {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < w.Settle {
			continue
		}
		p := filepath.Join(w.Dir, e.Name())
		b, err := os.ReadFile(p) //nolint:gosec // the watch directory is the operator's choice
		if err != nil {
			w.Log.Warn("reading watched file", "file", p, "error", err)
			continue
		}
		var params engine.AddParams
		params.Magnet, params.Metainfo, err = engine.ParseSource(b)
		if err == nil {
			_, err = w.Adder.Add(ctx, params)
		}
		if err != nil && !errors.Is(err, engine.ErrExists) {
			w.Log.Warn("adding watched file", "file", p, "error", err)
			_ = os.Rename(p, p+".failed")
			continue
		}
		w.Log.Info("added watched file", "file", p)
		_ = os.Remove(p)
	}
}
