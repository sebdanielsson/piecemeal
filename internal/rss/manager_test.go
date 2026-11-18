package rss

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

type env struct {
	t        *testing.T
	ctx      context.Context
	store    *store.Store
	settings *settings.Manager
	engine   *engine.Engine
	fetcher  *fetch.Fetcher
	srv      *httptest.Server
	feed     string // the feed document
	m        *Manager
	cancel   context.CancelFunc
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	save := t.TempDir()
	defaults := settings.Defaults(save)
	defaults.PeerPort, defaults.DHT, defaults.RSSFetchDelay = 0, false, 0
	sm, err := settings.Load(ctx, st, defaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	e, err := engine.New(ctx, engine.Config{DataDir: t.TempDir(), AllowedRoots: []string{save}, ClientConfig: enginetest.Offline}, st, sm, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	env := &env{
		t: t, ctx: ctx, store: st, settings: sm, engine: e,
		fetcher: fetch.New([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}),
	}
	torrents := map[string][]byte{}
	for _, name := range []string{"e01", "e02", "e02r", "e03"} {
		torrents[name] = enginetest.NewSeeder(t, "Show "+name, map[string]int{"v.mkv": 1 << 10}).Metainfo
	}
	env.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := strings.CutPrefix(r.URL.Path, "/t/"); ok {
			_, _ = w.Write(torrents[name])
			return
		}
		_, _ = w.Write([]byte(env.feed))
	}))
	t.Cleanup(env.srv.Close)
	env.start()
	return env
}

