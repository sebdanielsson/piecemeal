package qbit

import (
	"encoding/json"
	"fmt"
	"testing"
)

func benchTorrents(n int, rate int64) map[string]fields {
	out := map[string]fields{}
	for i := range n {
		t := Torrent{
			Name: fmt.Sprintf("torrent %d", i), State: "uploading", Size: 1 << 20, Progress: 1, Upspeed: rate, Tags: "a, b",
			SavePath: "/downloads", ContentPath: fmt.Sprintf("/downloads/t%d", i), Ratio: 1.5, MagnetURI: "magnet:?xt=urn:btih:abc",
		}
		f := structFields(t)
		delete(f, "hash")
		out[fmt.Sprintf("%040d", i)] = f
	}
	return out
}

func BenchmarkSnapshotFields(b *testing.B) {
	t := Torrent{Name: "torrent", State: "uploading", Size: 1 << 20, Progress: 1}
	for b.Loop() {
		for range 1000 {
			_ = structFields(t)
		}
	}
}

// structFields must give what encoding/json would write.
func TestStructFieldsMatchesJSON(t *testing.T) {
	v := Torrent{Name: "n", State: "uploading", Size: 1 << 40, Progress: 0.5, Upspeed: 7, Tags: "a, b", Ratio: 1.25, AddedOn: 1_700_000_000}
	got, err := json.Marshal(structFields(v))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(toFields(v))
	if string(got) != string(want) {
		t.Fatalf("structFields:\n%s\ntoFields:\n%s", got, want)
	}
}

func BenchmarkDiffObjects(b *testing.B) {
	prev, next := benchTorrents(1000, 0), benchTorrents(1000, 1)
	for b.Loop() {
		_, _ = diffObjects(prev, next, false)
	}
}
