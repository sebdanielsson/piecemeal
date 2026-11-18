package rss

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/fetch"
)

// Rule decides which articles to download, as qBittorrent's RSS download
// rules do. A rule accepts an article of one of its feeds when the title
// matches MustContain, doesn't match MustNotContain, passes the episode
// filters, and isn't dated too soon after the last match.
type Rule struct {
	Enabled bool `json:"enabled"`
	// Priority orders rules, lowest first; the first rule that accepts an
	// article takes it.
	Priority int  `json:"priority"`
	UseRegex bool `json:"useRegex" doc:"MustContain and MustNotContain are regular expressions instead of wildcard terms."`
	// MustContain and MustNotContain are wildcard terms (* and ?), all of
	// which must match, with | between alternatives; or regular
	// expressions with UseRegex. Matching ignores case.
	MustContain    string `json:"mustContain"`
	MustNotContain string `json:"mustNotContain"`
	EpisodeFilter  string `json:"episodeFilter" doc:"Season and episodes to take, such as 1x2;8-15;20-; (an open range continues into later seasons)."`
	SmartFilter    bool   `json:"smartFilter" doc:"Take each episode once, recognised by the smart episode filters setting."`
	// PreviouslyMatchedEpisodes are the episodes the smart filter took.
	PreviouslyMatchedEpisodes []string `json:"previouslyMatchedEpisodes"`
	AffectedFeeds             []string `json:"affectedFeeds" doc:"URLs of the feeds the rule applies to."`
	// IgnoreDays skips articles dated less than this many days after
	// LastMatch.
	IgnoreDays int `json:"ignoreDays"`
	// LastMatch is the date of the last article the rule took.
	LastMatch *time.Time `json:"lastMatch,omitempty"`
	Add       AddOptions `json:"add"`
}

// AddOptions are how a rule adds the torrents it takes.
type AddOptions struct {
	Category      string   `json:"category,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	SavePath      string   `json:"savePath,omitempty"`
	Stopped       *bool    `json:"stopped,omitempty" doc:"Add stopped; absent follows the addStopped setting."`
	ContentLayout string   `json:"contentLayout,omitempty" enum:"Original,Subfolder,NoSubfolder"`
	SkipChecking  bool     `json:"skipChecking,omitempty"`
	Forced        bool     `json:"forced,omitempty"`
	// Seeding goals: -2 follows the category, then the global setting; -1
	// is unlimited.
	RatioLimit               float64 `json:"ratioLimit"`
	SeedingTimeLimit         int64   `json:"seedingTimeLimit"`
	InactiveSeedingTimeLimit int64   `json:"inactiveSeedingTimeLimit"`
	SeedGoalAction           string  `json:"seedGoalAction,omitempty" enum:"default,stop,remove,removeWithData" doc:"What happens when the torrent reaches its seeding goals; absent or default follows the category, then the global setting."`
	SeedGoalMode             string  `json:"seedGoalMode,omitempty" enum:"default,any,all" doc:"Whether any goal or all of them must be met; absent or default follows the category, then the global setting."`
	// Extra keeps qBittorrent rule options piecemeal doesn't use, so
	// qBittorrent clients get them back unchanged.
	Extra map[string]json.RawMessage `json:"extra,omitempty" doc:"qBittorrent options piecemeal keeps but doesn't use."`
}

// NewRule returns a rule with the defaults qBittorrent gives a new one.
func NewRule() Rule {
	return Rule{
		Enabled: true,
		Add:     AddOptions{RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2},
	}
}

// upgrade moves the qBittorrent share limit action and mode, which
// piecemeal 1.0 kept in Extra without applying them, into their fields.
func (o *AddOptions) upgrade() {
	var v string
	if json.Unmarshal(o.Extra["share_limit_action"], &v) == nil {
		o.SeedGoalAction = map[string]string{"Stop": "stop", "Remove": "remove", "RemoveWithContent": "removeWithData"}[v]
	}
	if json.Unmarshal(o.Extra["share_limits_mode"], &v) == nil {
		o.SeedGoalMode = map[string]string{"MatchAny": "any", "MatchAll": "all"}[v]
	}
	delete(o.Extra, "share_limit_action")
	delete(o.Extra, "share_limits_mode")
	if len(o.Extra) == 0 {
		o.Extra = nil
	}
}

// Masked hides the queries of the rule's feed URLs, as FeedInfo.Masked.
func (r Rule) Masked() Rule {
	r = r.clone()
	for i, u := range r.AffectedFeeds {
		r.AffectedFeeds[i] = fetch.RedactString(u)
	}
	return r
}

func (r Rule) clone() Rule {
	r.PreviouslyMatchedEpisodes = slices.Clone(r.PreviouslyMatchedEpisodes)
	r.AffectedFeeds = slices.Clone(r.AffectedFeeds)
	r.Add.Tags = slices.Clone(r.Add.Tags)
	if r.LastMatch != nil {
		t := *r.LastMatch
		r.LastMatch = &t
	}
	if r.Add.Stopped != nil {
		b := *r.Add.Stopped
		r.Add.Stopped = &b
	}
	if r.Add.Extra != nil {
		extra := make(map[string]json.RawMessage, len(r.Add.Extra))
		for k, v := range r.Add.Extra {
			extra[k] = slices.Clone(v)
		}
		r.Add.Extra = extra
	}
	return r
}
