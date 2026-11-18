package rss

import (
	"strings"
	"testing"
	"time"
)

func TestParseRSS(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torrent="http://xmlns.ezrss.it/0.1/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<channel>
  <title>Example tracker</title>
  <item>
    <title>Show.S01E02.1080p</title>
    <link>https://tracker.example/details/2</link>
    <guid isPermaLink="false">id-2</guid>
    <pubDate>Tue, 17 Nov 2026 10:00:00 +0000</pubDate>
    <dc:creator>uploader</dc:creator>
    <description><![CDATA[<b>1.2 GB</b>]]></description>
    <enclosure url="https://tracker.example/download/2.torrent" length="1" type="application/x-bittorrent"/>
  </item>
  <item>
    <title>Show.S01E01 &amp; extras</title>
    <link>https://tracker.example/details/1</link>
    <torrent:magnetURI>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567</torrent:magnetURI>
  </item>
  <item>
    <title>Only a hash</title>
    <torrent:infoHash>89abcdef0123456789abcdef0123456789abcdef</torrent:infoHash>
  </item>
</channel>
</rss>`
	f, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Example tracker" || len(f.Articles) != 3 {
		t.Fatalf("feed: %+v", f)
	}
	a := f.Articles[0]
	want := Article{
		ID: "id-2", Title: "Show.S01E02.1080p", Author: "uploader", Description: "<b>1.2 GB</b>",
		Link: "https://tracker.example/details/2", TorrentURL: "https://tracker.example/download/2.torrent",
		Date: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC),
	}
	if a != want {
		t.Fatalf("article:\n%+v\nwant\n%+v", a, want)
	}
	if b := f.Articles[1]; b.Title != "Show.S01E01 & extras" || !strings.HasPrefix(b.TorrentURL, "magnet:") ||
		b.ID != b.TorrentURL || !b.Date.IsZero() {
		t.Fatalf("magnet article: %+v", b)
	}
	if c := f.Articles[2]; c.TorrentURL != "magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef" || c.ID != c.TorrentURL {
		t.Fatalf("info hash article: %+v", c)
	}
}

func TestParseAtomAndRDF(t *testing.T) {
	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><title type="text">Atom &amp; co</title>
<entry><title>Movie.2026</title><id>urn:x:1</id><updated>2026-11-17T10:00:00Z</updated>
<link href="https://a.example/1"/><link rel="enclosure" type="application/x-bittorrent" href="https://a.example/1.torrent"/>
<author><name>me</name></author><summary>Small</summary></entry></feed>`
	f, err := Parse(strings.NewReader(atom))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Atom & co" || len(f.Articles) != 1 {
		t.Fatalf("atom: %+v", f)
	}
	if a := f.Articles[0]; a.ID != "urn:x:1" || a.TorrentURL != "https://a.example/1.torrent" || a.Link != "https://a.example/1" ||
		a.Author != "me" || a.Description != "Small" || a.Date.IsZero() {
		t.Fatalf("atom entry: %+v", a)
	}

	rdf := `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/">
<channel><title>RDF</title></channel><item><title>One</title><link>https://r.example/1.torrent</link></item></rdf:RDF>`
	f, err = Parse(strings.NewReader(rdf))
	if err != nil || len(f.Articles) != 1 || f.Articles[0].TorrentURL != "https://r.example/1.torrent" {
		t.Fatalf("rdf: %+v %v", f, err)
	}
}

func TestParseCharsetAndErrors(t *testing.T) {
	latin1 := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss><channel><title>Caf\xe9</title></channel></rss>"
	f, err := Parse(strings.NewReader(latin1))
	if err != nil || f.Title != "Café" {
		t.Fatalf("latin-1: %+v %v", f, err)
	}
	for _, doc := range []string{"", "not xml", "<html><body>login</body></html>"} {
		if _, err := Parse(strings.NewReader(doc)); err == nil {
			t.Errorf("%q parsed as a feed", doc)
		}
	}
}
