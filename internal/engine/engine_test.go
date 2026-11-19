package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/types"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

var offline = enginetest.Offline

// seeder wraps enginetest.Seeder; its files live under "payload".
type seeder struct{ *enginetest.Seeder }

func newSeeder(t *testing.T, files map[string]int) *seeder {
	return &seeder{enginetest.NewSeeder(t, "payload", files)}
}

type harness struct {
	t        *testing.T
	dataDir  string
	saveDir  string
	store    *store.Store
	settings *settings.Manager
	engine   *Engine
	// log and wrapStorage, when set, go to the engine's Config.
	log         *slog.Logger
	wrapStorage func(storage.ClientImplCloser) storage.ClientImplCloser
}

// newHarness starts an engine; opts adjust the harness before it starts.
func newHarness(t *testing.T, opts ...func(*harness)) *harness {
	t.Helper()
	h := &harness{t: t, dataDir: t.TempDir(), saveDir: t.TempDir()}
	var err error
	h.store, err = store.Open(context.Background(), h.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.store.Close() })
	defaults := settings.Defaults(h.saveDir)
	defaults.PeerPort = 0
	defaults.DHT = false
	h.settings, err = settings.Load(context.Background(), h.store, defaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range opts {
		o(h)
	}
	h.start()
	return h
}

func (h *harness) start() {
	h.t.Helper()
	log := h.log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var err error
	h.engine, err = New(context.Background(), Config{
		DataDir:            h.dataDir,
		AllowedRoots:       []string{h.saveDir},
		TickInterval:       50 * time.Millisecond,
		CheckpointInterval: time.Second,
		ClientConfig:       offline,
		wrapStorage:        h.wrapStorage,
	}, h.store, h.settings, log)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = h.engine.Close() })
}

func (h *harness) waitFor(hash string, what string, cond func(Torrent) bool) Torrent {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, ok := h.engine.Get(hash)
		if ok && cond(st) {
			return st
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s; last status: %+v", what, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *harness) checkContent(s *seeder, root string) {
	h.t.Helper()
	h.checkContentIn(s, filepath.Join(h.saveDir, root))
}

// checkContentIn checks the seeder's files are in dir/payload.
func (h *harness) checkContentIn(s *seeder, dir string) {
	h.t.Helper()
	if filepath.Base(dir) != "payload" {
		dir = filepath.Join(dir, "payload")
	}
	for name, want := range s.Content {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			h.t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			h.t.Fatalf("%s: content differs", name)
		}
	}
}

func TestDownloadStopStartRestartRemove(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 300 << 10, "sub/b.bin": 100 << 10})
	h := newHarness(t)

	if err := h.engine.PutCategory(ctx, NewCategory("movies"), true); err != nil {
		t.Fatal(err)
	}
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo, Category: "movies", Tags: []string{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	if hash != s.Hash {
		t.Fatalf("hash %s, want %s", hash, s.Hash)
	}
	if _, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo}); !errors.Is(err, ErrExists) {
		t.Fatalf("adding twice: %v, want ErrExists", err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	st := h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	if st.Size != 400<<10 || st.Progress != 1 || st.Category != "movies" || len(st.Tags) != 1 {
		t.Fatalf("status after download: %+v", st)
	}
	if st.Comment != "piecemeal test" || st.ContentPath != filepath.Join(h.saveDir, "payload") {
		t.Fatalf("metadata fields: %+v", st)
	}
	h.checkContent(s, "payload")
	h.waitFor(hash, "completion time", func(st Torrent) bool { return st.CompletedAt > 0 && st.Downloaded >= 400<<10 })

	if err := h.engine.Stop(ctx, hash); err != nil {
		t.Fatal(err)
	}
	st, _ = h.engine.Get(hash)
	if st.State != StateStopped || !st.Complete || st.Completed != 400<<10 {
		t.Fatalf("status after stop: %+v", st)
	}

	// A restart keeps the record, counters and completion: nothing is
	// downloaded again, there's no peer to download from anyway.
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	st, ok := h.engine.Get(hash)
	if !ok || st.State != StateStopped || !st.Complete || st.Downloaded < 400<<10 || st.Category != "movies" {
		t.Fatalf("status after restart: %+v", st)
	}
	if err := h.engine.Start(ctx, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "seeding after start", func(st Torrent) bool {
		return st.State == StateStalledSeeding || st.State == StateSeeding
	})

	if err := h.engine.Remove(ctx, true, hash); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.engine.Get(hash); ok {
		t.Fatal("torrent still listed after removal")
	}
	if _, err := os.Stat(filepath.Join(h.saveDir, "payload")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("content still on disk after removal with data: %v", err)
	}
}

func TestMagnetFetchesMetadata(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"single.bin": 200 << 10})
	h := newHarness(t)

	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&x.pe=%s", s.Hash, "single", url.QueryEscape(s.Addr))
	hash, err := h.engine.Add(ctx, AddParams{Magnet: magnet})
	if err != nil {
		t.Fatal(err)
	}
	st := h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	if st.Name != "payload" || !st.HasMetadata {
		t.Fatalf("status: %+v", st)
	}
	h.checkContent(s, "payload")

	// The metadata was stored, so a restart doesn't need peers to know the
	// files.
	h.waitFor(hash, "checkpoint", func(Torrent) bool {
		rows, _ := h.store.ListTorrents(ctx)
		return len(rows) == 1 && rows[0].Metainfo != nil
	})
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	files, err := h.engine.Files(hash)
	if err != nil || len(files) != 1 || files[0].Size != 200<<10 {
		t.Fatalf("files after restart: %+v, %v", files, err)
	}
}

