package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
)

// writeContent puts a seeder's files under dir, as a torrent's data.
func writeContent(t *testing.T, s *enginetest.Seeder, dir string, corrupt bool) {
	t.Helper()
	for p, b := range s.Content {
		full := filepath.Join(dir, s.Name, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if corrupt {
			b = append([]byte{b[0] ^ 0xff}, b[1:]...)
		}
		if err := os.WriteFile(full, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// categoryAt is a new category with a save path.
func categoryAt(name, savePath string) Category {
	c := NewCategory(name)
	c.SavePath = savePath
	return c
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func seeding(st Torrent) bool {
	return st.State == StateSeeding || st.State == StateStalledSeeding
}

// TestIncompleteFolderDownload downloads into the incomplete folder, then
// the data moves to the save path once complete. A restart in the middle
// carries on in the incomplete folder.
func TestIncompleteFolderDownload(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 3 << 20, "sub/b.bin": 1 << 20})
	h := newHarness(t, withSetting("incompletePathEnabled", true), withSetting("downloadLimit", 1<<20))
	incomplete := filepath.Join(h.saveDir, "temp")

	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := h.engine.Get(hash)
	if st.SavePath != h.saveDir || st.IncompletePath != incomplete || st.ContentPath != filepath.Join(incomplete, "payload") {
		t.Fatalf("paths after adding: save %q, incomplete %q, content %q", st.SavePath, st.IncompletePath, st.ContentPath)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "data in the incomplete folder", func(st Torrent) bool {
		return st.Completed > 0 && exists(filepath.Join(incomplete, "payload", "a.bin"))
	})
	if exists(filepath.Join(h.saveDir, "payload")) {
		t.Fatal("data in the save path while incomplete")
	}

	// Restart halfway: the download carries on where it was.
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := h.store.ListTorrents(ctx)
	if err != nil || len(rows) != 1 || !rows[0].InIncompletePath || rows[0].IncompletePath != incomplete {
		t.Fatalf("stored record: %+v, %v", rows, err)
	}
	if _, err := h.settings.Set(ctx, map[string]any{"downloadLimit": 0}); err != nil {
		t.Fatal(err)
	}
	h.start()
	if st, _ := h.engine.Get(hash); st.Complete || st.ContentPath != filepath.Join(incomplete, "payload") {
		t.Fatalf("after restart: %+v", st)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	st = h.waitFor(hash, "complete in the save path", func(st Torrent) bool {
		return st.Complete && seeding(st) && st.ContentPath == filepath.Join(h.saveDir, "payload")
	})
	if st.IncompletePath != incomplete {
		t.Fatalf("incomplete path after completion: %q", st.IncompletePath)
	}
	h.checkContent(s, "payload")
	if exists(filepath.Join(incomplete, "payload")) {
		t.Fatal("data left in the incomplete folder")
	}
	h.waitFor(hash, "the record saved", func(Torrent) bool {
		rows, _ := h.store.ListTorrents(ctx)
		return len(rows) == 1 && !rows[0].InIncompletePath
	})
}

// TestIncompleteFolderCheck checks data that's already in the incomplete
// folder there, through the check queue, then moves it once complete.
// Data that's in the save path instead stays there.
func TestIncompleteFolderCheck(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, slowHashing(time.Millisecond), withSetting("incompletePathEnabled", true))
	incomplete := filepath.Join(h.saveDir, "temp")
	inIncomplete := enginetest.NewSeeder(t, "one", map[string]int{"data.bin": 1 << 20})
	inSave := enginetest.NewSeeder(t, "two", map[string]int{"data.bin": 1 << 20})
	writeContent(t, inIncomplete, incomplete, false)
	writeContent(t, inSave, h.saveDir, false)

	one, err := h.engine.Add(ctx, AddParams{Metainfo: inIncomplete.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	two, err := h.engine.Add(ctx, AddParams{Metainfo: inSave.Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{one, two} {
		if st, _ := h.engine.Get(hash); st.State != StateChecking && st.State != StateQueuedForChecking {
			t.Fatalf("%s right after adding: %s, want a check", st.Name, st.State)
		}
	}
	h.waitFor(one, "one checked and moved", func(st Torrent) bool {
		return st.Complete && seeding(st) && st.ContentPath == filepath.Join(h.saveDir, "one", "data.bin")
	})
	st := h.waitFor(two, "two checked", func(st Torrent) bool { return st.Complete && seeding(st) })
	if st.ContentPath != filepath.Join(h.saveDir, "two", "data.bin") || st.IncompletePath != incomplete {
		t.Fatalf("two: content %q, incomplete %q", st.ContentPath, st.IncompletePath)
	}
	if exists(filepath.Join(incomplete, "one")) || !exists(filepath.Join(h.saveDir, "one", "data.bin")) {
		t.Fatal("one's data didn't move to the save path")
	}
}

// TestSetIncompletePath moves an incomplete torrent's data between
// incomplete folders and to the save path, and keeps it in the incomplete
// folder when the save path changes.
func TestSetIncompletePath(t *testing.T) {
	ctx := context.Background()
	s := enginetest.NewSeeder(t, "payload", map[string]int{"data.bin": 256 << 10})
	h := newHarness(t)
	first := filepath.Join(h.saveDir, "inc1")
	second := filepath.Join(h.saveDir, "inc2")
	// The first piece is corrupt, so the torrent stays incomplete until it
	// gets a peer.
	writeContent(t, s, first, true)
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo, IncompletePath: &first})
	if err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "checked", func(st Torrent) bool { return st.State == StateStalledDownloading && st.Completed > 0 })

	if err := h.engine.SetIncompletePath(ctx, "/etc", hash); !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("incomplete folder outside the roots: %v", err)
	}
	if err := h.engine.SetIncompletePath(ctx, second, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "moved to the second folder", func(st Torrent) bool {
		return st.IncompletePath == second && st.State == StateStalledDownloading &&
			st.ContentPath == filepath.Join(second, "payload", "data.bin")
	})
	if !exists(filepath.Join(second, "payload", "data.bin")) || exists(filepath.Join(first, "payload")) {
		t.Fatal("data not in the second folder")
	}

	// A new save path: the data stays in the incomplete folder.
	dest := filepath.Join(h.saveDir, "done")
	if err := h.engine.Move(ctx, hash, dest); err != nil {
		t.Fatal(err)
	}
	st, _ := h.engine.Get(hash)
	if st.SavePath != dest || st.ContentPath != filepath.Join(second, "payload", "data.bin") || st.State == StateMoving {
		t.Fatalf("after a new save path: %+v", st)
	}

	// No incomplete folder: the data goes to the save path, and completes
	// there.
	if err := h.engine.SetIncompletePath(ctx, "", hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "moved to the save path", func(st Torrent) bool {
		return st.IncompletePath == "" && st.State == StateStalledDownloading && st.ContentPath == filepath.Join(dest, "payload", "data.bin")
	})
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "complete", func(st Torrent) bool { return st.Complete && seeding(st) })
	h.checkContentIn(&seeder{s}, dest)
	if exists(filepath.Join(second, "payload")) {
		t.Fatal("data left in the incomplete folder")
	}

	// A complete torrent stays in its save path.
	if err := h.engine.SetIncompletePath(ctx, first, hash); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(hash); st.IncompletePath != first || st.ContentPath != filepath.Join(dest, "payload", "data.bin") {
		t.Fatalf("complete torrent with a new incomplete folder: %+v", st)
	}
}

// TestAutoManaged follows an automatically managed torrent through
// category changes: its data moves to the new category's path, or, with
// relocateOnCategoryChange off, it switches to manual management and stays.
func TestAutoManaged(t *testing.T) {
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"a.bin": 200 << 10, "sub/b.bin": 100 << 10})
	h := newHarness(t)
	movies := filepath.Join(h.saveDir, "films")
	if err := h.engine.PutCategory(ctx, categoryAt("movies", movies), true); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.PutCategory(ctx, NewCategory("tv"), true); err != nil {
		t.Fatal(err)
	}
	auto := true
	other := filepath.Join(h.saveDir, "ignored")
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: s.Metainfo, Category: "movies", AutoManaged: &auto, SavePath: other})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(hash); !st.AutoManaged || st.SavePath != movies {
		t.Fatalf("added: auto %v, save path %q", st.AutoManaged, st.SavePath)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "download", func(st Torrent) bool { return st.Complete && seeding(st) })
	h.checkContentIn(s, movies)

	// A category without a save path: a folder named after it.
	if err := h.engine.SetCategory(ctx, "tv", hash); err != nil {
		t.Fatal(err)
	}
	tv := filepath.Join(h.saveDir, "tv")
	h.waitFor(hash, "moved to tv", func(st Torrent) bool { return st.SavePath == tv && seeding(st) })
	h.checkContentIn(s, tv)
	if exists(filepath.Join(movies, "payload")) {
		t.Fatal("data left in the old category's folder")
	}

	// Without relocation it switches to manual management and stays.
	if _, err := h.settings.Set(ctx, map[string]any{"relocateOnCategoryChange": false}); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.SetCategory(ctx, "movies", hash); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(hash); st.AutoManaged || st.SavePath != tv || st.Category != "movies" {
		t.Fatalf("after a category change without relocation: %+v", st)
	}

	// Turning it on again moves it to its category's path; off keeps it.
	if err := h.engine.SetAutoManaged(ctx, true, hash); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "moved back to movies", func(st Torrent) bool { return st.AutoManaged && st.SavePath == movies && seeding(st) })
	h.checkContentIn(s, movies)
	if err := h.engine.SetAutoManaged(ctx, false, hash); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(hash); st.AutoManaged || st.SavePath != movies {
		t.Fatalf("after turning it off: %+v", st)
	}

	// Moving it by hand turns it off.
	if err := h.engine.SetAutoManaged(ctx, true, hash); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Move(ctx, hash, other); err != nil {
		t.Fatal(err)
	}
	st := h.waitFor(hash, "moved by hand", func(st Torrent) bool { return st.SavePath == other && seeding(st) })
	if st.AutoManaged {
		t.Fatal("still automatically managed after a move")
	}
}

