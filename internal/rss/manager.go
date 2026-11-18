package rss

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Sep separates the names in an item path, as in qBittorrent.
const Sep = `\`

// Errors of item operations. They wrap into a PathError naming the path.
var (
	ErrExists     = errors.New("RSS item with given path already exists")
	ErrURLExists  = errors.New("RSS feed with given URL already exists")
	ErrNoParent   = errors.New("parent folder doesn't exist")
	ErrBadPath    = errors.New("incorrect RSS Item path")
	ErrNotFound   = errors.New("item doesn't exist")
	ErrNoFeed     = errors.New("feed doesn't exist")
	ErrRoot       = errors.New("cannot change the root folder")
	ErrIntoItself = errors.New("can't move a folder into itself or its subfolders")
)

// PathError is an item operation's error with the path it concerns.
type PathError struct {
	Err  error
	Path string
}

func (e *PathError) Error() string { return e.Err.Error() + ": " + e.Path + "." }
func (e *PathError) Unwrap() error { return e.Err }

// FeedInfo is a feed and, when asked for, its articles.
type FeedInfo struct {
	UID  string `json:"uid" doc:"Stable across renames and moves."`
	Path string `json:"path" doc:"Folder names and the feed's name, separated by backslashes."`
	URL  string `json:"url"`
	// RefreshInterval is the feed's own interval in seconds; 0 follows the
	// global setting.
	RefreshInterval int       `json:"refreshInterval" doc:"Seconds; 0 follows the rssRefreshInterval setting."`
	Title           string    `json:"title" doc:"The feed's own title, once fetched."`
	LastBuildDate   string    `json:"lastBuildDate"`
	FetchedAt       time.Time `json:"fetchedAt,omitzero"`
	Loading         bool      `json:"loading"`
	Error           string    `json:"error,omitempty" doc:"Why the last fetch failed."`
	Unread          int       `json:"unread"`
	Articles        []Article `json:"articles,omitempty"`
}

// Name is the last component of the feed's path.
func (f FeedInfo) Name() string { return lastName(f.Path) }

// Masked hides the query of the feed's URL, where trackers put passkeys,
// for callers who may not see them.
func (f FeedInfo) Masked() FeedInfo {
	f.URL = fetch.RedactString(f.URL)
	// Errors from before fetch errors were redacted quote the whole URL.
	f.Error = fetch.RedactText(f.Error)
	return f
}

type feed struct {
	FeedInfo
	// namedByURL is set for feeds added without a name: the first fetch
	// names them after their title.
	namedByURL bool
	refresh    bool // fetch at the next opportunity
	seq        int64
	// hidden are articles dropped past the limit that the feed still
	// lists, so they don't come back as new.
	hidden map[string]bool
}

// Deps are the services the manager uses.
type Deps struct {
	Store    *store.Store
	Settings *settings.Manager
	Fetcher  *fetch.Fetcher
	Engine   *engine.Engine
	Log      *slog.Logger
}

// Manager keeps the feeds and rules, fetches feeds on their schedule and
// runs the rules over new articles.
type Manager struct {
	d   Deps
	now func() time.Time

	mu       sync.Mutex
	folders  map[string]bool
	feeds    map[string]*feed // by uid
	rules    map[string]Rule
	smart    smartFilter
	lastHost map[string]time.Time

	// ruleGen counts each rule's replacements, so an auto-download run
	// doesn't undo a rule saved while it ran.
	ruleGen map[string]int

	// ctx is the manager's lifetime, for background work: a request's
	// context ends when its handler returns.
	ctx context.Context
	// runMu serialises auto-download runs, so an article isn't taken twice
	// before it is marked read.
	runMu sync.Mutex
	wake  chan struct{}
	wg    sync.WaitGroup
}

// New loads the feeds and rules.
func New(ctx context.Context, d Deps) (*Manager, error) {
	m := &Manager{
		d: d, now: time.Now, folders: map[string]bool{}, feeds: map[string]*feed{}, rules: map[string]Rule{},
		lastHost: map[string]time.Time{}, ruleGen: map[string]int{}, ctx: ctx, wake: make(chan struct{}, 1),
	}
	if err := m.load(ctx); err != nil {
		return nil, err
	}
	m.compileSmart(d.Settings.Get())
	d.Settings.Subscribe(func(old, cur settings.Settings) {
		if cur.RSSSmartEpisodeFilters != old.RSSSmartEpisodeFilters {
			m.mu.Lock()
			m.compileSmart(cur)
			m.mu.Unlock()
		}
		if cur.RSSMaxArticles != old.RSSMaxArticles {
			m.mu.Lock()
			for _, f := range m.feeds {
				m.trimLocked(ctx, f, cur.RSSMaxArticles)
			}
			m.mu.Unlock()
		}
		if cur.RSSAutoDownload && !old.RSSAutoDownload {
			m.goAutoDownload()
		}
		m.poke()
	})
	return m, nil
}

func (m *Manager) compileSmart(s settings.Settings) {
	f, err := compileSmartFilter(s.RSSSmartEpisodeFilters)
	if err != nil {
		m.d.Log.Warn("smart episode filters", "error", err)
	}
	m.smart = f
}

func (m *Manager) load(ctx context.Context) error {
	folders, err := m.d.Store.ListRSSFolders(ctx)
	if err != nil {
		return fmt.Errorf("rss folders: %w", err)
	}
	for _, p := range folders {
		m.folders[p] = true
	}
	feeds, err := m.d.Store.ListRSSFeeds(ctx)
	if err != nil {
		return fmt.Errorf("rss feeds: %w", err)
	}
	for _, r := range feeds {
		f := &feed{FeedInfo: FeedInfo{
			UID: r.Uid, Path: r.Path, URL: r.Url, RefreshInterval: int(r.RefreshInterval), Title: r.Title,
			LastBuildDate: r.LastBuildDate, Error: r.LastError,
		}}
		if r.FetchedAt > 0 {
			f.FetchedAt = time.Unix(r.FetchedAt, 0)
		}
		f.namedByURL = r.Title == "" && r.Path == r.Url
		m.feeds[f.UID] = f
	}
	articles, err := m.d.Store.ListRSSArticles(ctx)
	if err != nil {
		return fmt.Errorf("rss articles: %w", err)
	}
	for _, a := range articles {
		f := m.feeds[a.FeedUid]
		if f == nil {
			continue
		}
		f.seq = max(f.seq, a.Seq)
		if a.Hidden {
			f.hide(a.ID)
			continue
		}
		f.Articles = append(f.Articles, Article{
			ID: a.ID, Title: a.Title, Date: time.Unix(a.Date, 0).UTC(), TorrentURL: a.TorrentUrl, Link: a.Link,
			Description: a.Description, Author: a.Author, Category: a.Category, Read: a.IsRead, seq: a.Seq,
		})
	}
	rules, err := m.d.Store.ListRSSRules(ctx)
	if err != nil {
		return fmt.Errorf("rss rules: %w", err)
	}
	for _, r := range rules {
		rule := NewRule()
		if err := json.Unmarshal([]byte(r.Def), &rule); err != nil {
			m.d.Log.Warn("skipping an unreadable RSS rule", "rule", r.Name, "error", err)
			continue
		}
		rule.Add.upgrade()
		m.rules[r.Name] = rule
	}
	return nil
}

// Run fetches feeds on their schedule until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		m.fetchDue(ctx)
		select {
		case <-ctx.Done():
			m.wg.Wait()
			return
		case <-tick.C:
		case <-m.wake:
		}
	}
}

func (m *Manager) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// fetchDue starts fetching the feeds that are due, spacing requests to
// the same host by the fetch delay.
func (m *Manager) fetchDue(ctx context.Context) {
	s := m.d.Settings.Get()
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	busy := map[string]bool{}
	for _, f := range m.feeds {
		if f.Loading {
			busy[host(f.URL)] = true
		}
	}
	for _, f := range m.sortedFeedsLocked() {
		interval := time.Duration(s.RSSRefreshInterval) * time.Minute
		if f.RefreshInterval > 0 {
			interval = time.Duration(f.RefreshInterval) * time.Second
		}
		due := f.refresh || (s.RSSEnabled && now.Sub(f.FetchedAt) >= interval)
		h := host(f.URL)
		if f.Loading || !due || busy[h] || now.Sub(m.lastHost[h]) < time.Duration(s.RSSFetchDelay)*time.Second {
			continue
		}
		f.Loading, f.refresh = true, false
		busy[h] = true
		m.lastHost[h] = now
		uid, u := f.UID, f.URL
		m.wg.Go(func() { m.fetch(ctx, uid, u) })
	}
}

func host(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return u
}

// fetch downloads and parses a feed and stores its new articles.
func (m *Manager) fetch(ctx context.Context, uid, u string) {
	fctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	res, err := m.d.Fetcher.Get(fctx, u)
	var parsed Feed
	switch {
	case err != nil:
	case res.Magnet != "":
		err = errors.New("the URL leads to a magnet link, not a feed")
	default:
		parsed, err = Parse(bytes.NewReader(res.Body))
	}

	m.mu.Lock()
	f := m.feeds[uid]
	if f == nil || f.URL != u {
		// Removed or changed meanwhile.
		if f != nil {
			f.Loading = false
		}
		m.mu.Unlock()
		return
	}
	f.Loading = false
	f.FetchedAt = m.now()
	added := 0
	if err != nil {
		f.Error = err.Error()
		m.d.Log.Warn("RSS feed fetch failed", "feed", f.Path, "error", err)
	} else {
		f.Error = ""
		f.Title, f.LastBuildDate = parsed.Title, parsed.LastBuildDate
		added = m.mergeLocked(ctx, f, parsed.Articles)
		if f.namedByURL && f.Title != "" {
			m.nameAfterTitleLocked(ctx, f)
		}
		m.d.Log.Info("RSS feed updated", "feed", f.Path, "new", added)
	}
	m.saveFeedLocked(ctx, f)
	m.mu.Unlock()
	if added > 0 {
		m.goAutoDownload()
	}
}

func (f *feed) hide(id string) {
	if f.hidden == nil {
		f.hidden = map[string]bool{}
	}
	f.hidden[id] = true
}

// mergeLocked stores articles the feed doesn't have yet and drops the
// oldest beyond the limit. It returns how many were new.
func (m *Manager) mergeLocked(ctx context.Context, f *feed, articles []Article) int {
	have := map[string]bool{}
	for _, a := range f.Articles {
		have[a.ID] = true
	}
	listed := map[string]bool{}
	for _, a := range articles {
		listed[a.ID] = true
		if f.hidden[a.ID] {
			have[a.ID] = true
		}
	}
	// Forget dropped articles the feed no longer lists.
	for id := range f.hidden {
		if !listed[id] {
			delete(f.hidden, id)
			if err := m.d.Store.DeleteRSSArticle(ctx, store.DeleteRSSArticleParams{FeedUid: f.UID, ID: id}); err != nil {
				m.d.Log.Error("forgetting an RSS article", "feed", f.Path, "error", err)
			}
		}
	}
	now := m.now().UTC().Truncate(time.Second)
	added := 0
	// The document lists the newest first; store them oldest first, so
	// arrival order follows the feed.
	for _, a := range slices.Backward(articles) {
		if a.ID == "" || have[a.ID] {
			continue
		}
		have[a.ID] = true
		if a.Date.IsZero() {
			a.Date = now
		}
		f.seq++
		a.seq = f.seq
		f.Articles = append(f.Articles, a)
		added++
		if err := m.d.Store.InsertRSSArticle(ctx, store.InsertRSSArticleParams{
			FeedUid: f.UID, ID: a.ID, Seq: a.seq, Title: a.Title, Date: a.Date.Unix(), TorrentUrl: a.TorrentURL,
			Link: a.Link, Description: a.Description, Author: a.Author, Category: a.Category,
		}); err != nil {
			m.d.Log.Error("saving an RSS article", "feed", f.Path, "error", err)
		}
	}
	m.trimLocked(ctx, f, m.d.Settings.Get().RSSMaxArticles)
	return added
}

// trimLocked keeps the newest limit articles of a feed.
func (m *Manager) trimLocked(ctx context.Context, f *feed, limit int) {
	if len(f.Articles) <= limit {
		return
	}
	byAge := slices.Clone(f.Articles)
	slices.SortStableFunc(byAge, func(a, b Article) int {
		return cmp.Or(a.Date.Compare(b.Date), cmp.Compare(a.seq, b.seq))
	})
	drop := map[string]bool{}
	for _, a := range byAge[:len(byAge)-max(limit, 0)] {
		drop[a.ID] = true
		f.hide(a.ID)
		if err := m.d.Store.HideRSSArticle(ctx, store.HideRSSArticleParams{FeedUid: f.UID, ID: a.ID}); err != nil {
			m.d.Log.Error("dropping an RSS article", "feed", f.Path, "error", err)
		}
	}
	f.Articles = slices.DeleteFunc(f.Articles, func(a Article) bool { return drop[a.ID] })
}

// nameAfterTitleLocked renames a feed added without a name after its
// title, unless that name is taken.
func (m *Manager) nameAfterTitleLocked(ctx context.Context, f *feed) {
	f.namedByURL = false
	name := strings.ReplaceAll(strings.TrimSpace(f.Title), Sep, "/")
	target := join(parentOf(f.Path), name)
	if name == "" || m.existsLocked(target) {
		return
	}
	f.Path = target
	m.saveTreeLocked(ctx)
}

func (m *Manager) saveFeedLocked(ctx context.Context, f *feed) {
	var fetched int64
	if !f.FetchedAt.IsZero() {
		fetched = f.FetchedAt.Unix()
	}
	if err := m.d.Store.UpsertRSSFeed(ctx, store.UpsertRSSFeedParams{
		Uid: f.UID, Path: f.Path, Url: f.URL, RefreshInterval: int64(f.RefreshInterval), Title: f.Title,
		LastBuildDate: f.LastBuildDate, LastError: f.Error, FetchedAt: fetched,
	}); err != nil {
		m.d.Log.Error("saving an RSS feed", "feed", f.Path, "error", err)
	}
}

// saveTreeLocked writes the folders and every feed's path.
func (m *Manager) saveTreeLocked(ctx context.Context) {
	err := m.d.Store.Tx(ctx, func(q *store.Queries) error {
		if err := q.DeleteRSSFolders(ctx); err != nil {
			return err
		}
		for p := range m.folders {
			if err := q.InsertRSSFolder(ctx, p); err != nil {
				return err
			}
		}
		// Paths are unique, so a swap needs them out of the way first.
		for _, f := range m.feeds {
			if err := q.UpsertRSSFeed(ctx, store.UpsertRSSFeedParams{
				Uid: f.UID, Path: "\x00" + f.UID, Url: "\x00" + f.UID,
			}); err != nil {
				return err
			}
		}
		for _, f := range m.feeds {
			var fetched int64
			if !f.FetchedAt.IsZero() {
				fetched = f.FetchedAt.Unix()
			}
			if err := q.UpsertRSSFeed(ctx, store.UpsertRSSFeedParams{
				Uid: f.UID, Path: f.Path, Url: f.URL, RefreshInterval: int64(f.RefreshInterval), Title: f.Title,
				LastBuildDate: f.LastBuildDate, LastError: f.Error, FetchedAt: fetched,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		m.d.Log.Error("saving the RSS feeds", "error", err)
	}
}

// Path helpers.

func join(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + Sep + name
}

func parentOf(p string) string {
	if i := strings.LastIndex(p, Sep); i >= 0 {
		return p[:i]
	}
	return ""
}

func lastName(p string) string {
	return p[strings.LastIndex(p, Sep)+1:]
}

func validPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, Sep) && !strings.HasSuffix(p, Sep)
}

func within(p, folder string) bool {
	return folder == "" || p == folder || strings.HasPrefix(p, folder+Sep)
}

func (m *Manager) existsLocked(p string) bool {
	if m.folders[p] {
		return true
	}
	return m.feedAtLocked(p) != nil
}

func (m *Manager) feedAtLocked(p string) *feed {
	for _, f := range m.feeds {
		if f.Path == p {
			return f
		}
	}
	return nil
}

func (m *Manager) feedByURLLocked(u string) *feed {
	for _, f := range m.feeds {
		if f.URL == u {
			return f
		}
	}
	return nil
}

func (m *Manager) sortedFeedsLocked() []*feed {
	out := slices.Collect(maps.Values(m.feeds))
	slices.SortFunc(out, func(a, b *feed) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// checkParentLocked checks that a new item's folder exists.
func (m *Manager) checkParentLocked(p string) error {
	if !validPath(p) {
		return &PathError{ErrBadPath, p}
	}
	if m.existsLocked(p) {
		return &PathError{ErrExists, p}
	}
	if parent := parentOf(p); parent != "" && !m.folders[parent] {
		return &PathError{ErrNoParent, parent}
	}
	return nil
}

// Folders returns the folder paths, sorted.
func (m *Manager) Folders() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Sorted(maps.Keys(m.folders))
}

// Feeds returns the feeds sorted by path, with their articles when asked.
func (m *Manager) Feeds(withArticles bool) []FeedInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []FeedInfo
	for _, f := range m.sortedFeedsLocked() {
		info := f.FeedInfo
		info.Articles = nil
		for _, a := range f.Articles {
			if !a.Read {
				info.Unread++
			}
		}
		if withArticles {
			info.Articles = slices.Clone(f.Articles)
			// Newest first.
			slices.SortStableFunc(info.Articles, func(a, b Article) int {
				return cmp.Or(b.Date.Compare(a.Date), cmp.Compare(b.seq, a.seq))
			})
		}
		out = append(out, info)
	}
	return out
}

// Feed returns one feed with its articles.
func (m *Manager) Feed(uid string) (FeedInfo, bool) {
	for _, f := range m.Feeds(true) {
		if f.UID == uid {
			return f, true
		}
	}
	return FeedInfo{}, false
}

// AddFolder creates a folder; its parent must exist.
func (m *Manager) AddFolder(ctx context.Context, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkParentLocked(p); err != nil {
		return err
	}
	m.folders[p] = true
	m.saveTreeLocked(ctx)
	return nil
}

// AddFeed adds a feed at a path. An empty path names it after its URL
// until the first fetch names it after its title. refreshInterval is in
// seconds; 0 follows the global setting.
func (m *Manager) AddFeed(ctx context.Context, feedURL, p string, refreshInterval int) (FeedInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	named := p == ""
	if named {
		p = strings.ReplaceAll(feedURL, Sep, "/")
	}
	if err := m.checkParentLocked(p); err != nil {
		return FeedInfo{}, err
	}
	if m.feedByURLLocked(feedURL) != nil {
		return FeedInfo{}, &PathError{ErrURLExists, feedURL}
	}
	f := &feed{FeedInfo: FeedInfo{UID: newUID(), Path: p, URL: feedURL, RefreshInterval: max(refreshInterval, 0)}, namedByURL: named}
	f.refresh = true
	m.feeds[f.UID] = f
	m.saveFeedLocked(ctx, f)
	m.poke()
	return f.FeedInfo, nil
}

// newUID makes a feed ID like qBittorrent's: a UUID in braces.
func newUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("{%x-%x-%x-%x-%x}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// RemoveItem removes a feed, or a folder with everything in it.
func (m *Manager) RemoveItem(ctx context.Context, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p == "" {
		return &PathError{ErrRoot, p}
	}
	if !m.existsLocked(p) {
		return &PathError{ErrNotFound, p}
	}
	for uid, f := range m.feeds {
		if within(f.Path, p) {
			delete(m.feeds, uid)
			if err := m.d.Store.DeleteRSSFeed(ctx, uid); err != nil {
				m.d.Log.Error("removing an RSS feed", "feed", f.Path, "error", err)
			}
		}
	}
	for folder := range m.folders {
		if within(folder, p) {
			delete(m.folders, folder)
		}
	}
	m.saveTreeLocked(ctx)
	return nil
}

// MoveItem moves or renames a feed or folder.
func (m *Manager) MoveItem(ctx context.Context, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case from == "":
		return &PathError{ErrRoot, from}
	case !m.existsLocked(from):
		return &PathError{ErrNotFound, from}
	case from == to:
		return nil
	case m.folders[from] && within(to, from):
		return &PathError{ErrIntoItself, to}
	}
	if err := m.checkParentLocked(to); err != nil {
		return err
	}
	rename := func(p string) string { return to + strings.TrimPrefix(p, from) }
	for _, f := range m.feeds {
		if within(f.Path, from) {
			f.Path = rename(f.Path)
			f.namedByURL = false
		}
	}
	for folder := range m.folders {
		if within(folder, from) {
			delete(m.folders, folder)
			m.folders[rename(folder)] = true
		}
	}
	m.saveTreeLocked(ctx)
	return nil
}

// SetFeedURL changes a feed's URL; rules that named the old URL name the
// new one.
func (m *Manager) SetFeedURL(ctx context.Context, p, feedURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.feedAtLocked(p)
	if f == nil {
		return &PathError{ErrNoFeed, p}
	}
	if f.URL == feedURL {
		return nil
	}
	if m.feedByURLLocked(feedURL) != nil {
		return &PathError{ErrURLExists, feedURL}
	}
	old := f.URL
	f.URL = feedURL
	f.refresh = true
	m.saveFeedLocked(ctx, f)
	for name, r := range m.rules {
		if i := slices.Index(r.AffectedFeeds, old); i >= 0 {
			r = r.clone()
			r.AffectedFeeds[i] = feedURL
			m.putRuleLocked(ctx, name, r)
		}
	}
	m.poke()
	return nil
}

// SetFeedRefreshInterval sets a feed's own interval in seconds; 0 follows
// the global setting.
func (m *Manager) SetFeedRefreshInterval(ctx context.Context, p string, seconds int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.feedAtLocked(p)
	if f == nil {
		return &PathError{ErrNoFeed, p}
	}
	f.RefreshInterval = max(seconds, 0)
	m.saveFeedLocked(ctx, f)
	m.poke()
	return nil
}

// MarkAsRead marks an article read, or every article of a feed, of the
// feeds in a folder, or of every feed for the root. It reports false when
// the path doesn't exist.
func (m *Manager) MarkAsRead(ctx context.Context, p, articleID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p != "" && !m.existsLocked(p) {
		return false
	}
	for _, f := range m.feeds {
		if !within(f.Path, p) {
			continue
		}
		for i := range f.Articles {
			a := &f.Articles[i]
			if a.Read || (articleID != "" && a.ID != articleID) {
				continue
			}
			a.Read = true
			if err := m.d.Store.SetRSSArticleRead(ctx, store.SetRSSArticleReadParams{FeedUid: f.UID, ID: a.ID}); err != nil {
				m.d.Log.Error("marking an RSS article read", "feed", f.Path, "error", err)
			}
		}
	}
	return true
}

// Refresh fetches a feed, the feeds in a folder, or every feed, soon,
// whatever their schedule.
func (m *Manager) Refresh(p string) {
	m.mu.Lock()
	for _, f := range m.feeds {
		if within(f.Path, p) {
			f.refresh = true
		}
	}
	m.mu.Unlock()
	m.poke()
}

// Rules returns the rules by name.
func (m *Manager) Rules() map[string]Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Rule, len(m.rules))
	for k, r := range m.rules {
		out[k] = r.clone()
	}
	return out
}

func (m *Manager) putRuleLocked(ctx context.Context, name string, r Rule) {
	m.rules[name] = r
	m.ruleGen[name]++
	b, _ := json.Marshal(r)
	if err := m.d.Store.PutRSSRule(ctx, store.PutRSSRuleParams{Name: name, Def: string(b)}); err != nil {
		m.d.Log.Error("saving an RSS rule", "rule", name, "error", err)
	}
}

// SetRule creates or replaces a rule, and runs the enabled rules over the
// unread articles.
func (m *Manager) SetRule(ctx context.Context, name string, r Rule) {
	m.mu.Lock()
	m.putRuleLocked(ctx, name, r.clone())
	m.mu.Unlock()
	if r.Enabled {
		m.goAutoDownload()
	}
}

// RenameRule renames a rule; an unknown name or a taken new name does
// nothing, as in qBittorrent.
func (m *Manager) RenameRule(ctx context.Context, name, newName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[name]
	if _, taken := m.rules[newName]; !ok || taken || name == newName {
		return
	}
	delete(m.rules, name)
	if err := m.d.Store.DeleteRSSRule(ctx, name); err != nil {
		m.d.Log.Error("renaming an RSS rule", "rule", name, "error", err)
	}
	m.putRuleLocked(ctx, newName, r)
}

// RemoveRule removes a rule.
func (m *Manager) RemoveRule(ctx context.Context, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rules, name)
	if err := m.d.Store.DeleteRSSRule(ctx, name); err != nil {
		m.d.Log.Error("removing an RSS rule", "rule", name, "error", err)
	}
}

// CloneRule copies a rule under a new name, disabled and with its match
// history cleared.
func (m *Manager) CloneRule(ctx context.Context, name, cloneName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[name]
	if _, taken := m.rules[cloneName]; !ok || taken {
		return
	}
	c := r.clone()
	c.Enabled, c.LastMatch, c.PreviouslyMatchedEpisodes = false, nil, nil
	m.putRuleLocked(ctx, cloneName, c)
}

// Match is the articles of one feed a rule accepts.
type Match struct {
	FeedUID  string   `json:"feedUid"`
	FeedPath string   `json:"feedPath"`
	Titles   []string `json:"titles"`
}

// Matches lists, by feed, the titles of the articles a rule accepts, read
// or not and whether the rule is enabled or not. It doesn't record
// anything.
func (m *Manager) Matches(name string) []Match {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Match{}
	r, ok := m.rules[name]
	if !ok {
		return out
	}
	cr := compileRule(r)
	for _, f := range m.sortedFeedsLocked() {
		if !slices.Contains(r.AffectedFeeds, f.URL) {
			continue
		}
		match := Match{FeedUID: f.UID, FeedPath: f.Path}
		for _, a := range f.Articles {
			if _, ok := m.acceptLocked(cr, &r, a, false); ok {
				match.Titles = append(match.Titles, a.Title)
			}
		}
		if len(match.Titles) > 0 {
			out = append(out, match)
		}
	}
	return out
}

// MatchingArticles is Matches by feed name, as qBittorrent reports it.
// Feeds of the same name in different folders share an entry.
func (m *Manager) MatchingArticles(name string) map[string][]string {
	out := map[string][]string{}
	for _, match := range m.Matches(name) {
		n := lastName(match.FeedPath)
		out[n] = append(out[n], match.Titles...)
	}
	return out
}

// FeedPath returns the path of the feed with an ID.
func (m *Manager) FeedPath(uid string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f := m.feeds[uid]; f != nil {
		return f.Path, true
	}
	return "", false
}
