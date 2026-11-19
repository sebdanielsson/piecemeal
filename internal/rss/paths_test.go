package rss

import (
	"encoding/json"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// TestApplyPaths maps a rule's qBittorrent path options onto the torrents
// it adds, as qBittorrent's torrents/add takes them.
func TestApplyPaths(t *testing.T) {
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	for _, tc := range []struct {
		extra      string
		auto       string
		incomplete string
	}{
		{`{}`, "<nil>", "<nil>"},
		{`{"use_auto_tmm":true}`, "true", "<nil>"},
		{`{"use_auto_tmm":false,"use_download_path":false,"download_path":"/x"}`, "false", ""},
		{`{"download_path":"/x"}`, "<nil>", "/x"},
		{`{"use_download_path":true,"download_path":""}`, "<nil>", "/global"},
		{`{"use_download_path":true,"download_path":"rel"}`, "<nil>", "rel"},
	} {
		var o AddOptions
		if err := json.Unmarshal([]byte(tc.extra), &o.Extra); err != nil {
			t.Fatal(err)
		}
		var p engine.AddParams
		o.applyPaths(&p, "/global")
		auto := "<nil>"
		if p.AutoManaged != nil {
			auto = map[bool]string{true: "true", false: "false"}[*p.AutoManaged]
		}
		if auto != tc.auto || str(p.IncompletePath) != tc.incomplete {
			t.Errorf("%s: auto %s, incomplete %q; want %s, %q", tc.extra, auto, str(p.IncompletePath), tc.auto, tc.incomplete)
		}
	}
}