func (e *env) start() {
	m, err := New(e.ctx, Deps{Store: e.store, Settings: e.settings, Fetcher: e.fetcher, Engine: e.engine, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	e.m, e.cancel = m, func() { cancel(); <-done }
	e.t.Cleanup(e.cancel)
}

func (e *env) item(title, torrent, date string) string {
	return fmt.Sprintf(`<item><title>%s</title><guid>%s</guid><pubDate>%s</pubDate>
<enclosure url="%s/t/%s" type="application/x-bittorrent"/></item>`, title, title, date, e.srv.URL, torrent)
}

func (e *env) waitFor(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFeedsAndAutoDownload(t *testing.T) {
	e := newEnv(t)
	ctx := e.ctx
	e.feed = `<rss><channel><title>Show feed</title>` +
		e.item("Show.S01E01.1080p", "e01", "Mon, 16 Nov 2026 10:00:00 +0000") +
		e.item("Show.S01E02.1080p", "e02", "Tue, 17 Nov 2026 10:00:00 +0000") +
		`</channel></rss>`

	// The tree.
	if err := e.m.AddFolder(ctx, "TV"); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]error{"TV": ErrExists, `Missing\Sub`: ErrNoParent, `\Lead`: ErrBadPath, "": ErrBadPath} {
		if err := e.m.AddFolder(ctx, p); !errors.Is(err, want) {
			t.Errorf("AddFolder(%q): %v, want %v", p, err, want)
		}
	}
	f, err := e.m.AddFeed(ctx, e.srv.URL+"/feed", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.AddFeed(ctx, e.srv.URL+"/feed", "Other", 0); !errors.Is(err, ErrURLExists) {
		t.Fatalf("duplicate URL: %v", err)
	}
	// Added without a name, the feed is named after its title once
	// fetched.
	e.waitFor("the first fetch", func() bool {
		feeds := e.m.Feeds(true)
		return len(feeds) == 1 && len(feeds[0].Articles) == 2 && feeds[0].Path == "Show feed"
	})
	if err := e.m.MoveItem(ctx, "Show feed", `TV\Show`); err != nil {
		t.Fatal(err)
	}
	if err := e.m.MoveItem(ctx, "TV", `TV\Inside`); !errors.Is(err, ErrIntoItself) {
		t.Fatalf("moving a folder into itself: %v", err)
	}

	// A smart-filter rule takes each episode once, newest first.
	rule := NewRule()
	rule.MustContain, rule.SmartFilter, rule.AffectedFeeds = "show 1080p", true, []string{f.URL}
	stopped := true
	rule.Add = AddOptions{Category: "rss", Stopped: &stopped, RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2}
	e.m.SetRule(ctx, "Show", rule)
	if got := e.m.MatchingArticles("Show"); len(got["Show"]) != 2 {
		t.Fatalf("matching articles: %v", got)
	}
	if _, err := e.settings.Set(ctx, map[string]any{"rssAutoDownload": true}); err != nil {
		t.Fatal(err)
	}
	e.waitFor("two torrents", func() bool { return len(e.engine.List()) == 2 })
	for _, tr := range e.engine.List() {
		if tr.Category != "rss" || !tr.Stopped {
			t.Fatalf("added torrent: %+v", tr)
		}
	}
	e.waitFor("the articles to be read", func() bool { return e.m.Feeds(false)[0].Unread == 0 })
	got := e.m.Rules()["Show"]
	if got.LastMatch == nil || len(got.PreviouslyMatchedEpisodes) != 2 {
		t.Fatalf("rule after the run: %+v", got)
	}

	// A repack of a taken episode is taken once; a duplicate of a taken
	// episode isn't.
	e.feed = strings.Replace(e.feed, "</channel>",
		e.item("Show.S01E02.REPACK.1080p", "e02r", "Wed, 18 Nov 2026 09:00:00 +0000")+
			e.item("Show.S01E02.1080p.OTHER", "e01", "Wed, 18 Nov 2026 09:30:00 +0000")+
			e.item("Show.S01E03.1080p", "e03", "Wed, 18 Nov 2026 10:00:00 +0000")+"</channel>", 1)
	e.m.Refresh(`TV\Show`)
	e.waitFor("the repack and the next episode", func() bool { return len(e.engine.List()) == 4 })
	got = e.m.Rules()["Show"]
	if !strings.Contains(strings.Join(got.PreviouslyMatchedEpisodes, ","), "1x2-REPACK") {
		t.Fatalf("history: %v", got.PreviouslyMatchedEpisodes)
	}

	// Everything survives a restart.
	e.cancel()
	e.start()
	feeds := e.m.Feeds(true)
	if len(feeds) != 1 || feeds[0].Path != `TV\Show` || feeds[0].UID != f.UID || len(feeds[0].Articles) != 5 {
		t.Fatalf("feeds after restart: %+v", feeds)
	}
	if len(e.m.Rules()["Show"].PreviouslyMatchedEpisodes) != 4 || len(e.m.Folders()) != 1 {
		t.Fatalf("rules or folders after restart: %+v %v", e.m.Rules(), e.m.Folders())
	}

	// Renaming, cloning and removing.
	e.m.RenameRule(ctx, "Show", "Renamed")
	e.m.CloneRule(ctx, "Renamed", "Copy")
	if rs := e.m.Rules(); len(rs) != 2 || rs["Copy"].Enabled || rs["Copy"].LastMatch != nil {
		t.Fatalf("rules after rename and clone: %+v", rs)
	}
	if !e.m.MarkAsRead(ctx, "", "") || e.m.MarkAsRead(ctx, "Nope", "") {
		t.Fatal("MarkAsRead reported the wrong paths")
	}
	if err := e.m.RemoveItem(ctx, "TV"); err != nil {
		t.Fatal(err)
	}
	if len(e.m.Feeds(false)) != 0 || len(e.m.Folders()) != 0 {
		t.Fatal("removing a folder left its feed")
	}
}

func TestFetchErrors(t *testing.T) {
	e := newEnv(t)
	e.feed = "<html>login</html>"
	if _, err := e.m.AddFeed(e.ctx, e.srv.URL+"/feed", "Broken", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.AddFeed(e.ctx, "not a url", "Bad", 0); err != nil {
		t.Fatal(err)
	}
	e.waitFor("both to fail", func() bool {
		feeds := e.m.Feeds(false)
		return len(feeds) == 2 && feeds[0].Error != "" && feeds[1].Error != ""
	})
}
