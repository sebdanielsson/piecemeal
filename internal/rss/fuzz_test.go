package rss

import (
	"strings"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/settings"
)

var fuzzTitles = []string{
	"Show.S01E02.1080p.WEB.x264-GRP", "Show 1x08 720p", "Daily.Show.2026.11.17", "Show.S01.Complete", "",
}

// FuzzParse checks the feed parser never panics on any document.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`<rss><channel><title>t</title><item><title>a</title><guid>g</guid></item></channel></rss>`,
		`<feed xmlns="http://www.w3.org/2005/Atom"><entry><title type="xhtml"><div>x</div></title></entry></feed>`,
		`<rdf:RDF><item><link>magnet:?xt=urn:btih:00</link></item></rdf:RDF>`,
		`<?xml version="1.0" encoding="ISO-8859-1"?><rss><channel><title>Caf` + "\xe9" + `</title></channel></rss>`,
		"<rss>&lt;&#0;&unknown;</rss>",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, doc string) {
		feed, err := Parse(strings.NewReader(doc))
		if err != nil {
			return
		}
		for _, a := range feed.Articles {
			if a.ID == "" && (a.Title != "" || a.TorrentURL != "") {
				t.Fatalf("article without an ID: %+v", a)
			}
		}
	})
}

// FuzzRuleText checks wildcard and regular expression matching never
// panic, whatever a user writes.
func FuzzRuleText(f *testing.F) {
	for _, s := range []string{"Show*1080p", "[!]", "[z-a]", "a|b c|", `(?<=x)y`, "[[:alpha:]]", `\`, "((((((((((x"} {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(_ *testing.T, expr string, useRegex bool) {
		m := compileText(expr, useRegex)
		if m.empty || m.invalid {
			return
		}
		for _, title := range fuzzTitles {
			m.match(title)
		}
	})
}

// FuzzEpisodeFilter checks parsing and matching episode filters never
// panic.
func FuzzEpisodeFilter(f *testing.F) {
	for _, s := range []string{"1x2;8-15;5;30-;", "0x1-;", "1x99999999999999999999;", "1x2-1;", ";;;", "1x01;1x03-4;"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, filter string) {
		ef, ok := parseEpisodeFilter(filter)
		if !ok {
			return
		}
		for _, title := range fuzzTitles {
			ef.matches(title)
		}
	})
}

// FuzzSmartFilter checks user smart episode filters never panic.
func FuzzSmartFilter(f *testing.F) {
	f.Add(settings.DefaultSmartEpisodeFilters, "Show.S01E02")
	f.Add(`(\d+)`, "Show 2026")
	f.Add(`((a)(b)(c))`, "abc")
	f.Add(`(?<n>\d+)x(\d+)`, "1x99999999999999999999999")
	f.Fuzz(func(_ *testing.T, patterns, title string) {
		sf, err := compileSmartFilter(patterns)
		if err != nil {
			return
		}
		sf.episodeID(title)
	})
}
