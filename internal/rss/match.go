package rss

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// regexTimeout bounds one match of a user's regular expression: regexp2
// backtracks, so a pathological pattern could otherwise run for a long
// time.
const regexTimeout = 100 * time.Millisecond

// textMatcher matches a title against MustContain or MustNotContain.
type textMatcher struct {
	empty bool
	// invalid is an expression that didn't compile: it matches nothing.
	invalid bool
	match   func(title string) bool
}

// compileText compiles a MustContain or MustNotContain expression.
func compileText(expr string, useRegex bool) textMatcher {
	if strings.TrimSpace(expr) == "" {
		return textMatcher{empty: true}
	}
	if useRegex {
		re, err := regexp2.Compile(expr, regexp2.IgnoreCase)
		if err != nil {
			return textMatcher{invalid: true}
		}
		re.MatchTimeout = regexTimeout
		return textMatcher{match: func(title string) bool {
			ok, err := re.MatchString(title)
			return err == nil && ok
		}}
	}
	// Wildcards: | separates alternatives, whitespace separates terms
	// that must all match.
	var alts [][]*regexp.Regexp
	for alt := range strings.SplitSeq(expr, "|") {
		var terms []*regexp.Regexp
		for _, term := range strings.Fields(alt) {
			// A character class such as [!] or [z-a] doesn't compile; like
			// an invalid regular expression, the term then matches nothing.
			re, err := regexp.Compile("(?i)" + wildcardRegexp(term))
			if err != nil {
				return textMatcher{invalid: true}
			}
			terms = append(terms, re)
		}
		alts = append(alts, terms)
	}
	return textMatcher{match: func(title string) bool {
		for _, terms := range alts {
			all := true
			for _, t := range terms {
				if !t.MatchString(title) {
					all = false
					break
				}
			}
			// An empty alternative matches every title.
			if all {
				return true
			}
		}
		return false
	}}
}

