package engine

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

func TestCreateTorrent(t *testing.T) {
	h := newHarness(t)
	src := filepath.Join(h.saveDir, "made")
	for name, size := range map[string]int{"a.bin": 100 << 10, "dir/b.bin": 70 << 10} {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.Repeat([]byte{1}, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.engine.CreateTorrent(CreateParams{SourcePath: "/etc"}); !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("source outside the roots: %v", err)
	}
	if _, err := h.engine.CreateTorrent(CreateParams{SourcePath: src, PieceSize: 1000}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad piece size: %v", err)
	}
	id, err := h.engine.CreateTorrent(CreateParams{
		SourcePath: src, Private: true, Trackers: [][]string{{"https://t.example/announce"}}, Comment: "made here",
		Source: "TEST", StartSeeding: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var task CreateTask
	deadline := time.Now().Add(20 * time.Second)
	for {
		for _, x := range h.engine.CreateTasks() {
			if x.ID == id {
				task = x
			}
		}
		if task.Status == "finished" || task.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task: %+v", task)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.Status != "finished" || task.Progress != 1 {
		t.Fatalf("task: %+v", task)
	}
	b, err := h.engine.CreatedTorrent(id)
	if err != nil {
		t.Fatal(err)
	}
	mi, err := metainfo.Load(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := mi.UnmarshalInfo()
	if info.Name != "made" || len(info.Files) != 2 || !*info.Private || info.Source != "TEST" || mi.Comment != "made here" ||
		mi.HashInfoBytes().HexString() != task.Hash {
		t.Fatalf("metainfo: %+v %+v", mi, info)
	}
	// Seeding from the source, already complete.
	h.waitFor(task.Hash, "seeding the created torrent", func(st Torrent) bool { return st.Complete && st.Private })
	if err := h.engine.DeleteCreateTask(id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.CreatedTorrent(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}
