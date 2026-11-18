package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

// The test data comes from Transmission 4.1.4 and qBittorrent 5.3.0, each
// with two torrents: Show.S01 (three files, the second renamed to
// S01E02.mkv and the third skipped) and single.iso (stopped).
const (
	showHash   = "d2b934abbf003c241995ce32968962393cba887a"
	singleHash = "563b648b7f48239dec0503e9a79c818f8a8d15b5"
)

func importedByHash(t *testing.T, items []Imported) map[string]Imported {
	t.Helper()
	out := map[string]Imported{}
	for _, it := range items {
		mi, err := metainfo.LoadFromFile(strings.TrimSuffix(strings.TrimSuffix(it.Source, ".resume"), ".fastresume") + ".torrent")
		if err != nil {
			// Transmission keeps torrents and resume files apart.
			mi, err = metainfo.LoadFromFile(filepath.Join(filepath.Dir(filepath.Dir(it.Source)), "torrents",
				strings.TrimSuffix(filepath.Base(it.Source), ".resume")+".torrent"))
		}
		if err != nil {
			t.Fatal(err)
		}
		out[mi.HashInfoBytes().HexString()] = it
	}
	return out
}

func TestReadTransmission(t *testing.T) {
	items, errs := ReadTransmission("testdata/import/transmission")
	if len(errs) > 0 || len(items) != 2 {
		t.Fatalf("items %d, errors %v", len(items), errs)
	}
	byHash := importedByHash(t, items)
	show, single := byHash[showHash], byHash[singleHash]
	if show.SavePath != "/downloads" || show.Stopped || !slices.Equal(show.Tags, []string{"tv", "hd"}) || show.Category != "tv" ||
		!slices.Equal(show.Skip, []bool{false, false, true}) || show.Paths[1] != "Show.S01/S01E02.mkv" ||
		!show.Complete || show.AddedAt.IsZero() || show.RatioLimit != -2 || show.InactiveSeedingTimeLimit != -2 {
		t.Fatalf("show: %+v", show)
	}
	if !single.Stopped || slices.Contains(single.Skip, true) {
		t.Fatalf("single: %+v", single)
	}
	if _, errs := ReadTransmission(t.TempDir()); len(errs) == 0 {
		t.Fatal("an empty directory read without an error")
	}
}

func TestReadQBittorrent(t *testing.T) {
	// The data directory works as well as BT_backup itself.
	items, errs := ReadQBittorrent("testdata/import/qbittorrent")
	if len(errs) > 0 || len(items) != 2 {
		t.Fatalf("items %d, errors %v", len(items), errs)
	}
	byHash := importedByHash(t, items)
	show, single := byHash[showHash], byHash[singleHash]
	if show.SavePath != "/downloads" || show.Category != "tv" || !slices.Equal(show.Tags, []string{"hd", "x"}) ||
		!slices.Equal(show.Skip, []bool{false, false, true}) || show.Paths[1] != "Show.S01/S01E02.mkv" ||
		show.RatioLimit != 1.5 || show.SeedingTimeLimit != -2 || !show.Complete ||
		len(show.Trackers) != 1 || show.Trackers[0][0] != "udp://tracker.example:1337" {
		t.Fatalf("show: %+v", show)
	}
	if !single.Stopped || single.RatioLimit != -2 || single.Complete || single.Trackers != nil {
		t.Fatalf("single: %+v", single)
	}
}

func TestImport(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := store.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	items, _ := ReadQBittorrent("testdata/import/qbittorrent/BT_backup")
	root := t.TempDir()
	for i := range items {
		items[i].SavePath = root // as --map would
	}

	// A dry run writes nothing.
	res := Import(ctx, st, dataDir, items, ImportOptions{AllowedRoots: []string{root}, DryRun: true})
	if rows, _ := st.ListTorrents(ctx); len(rows) != 0 || len(res) != 2 || res[0].Err != nil {
		t.Fatalf("dry run: %+v, %d rows", res, len(rows))
	}
	// Outside the allowed roots.
	res = Import(ctx, st, dataDir, items[:1], ImportOptions{AllowedRoots: []string{t.TempDir()}})
	if res[0].Err == nil || !strings.Contains(res[0].Err.Error(), "--map") {
		t.Fatalf("outside the roots: %+v", res)
	}

	res = Import(ctx, st, dataDir, items, ImportOptions{AllowedRoots: []string{root}, Trust: true})
	for _, r := range res {
		if r.Err != nil || r.Skipped != "" {
			t.Fatalf("import: %+v", r)
		}
	}
	rows, err := st.ListTorrents(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows: %d %v", len(rows), err)
	}
	var show store.Torrent
	for _, r := range rows {
		if r.InfoHash == showHash {
			show = r
		}
	}
	var renames map[string]string
	var prio []Priority
	_ = json.Unmarshal([]byte(show.FilePaths), &renames)
	_ = json.Unmarshal([]byte(show.FilePriorities), &prio)
	if show.Category != "tv" || show.RatioLimit != 1.5 || renames["Show.S01/e02.mkv"] != "Show.S01/S01E02.mkv" ||
		len(renames) != 1 || !slices.Equal(prio, []Priority{1, 1, 0}) || show.SavePath != root {
		t.Fatalf("show: %+v renames %v priorities %v", show, renames, prio)
	}
	tags, _ := st.ListTorrentTags(ctx)
	cats, _ := st.ListCategories(ctx)
	if len(tags) != 2 || len(cats) != 1 {
		t.Fatalf("tags %v categories %v", tags, cats)
	}

	// Trusted: the complete torrent's pieces outside the skipped file are
	// marked; the stopped, incomplete one is left to be checked.
	pc, err := storage.NewBoltPieceCompletion(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	var ih metainfo.Hash
	_ = ih.FromHexString(showHash)
	c, err := pc.Get(metainfo.PieceKey{InfoHash: ih, Index: 0})
	_ = pc.Close()
	if err != nil || !c.Ok || !c.Complete {
		t.Fatalf("first piece: %+v %v", c, err)
	}

	// Importing again skips what's there.
	res = Import(ctx, st, dataDir, items, ImportOptions{AllowedRoots: []string{root}})
	if res[0].Skipped == "" || res[1].Skipped == "" {
		t.Fatalf("second import: %+v", res)
	}
}