// wildcardRegexp translates a wildcard term: * is any run of characters,
// ? one character, [...] a character class; everything else is literal.
func wildcardRegexp(term string) string {
	var b strings.Builder
	for i := 0; i < len(term); i++ {
		switch c := term[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			end := strings.IndexByte(term[i+1:], ']')
			if end <= 0 {
				b.WriteString(regexp.QuoteMeta("["))
				continue
			}
			class := term[i+1 : i+1+end]
			neg := strings.HasPrefix(class, "!") || strings.HasPrefix(class, "^")
			if neg {
				class = class[1:]
			}
			// Inside a class only \, ] and ^ are special; quote \ and ^.
			class = strings.NewReplacer(`\`, `\\`, `^`, `\^`).Replace(class)
			if neg {
				b.WriteString("[^" + class + "]")
			} else {
				b.WriteString("[" + class + "]")
			}
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// episodeFilter is a parsed EpisodeFilter: one season, and ranges of
// episodes in it. An open range continues into every later season.
type episodeFilter struct {
	season int
	ranges []episodeRange
}

type episodeRange struct {
	first, last int // last is -1 for an open range
}

var (
	firstSegment  = regexp.MustCompile(`(?i)^(\d+)x(\d+)(-(\d*))?$`)
	leadingNumber = regexp.MustCompile(`^\d+`)
	titleSxxEyy   = regexp.MustCompile(`(?i)s(\d+)e(\d+)`)
	titleNxNN     = regexp.MustCompile(`(?i)\b(\d+)x(\d+)\b`)
)

// parseEpisodeFilter reads an episode filter such as 1x2;8-15;5;30-;. It
// reports false for an invalid filter, which matches nothing.
func parseEpisodeFilter(s string) (episodeFilter, bool) {
	if s == "" || !strings.HasSuffix(s, ";") || strings.TrimLeft(s, " \t") != s {
		return episodeFilter{}, false
	}
	segments := strings.Split(strings.TrimSuffix(s, ";"), ";")
	m := firstSegment.FindStringSubmatch(segments[0])
	if m == nil {
		return episodeFilter{}, false
	}
	f := episodeFilter{season: atoi(m[1])}
	f.ranges = append(f.ranges, makeRange(m[2], m[3] != "", m[4]))
	for _, seg := range segments[1:] {
		// Later segments are read leniently, as qBittorrent does: the
		// leading number starts the range, and digits after a - end it, so
		// 1x04 reads as 1 and 1x03-4 as 1-4. Segments starting with
		// anything but a digit, such as a space, are skipped.
		m := leadingNumber.FindString(seg)
		if m == "" {
			continue
		}
		dash := strings.IndexByte(seg, '-')
		if dash < 0 {
			f.ranges = append(f.ranges, makeRange(m, false, ""))
			continue
		}
		f.ranges = append(f.ranges, makeRange(m, true, leadingNumber.FindString(seg[dash+1:])))
	}
	return f, true
}

func makeRange(first string, isRange bool, last string) episodeRange {
	r := episodeRange{first: atoi(first), last: atoi(first)}
	switch {
	case isRange && last == "":
		r.last = -1
	case isRange:
		r.last = atoi(last)
	}
	return r
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// titleEpisode finds the season and first episode in a title: SxxEyy, or
// NxNN.
func titleEpisode(title string) (season, episode int, ok bool) {
	m := titleSxxEyy.FindStringSubmatch(title)
	if m == nil {
		m = titleNxNN.FindStringSubmatch(title)
	}
	if m == nil {
		return 0, 0, false
	}
	return atoi(m[1]), atoi(m[2]), true
}

// matches reports whether a title's episode passes the filter. Season 0
// stands for every season, as in qBittorrent.
func (f episodeFilter) matches(title string) bool {
	season, episode, ok := titleEpisode(title)
	if !ok {
		return false
	}
	for _, r := range f.ranges {
		switch {
		case r.last == -1 && f.season != 0 && season > f.season:
			return true
		case f.season != 0 && season != f.season:
		case r.last == -1 && episode >= r.first:
			return true
		case r.last >= r.first && episode >= r.first && episode <= r.last:
			return true
		}
	}
	return false
}

// smartFilter recognises the episode in a title for the smart episode
// filter.
type smartFilter struct {
	re *regexp2.Regexp
}

// compileSmartFilter combines the patterns, one per line, as qBittorrent
// does: one alternation between word boundaries or underscores, ignoring
// case.
func compileSmartFilter(patterns string) (smartFilter, error) {
	var lines []string
	for line := range strings.Lines(patterns) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return smartFilter{}, nil
	}
	re, err := regexp2.Compile(`(?:_|\b)(?:`+strings.Join(lines, "|")+`)(?:_|\b)`, regexp2.IgnoreCase)
	if err != nil {
		return smartFilter{}, fmt.Errorf("smart episode filters: %w", err)
	}
	re.MatchTimeout = regexTimeout
	return smartFilter{re: re}, nil
}

// episodeID is the smart filter's ID for a title's episode: 1x2 for two
// numbers (season and episode), or the matched text of a single group,
// such as a date. ok is false for titles the filters don't recognise.
func (f smartFilter) episodeID(title string) (string, bool) {
	if f.re == nil {
		return "", false
	}
	m, err := f.re.FindStringMatch(title)
	if err != nil || m == nil {
		return "", false
	}
	var caps []string
	for _, g := range m.Groups()[1:] {
		if len(g.Captures) > 0 {
			caps = append(caps, g.String())
		}
	}
	switch {
	case len(caps) >= 2:
		s, err1 := strconv.Atoi(caps[0])
		e, err2 := strconv.Atoi(caps[1])
		if err1 != nil || err2 != nil {
			return "", false
		}
		return fmt.Sprintf("%dx%d", s, e), true
	case len(caps) == 1:
		return caps[0], true
	default:
		return "", false
	}
}

// repackSuffix is -REPACK or -PROPER for a title that is one, else "".
func repackSuffix(title string) string {
	upper := strings.ToUpper(title)
	switch {
	case strings.Contains(upper, "REPACK"):
		return "-REPACK"
	case strings.Contains(upper, "PROPER"):
		return "-PROPER"
	default:
		return ""
	}
}
