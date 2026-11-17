package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

func TestEnvName(t *testing.T) {
	for in, want := range map[string]string{
		"downloadLimit":            "PIECEMEAL_DOWNLOAD_LIMIT",
		"dht":                      "PIECEMEAL_DHT",
		"maxConnectionsPerTorrent": "PIECEMEAL_MAX_CONNECTIONS_PER_TORRENT",
	} {
		if got := EnvName(in); got != want {
			t.Errorf("EnvName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadUpdateAndEnvOverrides(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	env := []string{"PIECEMEAL_UPLOAD_LIMIT=1000", "PIECEMEAL_DHT=false", "UNRELATED=1"}
	m, err := Load(ctx, st, Defaults("/downloads"), env)
	if err != nil {
		t.Fatal(err)
	}
	if s := m.Get(); s.UploadLimit != 1000 || s.DHT || s.DefaultSavePath != "/downloads" {
		t.Fatalf("loaded %+v", s)
	}
	if l := m.Locked(); len(l) != 2 || l["uploadLimit"] != "PIECEMEAL_UPLOAD_LIMIT" {
		t.Fatalf("locked %v", l)
	}

	var notified bool
	m.Subscribe(func(old, cur Settings) { notified = old.DownloadLimit == 0 && cur.DownloadLimit == 500 })
	if _, err := m.Update(ctx, json.RawMessage(`{"downloadLimit": 500}`)); err != nil {
		t.Fatal(err)
	}
	if !notified {
		t.Fatal("subscriber not notified")
	}

	for _, patch := range []string{
		`{"uploadLimit": 1}`,  // pinned
		`{"nope": 1}`,         // unknown
		`{"encryption": "x"}`, // invalid
		`[1]`,                 // not an object
	} {
		_, err := m.Update(ctx, json.RawMessage(patch))
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("Update(%s): %v, want a ValidationError", patch, err)
		}
	}

	// Stored values survive a reload; the env still wins.
	m2, err := Load(ctx, st, Defaults("/downloads"), []string{"PIECEMEAL_UPLOAD_LIMIT=7"})
	if err != nil {
		t.Fatal(err)
	}
	if s := m2.Get(); s.DownloadLimit != 500 || s.UploadLimit != 7 || !s.DHT {
		t.Fatalf("reloaded %+v", s)
	}
}

func TestBadEnvValue(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := Load(ctx, st, Defaults("/d"), []string{"PIECEMEAL_DHT=maybe"}); err == nil {
		t.Fatal("no error for a bad boolean")
	}
}

func TestEmptyEnvIsUnset(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := Load(ctx, st, Defaults("/d"), []string{"PIECEMEAL_DOWNLOAD_LIMIT="})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Locked()) != 0 {
		t.Fatalf("locked: %v", m.Locked())
	}
}
