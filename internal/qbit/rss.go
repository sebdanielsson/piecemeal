package qbit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"

	"github.com/sebdanielsson/piecemeal/internal/rss"
)

// rss/* follows qBittorrent 5.3.0: actions answer 200 with an empty body,
// except setFeedRefreshInterval and importRules (204); tree errors are 409
// with a one-line reason. Reading needs any user; changing feeds and rules
// needs an admin, since rules add torrents.

// required answers 400 unless every parameter is present; an empty value
// counts as present.
func required(w http.ResponseWriter, r *http.Request, names ...string) bool {
	var missing []string
	for _, n := range names {
		if !r.Form.Has(n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		http.Error(w, "Missing required parameters: "+strings.Join(missing, ", "), http.StatusBadRequest)
		return false
	}
	return true
}

// rssWrite checks an admin made a write and its parameters are present.
func rssWrite(w http.ResponseWriter, r *http.Request, names ...string) bool {
	return requireAdmin(w, r) && required(w, r, names...)
}

func (s *Server) rssError(w http.ResponseWriter, err error) {
	var pe *rss.PathError
	if errors.As(err, &pe) {
		// qBittorrent's messages are sentences.
		msg := err.Error()
		http.Error(w, strings.ToUpper(msg[:1])+msg[1:], http.StatusConflict)
		return
	}
	s.engineError(w, err)
}

func (s *Server) rssAddFolder(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "path") {
		return
	}
	if err := s.rss.AddFolder(r.Context(), r.FormValue("path")); err != nil {
		s.rssError(w, err)
		return
	}
	done(w)
}

func (s *Server) rssAddFeed(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "url", "path") {
		return
	}
	// A refreshInterval that isn't a positive integer is ignored.
	interval, _ := strconv.Atoi(r.FormValue("refreshInterval"))
	if _, err := s.rss.AddFeed(r.Context(), r.FormValue("url"), r.FormValue("path"), max(interval, 0)); err != nil {
		s.rssError(w, err)
		return
	}
	done(w)
}

func (s *Server) rssRemoveItem(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "path") {
		return
	}
	err := s.rss.RemoveItem(r.Context(), r.FormValue("path"))
	if errors.Is(err, rss.ErrRoot) {
		http.Error(w, "Cannot delete root folder.", http.StatusConflict)
		return
	}
	if err != nil {
		s.rssError(w, err)
		return
	}
	done(w)
}

func (s *Server) rssMoveItem(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "itemPath", "destPath") {
		return
	}
	err := s.rss.MoveItem(r.Context(), r.FormValue("itemPath"), r.FormValue("destPath"))
	switch {
	case errors.Is(err, rss.ErrRoot):
		http.Error(w, "Cannot move root folder.", http.StatusConflict)
	case errors.Is(err, rss.ErrIntoItself):
		http.Error(w, "Can't move a folder into itself or its subfolders.", http.StatusConflict)
	case err != nil:
		s.rssError(w, err)
	default:
		done(w)
	}
}

// rssDate is qBittorrent's article and lastMatch date format.
const rssDate = "02 Jan 2006 15:04:05 -0700"

func (s *Server) rssItems(w http.ResponseWriter, r *http.Request) {
	withData := strings.EqualFold(r.FormValue("withData"), "true")
	root := map[string]any{}
	folder := func(p string) map[string]any {
		cur := root
		if p == "" {
			return cur
		}
		for name := range strings.SplitSeq(p, rss.Sep) {
			next, ok := cur[name].(map[string]any)
			if !ok {
				next = map[string]any{}
				cur[name] = next
			}
			cur = next
		}
		return cur
	}
	for _, p := range s.rss.Folders() {
		folder(p)
	}
	admin := isAdmin(principal(r))
	for _, f := range s.rss.Feeds(withData) {
		if !admin {
			f = f.Masked()
		}
		obj := map[string]any{"uid": f.UID, "url": f.URL}
		if f.RefreshInterval > 0 {
			obj["refreshInterval"] = f.RefreshInterval
		}
		if withData {
			articles := []map[string]any{}
			for _, a := range f.Articles {
				articles = append(articles, articleObject(a))
			}
			obj["title"], obj["lastBuildDate"], obj["isLoading"] = f.Title, f.LastBuildDate, f.Loading
			obj["hasError"], obj["articles"] = f.Error != "", articles
		}
		folder(parentPath(f.Path))[f.Name()] = obj
	}
	writeJSON(w, root)
}

