// Package rss reads RSS and Atom feeds and downloads matching torrents by
// rules, like qBittorrent's RSS downloader.
package rss

import (
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

// Feed is a parsed feed.
type Feed struct {
	Title string
	// LastBuildDate is the feed's own text: RSS lastBuildDate or Atom
	// updated.
	LastBuildDate string
	Articles      []Article
}

// Article is one item of a feed.
type Article struct {
	// ID identifies the article within its feed: the guid, else the
	// torrent URL, else the title.
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Date        time.Time `json:"date" doc:"From the feed, or when it was first seen."`
	TorrentURL  string    `json:"torrentURL,omitempty" doc:"What a rule adds: an enclosure, a magnet URI, or the link."`
	Link        string    `json:"link,omitempty"`
	Description string    `json:"description,omitempty" doc:"HTML from the feed; sanitise before showing it."`
	Author      string    `json:"author,omitempty"`
	Category    string    `json:"category,omitempty"`
	Read        bool      `json:"read"`
	seq         int64
}

// rssDoc covers RSS 2.0, RSS 1.0 (RDF) and Atom in one structure: the
// decoder matches elements by local name.
type rssDoc struct {
	XMLName xml.Name
	// RSS 2.0 and Atom.
	Channel struct {
		Title         string    `xml:"title"`
		LastBuildDate string    `xml:"lastBuildDate"`
		Items         []rssItem `xml:"item"`
	} `xml:"channel"`
	// RSS 1.0 puts items next to the channel; Atom has entries.
	Items   []rssItem   `xml:"item"`
	Title   atomText    `xml:"title"`
	Updated string      `xml:"updated"`
	Entries []atomEntry `xml:"entry"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Author      string `xml:"author"`
	Creator     string `xml:"creator"`
	Category    string `xml:"category"`
	PubDate     string `xml:"pubDate"`
	Date        string `xml:"date"`
	Enclosure   []struct {
		URL  string `xml:"url,attr"`
		Type string `xml:"type,attr"`
	} `xml:"enclosure"`
	// The torrent namespace (xmlns:torrent), used by many trackers.
	MagnetURI string `xml:"magnetURI"`
	InfoHash  string `xml:"infoHash"`
}

type atomText struct {
	Type string `xml:"type,attr"`
	Body string `xml:",innerxml"`
}

func (t atomText) text() string {
	s := strings.TrimSpace(t.Body)
	if strings.HasPrefix(s, "<![CDATA[") && strings.HasSuffix(s, "]]>") {
		return strings.TrimSuffix(strings.TrimPrefix(s, "<![CDATA["), "]]>")
	}
	if t.Type == "xhtml" {
		return s
	}
	return html.UnescapeString(s)
}

type atomEntry struct {
	Title   atomText `xml:"title"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
	Publish string   `xml:"published"`
	Summary atomText `xml:"summary"`
	Content atomText `xml:"content"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Links []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
		Type string `xml:"type,attr"`
	} `xml:"link"`
}

// ErrNotAFeed is returned for documents that aren't RSS or Atom.
var ErrNotAFeed = errors.New("not an RSS or Atom feed")

// Parse reads an RSS or Atom document.
func Parse(r io.Reader) (Feed, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	var doc rssDoc
	if err := dec.Decode(&doc); err != nil {
		return Feed{}, fmt.Errorf("%w: %w", ErrNotAFeed, err)
	}
	switch strings.ToLower(doc.XMLName.Local) {
	case "rss", "rdf":
		items := slices.Concat(doc.Channel.Items, doc.Items)
		f := Feed{Title: strings.TrimSpace(doc.Channel.Title), LastBuildDate: strings.TrimSpace(doc.Channel.LastBuildDate)}
		for _, it := range items {
			f.Articles = append(f.Articles, it.article())
		}
		return f, nil
	case "feed":
		f := Feed{Title: doc.Title.text(), LastBuildDate: strings.TrimSpace(doc.Updated)}
		for _, e := range doc.Entries {
			f.Articles = append(f.Articles, e.article())
		}
		return f, nil
	default:
		return Feed{}, ErrNotAFeed
	}
}

func (it rssItem) article() Article {
	a := Article{
		Title:       strings.TrimSpace(it.Title),
		Link:        strings.TrimSpace(it.Link),
		Description: strings.TrimSpace(it.Description),
		Author:      strings.TrimSpace(firstNonEmpty(it.Author, it.Creator)),
		Category:    strings.TrimSpace(it.Category),
		Date:        parseDate(firstNonEmpty(it.PubDate, it.Date)),
	}
	for _, e := range it.Enclosure {
		if e.Type == "application/x-bittorrent" || strings.HasPrefix(e.URL, "magnet:") {
			a.TorrentURL = strings.TrimSpace(e.URL)
			break
		}
	}
	// The torrent namespace's magnet and info hash beat the link, which
	// is often a details page.
	switch {
	case a.TorrentURL != "":
	case len(it.Enclosure) > 0 && it.Enclosure[0].URL != "":
		a.TorrentURL = strings.TrimSpace(it.Enclosure[0].URL)
	case it.MagnetURI != "":
		a.TorrentURL = strings.TrimSpace(it.MagnetURI)
	case it.InfoHash != "":
		a.TorrentURL = "magnet:?xt=urn:btih:" + strings.TrimSpace(it.InfoHash)
	default:
		a.TorrentURL = a.Link
	}
	if strings.HasPrefix(a.Link, "magnet:") {
		a.Link = ""
	}
	a.ID = firstNonEmpty(strings.TrimSpace(it.GUID), a.TorrentURL, a.Title)
	return a
}

func (e atomEntry) article() Article {
	a := Article{
		Title:       e.Title.text(),
		Author:      strings.TrimSpace(e.Author.Name),
		Description: firstNonEmpty(e.Content.text(), e.Summary.text()),
		Date:        parseDate(firstNonEmpty(e.Publish, e.Updated)),
	}
	for _, l := range e.Links {
		switch {
		case l.Rel == "enclosure" && (l.Type == "application/x-bittorrent" || strings.HasPrefix(l.Href, "magnet:")):
			a.TorrentURL = strings.TrimSpace(l.Href)
		case (l.Rel == "" || l.Rel == "alternate") && a.Link == "":
			a.Link = strings.TrimSpace(l.Href)
		}
	}
	if a.TorrentURL == "" {
		a.TorrentURL = a.Link
	}
	a.ID = firstNonEmpty(strings.TrimSpace(e.ID), a.TorrentURL, a.Title)
	return a
}

var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC3339Nano,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2 Jan 2006 15:04:05 -0700",
	"Mon, 02 Jan 2006 15:04 -0700", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02",
}

// parseDate reads the date formats feeds use; zero when none fits.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
