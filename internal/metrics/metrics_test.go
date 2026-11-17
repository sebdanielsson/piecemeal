package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

type fakeSource struct{}

func (fakeSource) List() []engine.Torrent {
	return []engine.Torrent{{Hash: "aa", Name: "x", State: engine.StateSeeding, Uploaded: 42}}
}

func (fakeSource) Transfer() engine.TransferStatus {
	return engine.TransferStatus{Port: 51413, FreeSpace: -1}
}

func TestCollector(t *testing.T) {
	c := NewCollector(fakeSource{})
	want := `
# HELP piecemeal_torrent_uploaded_bytes_total Payload bytes uploaded, all time.
# TYPE piecemeal_torrent_uploaded_bytes_total counter
piecemeal_torrent_uploaded_bytes_total{hash="aa",name="x"} 42
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want), "piecemeal_torrent_uploaded_bytes_total"); err != nil {
		t.Fatal(err)
	}
	if n := testutil.CollectAndCount(c, "piecemeal_torrents"); n != 10 {
		t.Fatalf("piecemeal_torrents has %d series, want one per state", n)
	}
}