func parentPath(p string) string {
	if i := strings.LastIndex(p, rss.Sep); i >= 0 {
		return p[:i]
	}
	return ""
}

// articleHTML keeps the formatting of article descriptions and drops
// scripts, event handlers and script URLs: qBittorrent web UIs served on
// piecemeal's origin may render them as HTML.
var articleHTML = bluemonday.UGCPolicy()

// webURL reports whether s is a URL a web UI may link to.
func webURL(s string, schemes ...string) bool {
	u, err := url.Parse(s)
	return err == nil && slices.Contains(schemes, strings.ToLower(u.Scheme))
}

func articleObject(a rss.Article) map[string]any {
	obj := map[string]any{"id": a.ID, "title": a.Title, "date": a.Date.UTC().Format(rssDate), "torrentURL": nil}
	if webURL(a.TorrentURL, "http", "https", "magnet") {
		obj["torrentURL"] = a.TorrentURL
	}
	if !webURL(a.Link, "http", "https") {
		a.Link = ""
	}
	a.Description = articleHTML.Sanitize(a.Description)
	for k, v := range map[string]string{"link": a.Link, "description": a.Description, "author": a.Author, "category": a.Category} {
		if v != "" {
			obj[k] = v
		}
	}
	if a.Read {
		obj["isRead"] = true
	}
	return obj
}

func (s *Server) rssMarkAsRead(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "itemPath") {
		return
	}
	if !s.rss.MarkAsRead(r.Context(), r.FormValue("itemPath"), r.FormValue("articleId")) {
		noContent(w)
		return
	}
	done(w)
}

func (s *Server) rssRefreshItem(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "itemPath") {
		return
	}
	s.rss.Refresh(r.FormValue("itemPath"))
	done(w)
}

func (s *Server) rssSetFeedURL(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "path", "url") {
		return
	}
	if err := s.rss.SetFeedURL(r.Context(), r.FormValue("path"), r.FormValue("url")); err != nil {
		s.rssError(w, err)
		return
	}
	done(w)
}

func (s *Server) rssSetFeedRefreshInterval(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "path", "refreshInterval") {
		return
	}
	n, err := strconv.Atoi(r.FormValue("refreshInterval"))
	if err != nil || n < 0 {
		http.Error(w, "Invalid 'refreshInterval' value", http.StatusBadRequest)
		return
	}
	if err := s.rss.SetFeedRefreshInterval(r.Context(), r.FormValue("path"), n); err != nil {
		s.rssError(w, err)
		return
	}
	noContent(w)
}

func (s *Server) rssSetRule(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "ruleName", "ruleDef") {
		return
	}
	// As in qBittorrent, a definition that isn't a JSON object makes a
	// rule with every field at its default.
	var def map[string]json.RawMessage
	_ = json.Unmarshal([]byte(r.FormValue("ruleDef")), &def)
	s.rss.SetRule(r.Context(), r.FormValue("ruleName"), decodeRule(def))
	done(w)
}

func (s *Server) rssRenameRule(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "ruleName", "newRuleName") {
		return
	}
	s.rss.RenameRule(r.Context(), r.FormValue("ruleName"), r.FormValue("newRuleName"))
	done(w)
}

func (s *Server) rssRemoveRule(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "ruleName") {
		return
	}
	s.rss.RemoveRule(r.Context(), r.FormValue("ruleName"))
	done(w)
}

func (s *Server) rssCloneRule(w http.ResponseWriter, r *http.Request) {
	if !rssWrite(w, r, "sourceName", "cloneName") {
		return
	}
	s.rss.CloneRule(r.Context(), r.FormValue("sourceName"), r.FormValue("cloneName"))
	done(w)
}

func (s *Server) rulesObject(admin bool) map[string]any {
	out := map[string]any{}
	for name, rule := range s.rss.Rules() {
		if !admin {
			rule = rule.Masked()
		}
		out[name] = encodeRule(rule)
	}
	return out
}

func (s *Server) rssRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.rulesObject(isAdmin(principal(r))))
}