func TestSeedGoalStops(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	h := newHarness(t)
	if _, err := h.settings.Set(ctx, map[string]any{"seedingTimeLimit": 0}); err != nil {
		t.Fatal(err)
	}
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "stop at the seeding goal", func(st Torrent) bool { return st.Complete && st.State == StateStopped })
}

func TestConfine(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	e := &Engine{cfg: Config{AllowedRoots: []string{root}}}
	for path, ok := range map[string]bool{
		root:                                   true,
		filepath.Join(root, "a/b"):             true,
		filepath.Join(root, "../x"):            false,
		outside:                                false,
		filepath.Join(root, "escape"):          false,
		filepath.Join(root, "escape/new/path"): false,
		"relative":                             false,
	} {
		_, err := e.confine(path)
		if (err == nil) != ok {
			t.Errorf("confine(%q): %v, want ok=%v", path, err, ok)
		}
	}
}

func TestBusDropsSlowSubscribers(t *testing.T) {
	b := newBus()
	slow := b.Subscribe(1)
	fast := b.Subscribe(10)
	for range 3 {
		b.publish(Event{Type: EventTags})
	}
	n := 0
	for range slow.C {
		n++
	}
	if n != 1 {
		t.Fatalf("slow subscriber got %d events before being dropped, want 1", n)
	}
	if len(fast.C) != 3 {
		t.Fatalf("fast subscriber has %d events, want 3", len(fast.C))
	}
	fast.Close()
}

