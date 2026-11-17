package watch

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

type fakeAdder struct{ added []engine.AddParams }

func (f *fakeAdder) Add(_ context.Context, p engine.AddParams) (string, error) {
	if p.Magnet == "magnet:?xt=urn:btih:dupe" {
		return "", engine.ErrExists
	}
	f.added = append(f.added, p)
	return "x", nil
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.magnet", "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567\n")
	write("dupe.magnet", "magnet:?xt=urn:btih:dupe")
	write("bad.torrent", "not bencode")
	write("notes.txt", "ignored")

	a := &fakeAdder{}
	w := &Watcher{Dir: dir, Adder: a, Log: slog.New(slog.DiscardHandler)}
	w.Scan(context.Background())

	if len(a.added) != 1 || a.added[0].Magnet == "" {
		t.Fatalf("added %+v", a.added)
	}
	for name, exists := range map[string]bool{
		"a.magnet": false, "dupe.magnet": false, "bad.torrent": false, "bad.torrent.failed": true, "notes.txt": true,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if exists != (err == nil) || (err != nil && !errors.Is(err, os.ErrNotExist)) {
			t.Errorf("%s: exists=%v, err=%v", name, exists, err)
		}
	}
}