// TestAutoManagedRelocation changes the paths automatically managed
// torrents follow: a category's save path and the default save path. With
// the relocation settings on their data moves; off, they switch to manual
// management.
func TestAutoManagedRelocation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, withSetting("autoManagement", true))
	inCat := enginetest.NewSeeder(t, "one", map[string]int{"data.bin": 64 << 10})
	noCat := enginetest.NewSeeder(t, "two", map[string]int{"data.bin": 64 << 10})
	if err := h.engine.PutCategory(ctx, categoryAt("c", "rel"), true); err != nil {
		t.Fatal(err)
	}
	// Data in place, added with skip checking: complete at once.
	writeContent(t, inCat, filepath.Join(h.saveDir, "rel"), false)
	writeContent(t, noCat, h.saveDir, false)
	one, err := h.engine.Add(ctx, AddParams{Metainfo: inCat.Metainfo, Category: "c", SkipChecking: true})
	if err != nil {
		t.Fatal(err)
	}
	two, err := h.engine.Add(ctx, AddParams{Metainfo: noCat.Metainfo, SkipChecking: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{one, two} {
		h.waitFor(hash, "seeding", func(st Torrent) bool { return st.AutoManaged && st.Complete && seeding(st) })
	}

	// The category's path changes; without relocation, its torrent turns
	// manual and stays; the other isn't in it.
	if err := h.engine.PutCategory(ctx, categoryAt("c", "rel2"), false); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(one); st.AutoManaged || st.SavePath != filepath.Join(h.saveDir, "rel") {
		t.Fatalf("one after the category's path changed: %+v", st)
	}
	if st, _ := h.engine.Get(two); !st.AutoManaged {
		t.Fatal("two isn't in the category but turned manual")
	}
	// With relocation, it moves.
	if err := h.engine.SetAutoManaged(ctx, true, one); err != nil {
		t.Fatal(err)
	}
	h.waitFor(one, "one in rel2", func(st Torrent) bool { return st.SavePath == filepath.Join(h.saveDir, "rel2") && seeding(st) })
	if _, err := h.settings.Set(ctx, map[string]any{"relocateOnCategoryPathChange": true}); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.PutCategory(ctx, categoryAt("c", filepath.Join(h.saveDir, "abs")), false); err != nil {
		t.Fatal(err)
	}
	h.waitFor(one, "one in abs", func(st Torrent) bool {
		return st.AutoManaged && st.SavePath == filepath.Join(h.saveDir, "abs") && seeding(st)
	})

	// The default save path changes: with relocation on, the torrent
	// without a category follows it; the category's path is absolute now,
	// so its torrent stays.
	newDefault := filepath.Join(h.saveDir, "new")
	if _, err := h.settings.Set(ctx, map[string]any{"relocateOnDefaultPathChange": true, "defaultSavePath": newDefault}); err != nil {
		t.Fatal(err)
	}
	h.waitFor(two, "two in the new default", func(st Torrent) bool { return st.AutoManaged && st.SavePath == newDefault && seeding(st) })
	if st, _ := h.engine.Get(one); !st.AutoManaged || st.SavePath != filepath.Join(h.saveDir, "abs") {
		t.Fatalf("one with an absolute category path after the default changed: %+v", st)
	}
	if !exists(filepath.Join(newDefault, "two", "data.bin")) {
		t.Fatal("two's data didn't move")
	}
	// Without relocation, it turns manual.
	if _, err := h.settings.Set(ctx, map[string]any{"relocateOnDefaultPathChange": false, "defaultSavePath": h.saveDir}); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(two); st.AutoManaged || st.SavePath != newDefault {
		t.Fatalf("two after the default changed without relocation: %+v", st)
	}
}
