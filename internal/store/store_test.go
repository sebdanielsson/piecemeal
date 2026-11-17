package store

import (
	"context"
	"testing"
)

func TestOpenMigratesAndQueries(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.UpsertCategory(ctx, UpsertCategoryParams{Name: "movies", SavePath: "/downloads/movies"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertTorrent(ctx, UpsertTorrentParams{InfoHash: "aa", Name: "x", SavePath: "/d", Category: "movies", AddedAt: 1, Stopped: true, RatioLimit: -2}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTag(ctx, "hd"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTorrentTag(ctx, AddTorrentTagParams{InfoHash: "aa", Tag: "hd"}); err != nil {
		t.Fatal(err)
	}

	// Deleting a tag removes it from torrents (foreign keys are on).
	if _, err := s.DeleteTag(ctx, "hd"); err != nil {
		t.Fatal(err)
	}
	tags, err := s.ListTorrentTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("torrent tags after deleting the tag: %v", tags)
	}

	torrents, err := s.ListTorrents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(torrents) != 1 || !torrents[0].Stopped || torrents[0].RatioLimit != -2 {
		t.Fatalf("torrents: %+v", torrents)
	}
}

func TestOpenMemory(t *testing.T) {
	s, err := OpenMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.ListSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
}