func TestPiecePriorities(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 200 << 10, "b.bin": 200 << 10})
	h := newHarness(t)
	// No peers: the torrent runs but never gets data, so priorities stay put.
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	m := func() *managed {
		h.engine.mu.Lock()
		defer h.engine.mu.Unlock()
		mm, _ := h.engine.get(hash)
		return mm
	}()
	boosted := func() map[int]types.PiecePriority {
		h.engine.mu.Lock()
		defer h.engine.mu.Unlock()
		return maps.Clone(m.boosted)
	}

	if err := h.engine.SetFirstLastPiece(ctx, true, hash); err != nil {
		t.Fatal(err)
	}
	// 400 KiB in 32 KiB pieces: a.bin is pieces 0-6, b.bin 6-12; piece 6
	// is the last of one and the first of the other.
	if b := boosted(); len(b) != 3 || b[0] != types.PiecePriorityHigh || b[12] != types.PiecePriorityHigh {
		t.Fatalf("first/last on: %v", b)
	}
	// Skipping a file drops its boost.
	if err := h.engine.SetFilePriority(ctx, hash, []int{1}, PrioritySkip); err != nil {
		t.Fatal(err)
	}
	if b := boosted(); len(b) != 2 || b[12] != 0 {
		t.Fatalf("after skipping b.bin: %v", b)
	}
	if err := h.engine.SetFirstLastPiece(ctx, false, hash); err != nil {
		t.Fatal(err)
	}
	if b := boosted(); len(b) != 0 {
		t.Fatalf("first/last off: %v", b)
	}
	// A piece whose completion isn't known yet, or that is being checked,
	// reports priority none; wait for the initial check to finish.
	deadline := time.Now().Add(10 * time.Second)
	for {
		ps := m.t.Piece(0).State()
		if ps.Ok && !ps.Hashing && !ps.QueuedForHash {
			if ps.Priority != types.PiecePriorityNormal {
				t.Fatalf("piece 0 priority after turning first/last off: %v, want normal", ps.Priority)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("piece 0 still being checked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Sequential raises the first missing wanted pieces.
	if err := h.engine.SetSequential(ctx, true, hash); err != nil {
		t.Fatal(err)
	}
	if b := boosted(); b[0] != types.PiecePriorityNext || b[1] != types.PiecePriorityReadahead || b[7] != 0 {
		t.Fatalf("sequential: %v", b)
	}
	if err := h.engine.SetSequential(ctx, false, hash); err != nil {
		t.Fatal(err)
	}
	if b := boosted(); len(b) != 0 {
		t.Fatalf("sequential off: %v", b)
	}
}

func TestRemovedTorrentStaysRemoved(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	h := newHarness(t)
	sub := h.engine.Events().Subscribe(1000)
	defer sub.Close()
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Remove(ctx, false, hash); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.checkpoint(ctx, true); err != nil {
		t.Fatal(err)
	}
	rows, err := h.store.ListTorrents(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows after remove and checkpoint: %v, %v", rows, err)
	}
	for {
		select {
		case ev := <-sub.C:
			if ev.Type == EventTorrentRemoved && ev.Hash == hash {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no removal event")
		}
	}
}

func TestAddRejectsBadTags(t *testing.T) {
	h := newHarness(t)
	_, err := h.engine.Add(context.Background(), AddParams{
		Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Tags: []string{"a,b"},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("tag with a comma: %v, want ErrInvalid", err)
	}
}

func TestDefaultSavePathMustBeAllowed(t *testing.T) {
	h := newHarness(t)
	_, err := h.settings.Set(context.Background(), map[string]any{"defaultSavePath": "/etc"})
	var ve *settings.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("default save path outside the roots: %v, want a ValidationError", err)
	}
}

// TestRestartKeepsPort checks that settings that recreate the client keep
// the peer port, even right after the old client closed it, and with the
// port setting at 0 (random).
func TestRestartKeepsPort(t *testing.T) {
	h := newHarness(t)
	_ = h.engine.Close()
	e, err := New(context.Background(), Config{
		DataDir: h.dataDir, AllowedRoots: []string{h.saveDir}, TickInterval: 50 * time.Millisecond,
		// Offline, but listening on the port the engine asks for.
		ClientConfig: func(tc *torrent.ClientConfig) {
			port := tc.ListenPort
			offline(tc)
			tc.ListenPort = port
		},
	}, h.store, h.settings, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	port := e.Port()
	for i, pex := range []bool{false, true, false} {
		if _, err := h.settings.Set(context.Background(), map[string]any{"pex": pex}); err != nil {
			t.Fatal(err)
		}
		if got := e.Port(); got != port {
			t.Fatalf("restart %d: port %d, want %d", i, got, port)
		}
	}
}

// TestLostPieceCompletion checks that torrents whose piece completion
// database is gone, as after restoring a backup on a new machine, are
// checked against their data instead of downloaded again.
func TestLostPieceCompletion(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 200 << 10})
	h := newHarness(t)
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.dataDir, ".torrent.bolt.db")); err != nil {
		t.Fatal(err)
	}
	h.start()
	// No peers this time: completion can only come from the data on disk.
	h.waitFor(hash, "the data to be checked", func(st Torrent) bool { return st.Complete })
}

func TestCheckRoots(t *testing.T) {
	parent := t.TempDir()
	data := filepath.Join(parent, "data")
	downloads := filepath.Join(parent, "downloads")
	link := filepath.Join(t.TempDir(), "srv")
	for _, d := range []string{data, downloads} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(parent, link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		roots []string
		ok    bool
	}{
		{"apart", []string{downloads}, true},
		{"parent", []string{downloads, parent}, false},
		{"the data dir itself", []string{data}, false},
		{"inside the data dir", []string{filepath.Join(data, "x")}, false},
		{"through a symlink", []string{link}, false},
	} {
		if err := CheckRoots(c.roots, data, ""); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// TestMagnetInfoBeforeAttach adds a magnet whose metadata anacrolix already
// has when the engine attaches it, as when a fast peer sends it before
// attachLocked looks: the storage still opens, the torrent file is saved,
// and the data downloads.
func TestMagnetInfoBeforeAttach(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"single.bin": 200 << 10})
	h := newHarness(t)
	mi, err := metainfo.Load(bytes.NewReader(s.Metainfo))
	if err != nil {
		t.Fatal(err)
	}
	h.engine.mu.Lock()
	h.engine.client.AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash: mi.HashInfoBytes(), InfoBytes: mi.InfoBytes, Storage: h.engine.storage, DeferStorageOpen: true,
	})
	h.engine.mu.Unlock()

	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=single&x.pe=%s", s.Hash, url.QueryEscape(s.Addr))
	hash, err := h.engine.Add(ctx, AddParams{Magnet: magnet})
	if err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete })
	h.checkContent(s, "payload")
	h.waitFor(hash, "the torrent file saved", func(Torrent) bool {
		rows, _ := h.store.ListTorrents(ctx)
		return len(rows) == 1 && rows[0].Metainfo != nil
	})
}