func (s *Server) rssExportRules(w http.ResponseWriter, r *http.Request) {
	b, err := json.MarshalIndent(s.rulesObject(isAdmin(principal(r))), "", "    ")
	if err != nil {
		s.engineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="rss-downloader-rules.json"`)
	_, _ = w.Write(b)
}

func (s *Server) rssImportRules(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		for _, fs := range r.MultipartForm.File {
			files = append(files, fs...)
		}
	}
	if len(files) != 1 {
		http.Error(w, "Exactly one rules file is required", http.StatusBadRequest)
		return
	}
	f, err := files[0].Open()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, 16<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var top any
	if err := json.Unmarshal(b, &top); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rules, ok := top.(map[string]any)
	if !ok {
		http.Error(w, "Invalid data format.", http.StatusBadRequest)
		return
	}
	decoded := map[string]rss.Rule{}
	for name, v := range rules {
		if _, ok := v.(map[string]any); !ok {
			http.Error(w, "Invalid data format.", http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(v)
		var def map[string]json.RawMessage
		_ = json.Unmarshal(raw, &def)
		decoded[name] = decodeRule(def)
	}
	for name, rule := range decoded {
		s.rss.SetRule(r.Context(), name, rule)
	}
	noContent(w)
}

func (s *Server) rssMatchingArticles(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "ruleName") {
		return
	}
	writeJSON(w, s.rss.MatchingArticles(r.FormValue("ruleName")))
}

// Rule definitions in qBittorrent's JSON form.

// decodeRule reads a qBittorrent rule definition. Missing fields and
// fields of the wrong type take their defaults. Without torrentParams, the
// legacy addPaused, assignedCategory, savePath and torrentContentLayout
// fields fill it.
func decodeRule(def map[string]json.RawMessage) rss.Rule {
	r := rss.NewRule()
	get := func(key string, v any) bool { return getJSON(def, key, v) }
	var b bool
	var n float64
	var str string
	if get("enabled", &b) {
		r.Enabled = b
	}
	if get("priority", &n) && n == math.Trunc(n) {
		r.Priority = int(n)
	}
	if get("useRegex", &b) {
		r.UseRegex = b
	}
	if get("mustContain", &str) {
		r.MustContain = str
	}
	if get("mustNotContain", &str) {
		r.MustNotContain = str
	}
	if get("episodeFilter", &str) {
		r.EpisodeFilter = str
	}
	if get("smartFilter", &b) {
		r.SmartFilter = b
	}
	r.PreviouslyMatchedEpisodes = stringList(def["previouslyMatchedEpisodes"])
	r.AffectedFeeds = stringList(def["affectedFeeds"])
	if get("ignoreDays", &n) && n == math.Trunc(n) {
		r.IgnoreDays = int(n)
	}
	if get("lastMatch", &str) {
		if t, ok := parseLastMatch(str); ok {
			r.LastMatch = &t
		}
	}
	if raw, ok := def["torrentParams"]; ok {
		var params map[string]json.RawMessage
		_ = json.Unmarshal(raw, &params)
		r.Add = decodeTorrentParams(params)
		return r
	}
	// Legacy fields.
	var stopped bool
	if get("addPaused", &stopped) {
		r.Add.Stopped = &stopped
	}
	if get("assignedCategory", &str) {
		r.Add.Category = str
	}
	if get("savePath", &str) {
		r.Add.SavePath = str
		if str != "" {
			r.Add.Extra = map[string]json.RawMessage{"use_auto_tmm": json.RawMessage("false")}
		}
	}
	if get("torrentContentLayout", &str) {
		r.Add.ContentLayout = str
	}
	return r
}

// getJSON decodes a field into v and reports whether it was there with a
// value of the right type. null counts as absent: decoding it would leave
// v as it was.
func getJSON(obj map[string]json.RawMessage, key string, v any) bool {
	raw, ok := obj[key]
	return ok && !isNull(raw) && json.Unmarshal(raw, v) == nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// stringList reads an array of strings, or a single string as a one-item
// list; anything else is an empty list.
func stringList(raw json.RawMessage) []string {
	if len(raw) == 0 || isNull(raw) {
		return []string{}
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var list []any
	_ = json.Unmarshal(raw, &list)
	out := []string{}
	for _, v := range list {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

var lastMatchLayouts = []string{
	"Mon, 2 Jan 2006 15:04:05 -0700", "2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04 -0700", "2 Jan 2006 15:04 -0700",
	"Mon, 2 Jan 2006 15:04:05", "2 Jan 2006 15:04:05", "Mon, 2 Jan 2006 15:04", "2 Jan 2006 15:04",
}

// parseLastMatch reads an RFC 2822 date with a numeric zone, or none for
// UTC, with or without the weekday and seconds.
func parseLastMatch(s string) (time.Time, bool) {
	for _, l := range lastMatchLayouts {
		if t, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Torrent parameters piecemeal applies; the rest are kept for clients.
var (
	keptParams     = []string{"download_path", "download_limit", "upload_limit", "share_limit_action", "share_limits_mode", "ssl_certificate", "ssl_private_key", "ssl_dh_params"}
	keptWhenSet    = []string{"stop_condition", "use_auto_tmm", "use_download_path", "add_to_top_of_queue"}
	keptParamsDefs = map[string]any{
		"download_path": "", "download_limit": -1, "upload_limit": -1, "share_limit_action": "Default",
		"share_limits_mode": "Default", "ssl_certificate": "", "ssl_private_key": "", "ssl_dh_params": "",
	}
)

func decodeTorrentParams(p map[string]json.RawMessage) rss.AddOptions {
	o := rss.NewRule().Add
	get := func(key string, v any) bool { return getJSON(p, key, v) }
	var str string
	var b bool
	var n float64
	if get("category", &str) {
		o.Category = str
	}
	var tags []any
	if get("tags", &tags) {
		for _, t := range tags {
			s, _ := t.(string)
			o.Tags = append(o.Tags, s)
		}
	}
	if get("save_path", &str) {
		o.SavePath = str
	}
	if get("stopped", &b) {
		stopped := b
		o.Stopped = &stopped
	}
	if get("content_layout", &str) {
		o.ContentLayout = str
	}
	if get("seed_mode", &b) || get("skip_checking", &b) {
		o.SkipChecking = b
	}
	if get("operating_mode", &str) {
		o.Forced = str == "Forced"
	}
	if get("ratio_limit", &n) {
		o.RatioLimit = n
	}
	if get("seeding_time_limit", &n) && n == math.Trunc(n) {
		o.SeedingTimeLimit = int64(n)
	}
	if get("inactive_seeding_time_limit", &n) && n == math.Trunc(n) {
		o.InactiveSeedingTimeLimit = int64(n)
	}
	for _, k := range append(slices.Clone(keptParams), keptWhenSet...) {
		if raw, ok := p[k]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if o.Extra == nil {
				o.Extra = map[string]json.RawMessage{}
			}
			o.Extra[k] = raw
		}
	}
	return o
}

// encodeRule writes a rule in qBittorrent 5.3.0's output form: all 16
// fields, torrentParams, and the legacy fields derived from it.
func encodeRule(r rss.Rule) map[string]any {
	o := r.Add
	params := map[string]any{
		"category": o.Category, "save_path": o.SavePath, "seed_mode": o.SkipChecking, "operating_mode": "AutoManaged",
		"ratio_limit": o.RatioLimit, "seeding_time_limit": o.SeedingTimeLimit,
		"inactive_seeding_time_limit": o.InactiveSeedingTimeLimit,
	}
	if o.Forced {
		params["operating_mode"] = "Forced"
	}
	tags := slices.Clone(o.Tags)
	slices.Sort(tags)
	if tags == nil {
		tags = []string{}
	}
	params["tags"] = tags
	for _, k := range keptParams {
		params[k] = keptParamsDefs[k]
	}
	for k, v := range o.Extra {
		params[k] = v
	}
	var addPaused, layout any
	if o.Stopped != nil {
		params["stopped"], addPaused = *o.Stopped, *o.Stopped
	}
	if o.ContentLayout != "" {
		params["content_layout"], layout = o.ContentLayout, o.ContentLayout
	}
	lastMatch := ""
	if r.LastMatch != nil {
		lastMatch = r.LastMatch.Format(rssDate)
	}
	nonNil := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	return map[string]any{
		"enabled": r.Enabled, "priority": r.Priority, "useRegex": r.UseRegex, "mustContain": r.MustContain,
		"mustNotContain": r.MustNotContain, "episodeFilter": r.EpisodeFilter, "smartFilter": r.SmartFilter,
		"previouslyMatchedEpisodes": nonNil(r.PreviouslyMatchedEpisodes), "affectedFeeds": nonNil(r.AffectedFeeds),
		"ignoreDays": r.IgnoreDays, "lastMatch": lastMatch, "torrentParams": params,
		"addPaused": addPaused, "assignedCategory": o.Category, "savePath": o.SavePath, "torrentContentLayout": layout,
	}
}
