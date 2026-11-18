package rss

import (
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/settings"
)

func TestWildcards(t *testing.T) {
	const title = "Probe.Show.S01E02.1080p.WEB.x264-GRP"
	for expr, want := range map[string]bool{
		"":                       true,
		"probe.show.s01e02":      true,
		"Show*":                  true,
		"Probe.Show":             true,
		"Probe Show":             true, // two terms, both present
		"Probe\\.Show":           false,
		"\"Probe Show\"":         false,
		"1080p S01E02":           true,
		"S01E02 1080p":           true,
		"S01E02 720p|S02E01":     false,
		"S01E01 | S01E02":        true,
		"S01E0?":                 true,
		"[PS]robe":               true,
		"[!P]robe":               false,
		"720p":                   false,
		"720p|":                  true, // an empty alternative matches all
		"Probe*S01E02*GRP":       true,
		"Probe*S01E03":           false,
		"x264-GRP":               true,
		"[unterminated":          false,
		"Probe.Show.S01E02.1080": true,
	} {
		m := compileText(expr, false)
		got := m.empty || (!m.invalid && m.match(title))
		if got != want {
			t.Errorf("%q: %v, want %v", expr, got, want)
		}
	}
}

func TestRegexMode(t *testing.T) {
	const title = "Probe.Show.S01E02.1080p.WEB.x264-GRP"
	for expr, want := range map[string]bool{
		`s01e02`:             true,
		`^Probe\.Show\.S01`:  true,
		`S01E02 1080p`:       false,
		`S01E01|`:            true,
		`^(?!.*720p).*1080p`: true, // lookahead, which RE2 lacks
		`(?-i)probe`:         false,
		`(Probe`:             false, // invalid
	} {
		m := compileText(expr, true)
		got := !m.invalid && m.match(title)
		if got != want {
			t.Errorf("%q: %v, want %v", expr, got, want)
		}
	}
}

func TestEpisodeFilter(t *testing.T) {
	titles := map[string]string{
		"s1e1": "Show.S01E01.1080p", "s1e2": "Show.S01E02.1080p", "s1e3": "Show.S01E03",
		"s1e4": "Show.S01E04E05", "s1e8": "Show 1x08 720p", "s2e1": "Show.S02E01", "s3e1": "Show.S03E01",
		"pack": "Show.S01.Complete",
	}
	for filter, want := range map[string][]string{
		"1x2;":            {"s1e2"},
		"1X02;":           {"s1e2"},
		"01x002;":         {"s1e2"},
		"1x2;8-15;5;30-;": {"s1e2", "s1e8", "s2e1", "s3e1"}, // 30- runs into later seasons
		"1x3-;":           {"s1e3", "s1e4", "s1e8", "s2e1", "s3e1"},
		"1x4;":            {"s1e4"},
		"1x5;":            nil, // only the first episode of S01E04E05 counts
		"1x01;1x04;":      {"s1e1"},
		"1x01;1x03-4;":    {"s1e1", "s1e2", "s1e3", "s1e4"},
		"2x1;1x1;":        {"s2e1"},
		"1x02;;":          {"s1e2"},
		"1x2-1;":          nil,
		"1x2":             nil, // no trailing ;
		" 1x2;":           nil, //nolint:gocritic // leading whitespace makes it invalid
		"S01E02;":         nil,
		"1x;":             nil,
		"0x01;":           {"s1e1", "s2e1", "s3e1"},
	} {
		f, ok := parseEpisodeFilter(filter)
		var got []string
		for _, key := range []string{"s1e1", "s1e2", "s1e3", "s1e4", "s1e8", "s2e1", "s3e1", "pack"} {
			if ok && f.matches(titles[key]) {
				got = append(got, key)
			}
		}
		if len(got) != len(want) {
			t.Errorf("%q: %v, want %v", filter, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%q: %v, want %v", filter, got, want)
				break
			}
		}
	}
}

func TestSmartFilterIDs(t *testing.T) {
	f, err := compileSmartFilter(settings.DefaultSmartEpisodeFilters)
	if err != nil {
		t.Fatal(err)
	}
	for title, want := range map[string]string{
		"Show.S01E02.1080p":          "1x2",
		"show s10e100":               "10x100",
		"Show 1x08 720p":             "1x8",
		"Daily.Show.2026.11.17.WEB":  "2026.11.17",
		"Daily Show 17.11.2026":      "17.11.2026",
		"Daily_Show_2026-11-18_x264": "2026-11-18",
		"Show.S01.Complete":          "",
		"Show.S01E04E05":             "", // 4E isn't a word boundary
		"ubuntu-24.04-desktop.iso":   "",
	} {
		got, ok := f.episodeID(title)
		if (want == "") == ok || got != want {
			t.Errorf("%q: %q %v, want %q", title, got, ok, want)
		}
	}
	if _, err := compileSmartFilter("(unclosed"); err == nil {
		t.Error("invalid smart filter compiled")
	}
	empty, _ := compileSmartFilter("")
	if _, ok := empty.episodeID("Show.S01E02"); ok {
		t.Error("no filters recognised an episode")
	}
}
