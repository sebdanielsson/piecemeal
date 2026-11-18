package rss

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// compiledRule is a rule's matchers.
type compiledRule struct {
	mustContain, mustNotContain textMatcher
	episodes                    *episodeFilter
	badEpisodeFilter            bool
}

func compileRule(r Rule) compiledRule {
	c := compiledRule{
		mustContain:    compileText(r.MustContain, r.UseRegex),
		mustNotContain: compileText(r.MustNotContain, r.UseRegex),
	}
	if r.EpisodeFilter != "" {
		if f, ok := parseEpisodeFilter(r.EpisodeFilter); ok {
			c.episodes = &f
		} else {
			c.badEpisodeFilter = true
		}
	}
	return c
}

// acceptLocked reports whether a rule accepts an article, and the smart
// filter's episode ID for it. With record, a smart-filter accept is added
// to the rule's history at once, so later articles of the same episode in
// the same run are refused.
func (m *Manager) acceptLocked(c compiledRule, r *Rule, a Article, record bool) (string, bool) {
	title := a.Title
	if !c.mustContain.empty && (c.mustContain.invalid || !c.mustContain.match(title)) {
		return "", false
	}
	// An invalid MustNotContain excludes nothing.
	if !c.mustNotContain.empty && !c.mustNotContain.invalid && c.mustNotContain.match(title) {
		return "", false
	}
	if c.badEpisodeFilter || (c.episodes != nil && !c.episodes.matches(title)) {
		return "", false
	}
	if r.IgnoreDays > 0 && r.LastMatch != nil && a.Date.Before(r.LastMatch.Add(time.Duration(r.IgnoreDays)*24*time.Hour)) {
		return "", false
	}
	if !r.SmartFilter {
		return "", true
	}
	id, ok := m.smart.episodeID(title)
	if !ok {
		return "", false
	}
	if slices.Contains(r.PreviouslyMatchedEpisodes, id) {
		suffix := repackSuffix(title)
		if suffix == "" || !m.d.Settings.Get().RSSDownloadRepacks {
			return "", false
		}
		id += suffix
		if slices.Contains(r.PreviouslyMatchedEpisodes, id) {
			return "", false
		}
	}
	if record {
		r.PreviouslyMatchedEpisodes = append(r.PreviouslyMatchedEpisodes, id)
	}
	return id, true
}

// download is an article a rule took.
type download struct {
	feedUID, feedPath, articleID, title, torrentURL, rule string
	add                                                   AddOptions
}

func (m *Manager) goAutoDownload(ctx context.Context) {
	if !m.d.Settings.Get().RSSAutoDownload {
		return
	}
	m.wg.Go(func() { m.AutoDownload(ctx) })
}

// AutoDownload runs the enabled rules over the unread articles, newest
// first, and adds what they take. A rule records the article's date as
// its last match and, with the smart filter, the episode. Articles are
// marked read once added, or when the torrent was already there; a
// failed add leaves the article for the next run.
func (m *Manager) AutoDownload(ctx context.Context) {
	if !m.d.Settings.Get().RSSAutoDownload {
		return
	}
	m.runMu.Lock()
	defer m.runMu.Unlock()

	m.mu.Lock()
	names := make([]string, 0, len(m.rules))
	for name, r := range m.rules {
		if r.Enabled {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(m.rules[a].Priority, m.rules[b].Priority), strings.Compare(a, b))
	})
	compiled := map[string]compiledRule{}
	for _, name := range names {
		compiled[name] = compileRule(m.rules[name])
	}
	var jobs []download
	changed := map[string]bool{}
	for _, f := range m.sortedFeedsLocked() {
		articles := slices.Clone(f.Articles)
		slices.SortStableFunc(articles, func(a, b Article) int {
			return cmp.Or(b.Date.Compare(a.Date), cmp.Compare(b.seq, a.seq))
		})
		for _, a := range articles {
			if a.Read {
				continue
			}
			for _, name := range names {
				r := m.rules[name]
				if !slices.Contains(r.AffectedFeeds, f.URL) {
					continue
				}
				r = r.clone()
				if _, ok := m.acceptLocked(compiled[name], &r, a, true); !ok {
					continue
				}
				d := a.Date
				r.LastMatch = &d
				m.rules[name] = r
				changed[name] = true
				jobs = append(jobs, download{
					feedUID: f.UID, feedPath: f.Path, articleID: a.ID, title: a.Title, torrentURL: a.TorrentURL,
					rule: name, add: r.Add,
				})
				break
			}
		}
	}
	for name := range changed {
		m.putRuleLocked(ctx, name, m.rules[name])
	}
	m.mu.Unlock()

	for _, j := range jobs {
		m.d.Log.Info("RSS article accepted by rule, adding the torrent", "article", j.title, "rule", j.rule, "feed", j.feedPath)
		hash, err := m.add(ctx, j.torrentURL, j.add)
		switch {
		case errors.Is(err, engine.ErrExists):
			m.d.Log.Info("RSS article's torrent was already added", "article", j.title, "hash", hash)
		case err != nil:
			m.d.Log.Warn("adding an RSS article's torrent failed", "article", j.title, "rule", j.rule, "error", err)
			continue
		}
		m.mu.Lock()
		if f := m.feeds[j.feedUID]; f != nil {
			m.mu.Unlock()
			m.MarkAsRead(ctx, f.Path, j.articleID)
		} else {
			m.mu.Unlock()
		}
	}
}

// add adds a torrent from a magnet URI or a URL of a .torrent file.
func (m *Manager) add(ctx context.Context, torrentURL string, o AddOptions) (string, error) {
	if torrentURL == "" {
		return "", fmt.Errorf("%w: the article has no torrent link", engine.ErrInvalid)
	}
	p := engine.AddParams{
		Category: o.Category, Tags: o.Tags, SavePath: o.SavePath, Stopped: o.Stopped,
		ContentLayout: o.ContentLayout, SkipChecking: o.SkipChecking,
	}
	if o.RatioLimit != -2 {
		p.RatioLimit = &o.RatioLimit
	}
	if o.SeedingTimeLimit != -2 {
		p.SeedingTimeLimit = &o.SeedingTimeLimit
	}
	if o.InactiveSeedingTimeLimit != -2 {
		p.InactiveSeedingTimeLimit = &o.InactiveSeedingTimeLimit
	}
	res, err := m.d.Fetcher.Get(ctx, torrentURL)
	if err != nil {
		return "", err
	}
	if res.Magnet != "" {
		p.Magnet = res.Magnet
	} else if p.Magnet, p.Metainfo, err = engine.ParseSource(res.Body); err != nil {
		return "", err
	}
	if o.Category != "" {
		if err := m.d.Engine.PutCategory(ctx, engine.Category{Name: o.Category}, true); err != nil && !errors.Is(err, engine.ErrExists) {
			return "", err
		}
	}
	hash, err := m.d.Engine.Add(ctx, p)
	if err == nil && o.Forced {
		err = m.d.Engine.SetForced(ctx, true, hash)
	}
	return hash, err
}
