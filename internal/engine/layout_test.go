package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
)

func TestContentAndRoot(t *testing.T) {
	for _, tc := range []struct {
		rel           []string
		content, root string
	}{
		{[]string{"a.mkv"}, "/s/a.mkv", ""},
		{[]string{"a/a.mkv"}, "/s/a/a.mkv", "/s/a"},
		{[]string{"R/x.mkv", "R/y.nfo"}, "/s/R", "/s/R"},
		{[]string{"x.mkv", "y.nfo"}, "/s", ""},
		{[]string{"R/x.mkv", "Q/y.nfo"}, "/s", ""},
		{nil, "/s", ""},
	} {
		c, r := contentAndRoot("/s", tc.rel)
		if c != tc.content || r != tc.root {
			t.Errorf("contentAndRoot(%v) = %q, %q; want %q, %q", tc.rel, c, r, tc.content, tc.root)
		}
	}
}

func TestLayoutPath(t *testing.T) {
	single := &metainfo.Info{Name: "movie.mkv", Length: 1}
	multi := &metainfo.Info{Name: "Show", Files: []metainfo.FileInfo{{Path: []string{"s01", "e01.mkv"}, Length: 1}}}
	mf := multi.UpvertedFiles()[0]
	sf := single.UpvertedFiles()[0]
	for _, tc := range []struct {
		info   *metainfo.Info
		fi     metainfo.FileInfo
		layout string
		want   string
	}{
		{single, sf, LayoutOriginal, "movie.mkv"},
		{single, sf, LayoutSubfolder, "movie/movie.mkv"},
		{single, sf, LayoutNoSubfolder, "movie.mkv"},
		{multi, mf, LayoutOriginal, "Show/s01/e01.mkv"},
		{multi, mf, LayoutSubfolder, "Show/s01/e01.mkv"},
		{multi, mf, LayoutNoSubfolder, "s01/e01.mkv"},
	} {
		if got := layoutPath(tc.info, tc.fi, tc.layout); got != tc.want {
			t.Errorf("layoutPath(%s, %s) = %q, want %q", tc.info.Name, tc.layout, got, tc.want)
		}
	}
}

func TestSubfolderLayoutAndRename(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 100 << 10, "sub/b.bin": 50 << 10})
	h := newHarness(t)
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo, ContentLayout: LayoutNoSubfolder})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	st := h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	if st.ContentPath != h.saveDir || st.RootPath != "" {
		t.Fatalf("NoSubfolder paths: content %q root %q", st.ContentPath, st.RootPath)
	}
	if _, err := os.Stat(filepath.Join(h.saveDir, "sub", "b.bin")); err != nil {
		t.Fatalf("NoSubfolder file: %v", err)
	}

	// Rename a file, then the folder it's in.
	if err := h.engine.RenamePath(hash, "a.bin", "renamed/a2.bin"); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.RenamePath(hash, "sub", "deeper/sub2"); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.RenamePath(hash, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renaming a missing path: %v", err)
	}
	if err := h.engine.RenamePath(hash, "renamed/a2.bin", "../escape"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("renaming out of the save path: %v", err)
	}
	for _, p := range []string{"renamed/a2.bin", "deeper/sub2/b.bin"} {
		if _, err := os.Stat(filepath.Join(h.saveDir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.saveDir, "sub")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("emptied folder left behind: %v", err)
	}

	// The renames survive a restart, and the data is still complete there.
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	files, err := h.engine.Files(hash)
	if err != nil || len(files) != 2 || files[0].Path != "renamed/a2.bin" || files[1].Path != "deeper/sub2/b.bin" {
		t.Fatalf("files after restart: %+v %v", files, err)
	}
	h.waitFor(hash, "complete after restart", func(st Torrent) bool { return st.Complete && st.State != StateChecking })
}
