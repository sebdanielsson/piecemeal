package qbit

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// infinity is the ETA qBittorrent reports when it can't estimate one.
const infinity = 8640000

// State maps a torrent to qBittorrent's state name.
func State(t engine.Torrent) string {
	switch t.State {
	case engine.StateError:
		return "error"
	case engine.StateMoving:
		return "moving"
	case engine.StateStopped:
		if t.Complete {
			return "stoppedUP"
		}
		return "stoppedDL"
	case engine.StateQueued:
		if t.Complete {
			return "queuedUP"
		}
		return "queuedDL"
	case engine.StateChecking:
		if t.Complete {
			return "checkingUP"
		}
		return "checkingDL"
	case engine.StateMetadata:
		if t.Forced {
			return "forcedMetaDL"
		}
		return "metaDL"
	case engine.StateDownloading, engine.StateStalledDownloading:
		switch {
		case t.Forced:
			return "forcedDL"
		case t.State == engine.StateDownloading:
			return "downloading"
		default:
			return "stalledDL"
		}
	case engine.StateSeeding, engine.StateStalledSeeding:
		switch {
		case t.Forced:
			return "forcedUP"
		case t.State == engine.StateSeeding:
			return "uploading"
		default:
			return "stalledUP"
		}
	default:
		return "unknown"
	}
}

// Torrent is a torrents/info entry.
type Torrent struct {
	AddedOn                  int64   `json:"added_on"`
	AmountLeft               int64   `json:"amount_left"`
	AutoTMM                  bool    `json:"auto_tmm"`
	Availability             float64 `json:"availability"`
	Category                 string  `json:"category"`
	Comment                  string  `json:"comment"`
	Completed                int64   `json:"completed"`
	CompletionOn             int64   `json:"completion_on"`
	ConnectionsCount         int     `json:"connections_count"`
	ConnectionsLimit         int     `json:"connections_limit"`
	ContentPath              string  `json:"content_path"`
	CreatedBy                string  `json:"created_by"`
	CreationDate             int64   `json:"creation_date"`
	DlLimit                  int64   `json:"dl_limit"`
	Dlspeed                  int64   `json:"dlspeed"`
	DownloadPath             string  `json:"download_path"`
	Downloaded               int64   `json:"downloaded"`
	DownloadedSession        int64   `json:"downloaded_session"`
	ETA                      int64   `json:"eta"`
	FLPiecePrio              bool    `json:"f_l_piece_prio"`
	ForceStart               bool    `json:"force_start"`
	HasMetadata              bool    `json:"has_metadata"`
	Hash                     string  `json:"hash"`
	InactiveSeedingTimeLimit int64   `json:"inactive_seeding_time_limit"`
	InfohashV1               string  `json:"infohash_v1"`
	InfohashV2               string  `json:"infohash_v2"`
	LastActivity             int64   `json:"last_activity"`
	MagnetURI                string  `json:"magnet_uri"`
	MaxInactiveSeedingTime   int64   `json:"max_inactive_seeding_time"`
	MaxRatio                 float64 `json:"max_ratio"`
	MaxSeedingTime           int64   `json:"max_seeding_time"`
	Name                     string  `json:"name"`
	NumComplete              int     `json:"num_complete"`
	NumIncomplete            int     `json:"num_incomplete"`
	NumLeechs                int     `json:"num_leechs"`
	NumSeeds                 int     `json:"num_seeds"`
	PieceSize                int64   `json:"piece_size"`
	PiecesHave               int     `json:"pieces_have"`
	PiecesNum                int     `json:"pieces_num"`
	Popularity               float64 `json:"popularity"`
	Priority                 int64   `json:"priority"`
	Private                  bool    `json:"private"`
	Progress                 float64 `json:"progress"`
	Ratio                    float64 `json:"ratio"`
	RatioLimit               float64 `json:"ratio_limit"`
	Reannounce               int64   `json:"reannounce"`
	RootPath                 string  `json:"root_path"`
	SavePath                 string  `json:"save_path"`
	SeedingTime              int64   `json:"seeding_time"`
	SeedingTimeLimit         int64   `json:"seeding_time_limit"`
	SeenComplete             int64   `json:"seen_complete"`
	SeqDl                    bool    `json:"seq_dl"`
	ShareLimitAction         string  `json:"share_limit_action"`
	ShareLimitsMode          string  `json:"share_limits_mode"`
	Size                     int64   `json:"size"`
	State                    string  `json:"state"`
	SuperSeeding             bool    `json:"super_seeding"`
	Tags                     string  `json:"tags"`
	TimeActive               int64   `json:"time_active"`
	TotalSize                int64   `json:"total_size"`
	TotalWasted              int64   `json:"total_wasted"`
	Tracker                  string  `json:"tracker"`
	TrackersCount            int     `json:"trackers_count"`
	UpLimit                  int64   `json:"up_limit"`
	Uploaded                 int64   `json:"uploaded"`
	UploadedSession          int64   `json:"uploaded_session"`
	Upspeed                  int64   `json:"upspeed"`
}

func eta(t engine.Torrent) int64 {
	if t.ETA < 0 {
		return infinity
	}
	return t.ETA
}

func ratio(r float64) float64 {
	if math.IsNaN(r) || math.IsInf(r, 0) || r > 9999 {
		return 9999
	}
	return r
}

// effective resolves a per-torrent limit (-2 follows the global setting).
func effective[T int64 | float64](own, global T) T {
	if own == -2 {
		return global
	}
	return own
}

// queuePriority is qBittorrent's priority field: the queue position, 0 for
// torrents outside the queue (complete ones), and -1 with queueing off.
func queuePriority(t engine.Torrent, queueing bool) int64 {
	if !queueing {
		return -1
	}
	return t.QueuePosition
}

func (s *Server) torrent(t engine.Torrent) Torrent {
	st := s.settings.Get()
	completedOn := t.CompletedAt
	if completedOn == 0 {
		completedOn = -1
	}
	createdAt := t.CreatedAt
	if createdAt == 0 {
		createdAt = -1
	}
	avail, _ := s.availability(t)
	return Torrent{
		Availability: avail, Popularity: popularity(t),
		ConnectionsCount: t.Seeds + t.Peers, ConnectionsLimit: st.MaxConnectionsPerTorrent,
		CreatedBy: t.CreatedBy, CreationDate: createdAt, PieceSize: t.PieceSize, PiecesHave: t.PiecesHave,
		PiecesNum: t.Pieces,
		AddedOn:   t.AddedAt, AmountLeft: max(t.Size-t.Completed, 0), Category: t.Category,
		Comment: t.Comment, Completed: t.Completed, CompletionOn: completedOn, ContentPath: t.ContentPath,
		DlLimit: -1, Dlspeed: t.DownRate, Downloaded: t.Downloaded,
		DownloadedSession: t.SessionDown, ETA: eta(t), FLPiecePrio: t.FirstLast, ForceStart: t.Forced,
		HasMetadata: t.HasMetadata, Hash: t.Hash, InactiveSeedingTimeLimit: t.InactiveSeedingTimeLimit,
		InfohashV1: t.Hash, LastActivity: t.LastActive, MagnetURI: magnet(t),
		MaxInactiveSeedingTime: effective(t.InactiveSeedingTimeLimit, st.InactiveSeedingTimeLimit),
		MaxRatio:               effective(t.RatioLimit, st.RatioLimit),
		MaxSeedingTime:         effective(t.SeedingTimeLimit, st.SeedingTimeLimit),
		Name:                   t.Name, NumComplete: t.Seeds, NumIncomplete: max(t.SwarmPeers-t.Seeds, t.Peers),
		NumLeechs: t.Peers, NumSeeds: t.Seeds, Priority: queuePriority(t, st.QueueingEnabled), Private: t.Private, Progress: t.Progress,
		Ratio: ratio(t.Ratio), RatioLimit: t.RatioLimit, RootPath: t.RootPath, SavePath: t.SavePath,
		SeedingTime: t.SeedingTime, SeedingTimeLimit: t.SeedingTimeLimit, SeenComplete: completedOn,
		SeqDl: t.Sequential, ShareLimitAction: "Default", ShareLimitsMode: "Default", Size: t.Size,
		State: State(t), Tags: strings.Join(t.Tags, ", "), TimeActive: t.ActiveTime, TotalSize: t.TotalSize,
		Tracker: t.Tracker, TrackersCount: t.Trackers, UpLimit: -1, Uploaded: t.Uploaded,
		UploadedSession: t.SessionUp, Upspeed: t.UpRate,
	}
}

// magnet returns the magnet the torrent was added with, or one built from
// its hash and name.
func magnet(t engine.Torrent) string {
	if t.Magnet != "" {
		return t.Magnet
	}
	return "magnet:?xt=urn:btih:" + t.Hash + "&dn=" + url.QueryEscape(t.Name)
}

// matches reports whether a torrent passes a torrents/info filter.
func matches(t engine.Torrent, filter string) bool {
	state := State(t)
	switch filter {
	case "", "all":
		return true
	case "downloading":
		return !t.Complete && !t.Stopped
	case "seeding":
		return state == "uploading" || state == "stalledUP" || state == "forcedUP"
	case "completed":
		return t.Complete
	case "stopped", "paused":
		return t.Stopped
	case "running", "resumed":
		return !t.Stopped
	case "active":
		return t.DownRate > 0 || t.UpRate > 0
	case "inactive":
		return t.DownRate == 0 && t.UpRate == 0
	case "stalled":
		return state == "stalledDL" || state == "stalledUP"
	case "stalled_uploading":
		return state == "stalledUP"
	case "stalled_downloading":
		return state == "stalledDL"
	case "checking":
		return strings.HasPrefix(state, "checking")
	case "moving":
		return state == "moving"
	case "errored":
		return state == "error" || state == "missingFiles"
	default:
		return false
	}
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	q := r.Form
	want := map[string]bool{}
	for _, h := range hashes(r, "hashes") {
		want[h] = true
	}
	filter := q.Get("filter")
	category, byCategory := q.Get("category"), q.Has("category")
	tag, byTag := q.Get("tag"), q.Has("tag")
	private, byPrivate := q.Get("private"), q.Has("private")

	out := []Torrent{}
	for _, t := range s.engine.List() {
		switch {
		case len(want) > 0 && !want["all"] && !want[t.Hash]:
		case !matches(t, filter):
		case byCategory && t.Category != category:
		case byTag && tag == "" && len(t.Tags) > 0:
		case byTag && tag != "" && !slices.Contains(t.Tags, tag):
		case byPrivate && strconv.FormatBool(t.Private) != strings.ToLower(private):
		default:
			out = append(out, s.torrent(t))
		}
	}
	if field := q.Get("sort"); field != "" {
		sortBy(out, field, strings.EqualFold(q.Get("reverse"), "true"))
	}
	if offset, err := strconv.Atoi(q.Get("offset")); err == nil {
		if offset < 0 {
			offset = max(len(out)+offset, 0)
		}
		out = out[min(offset, len(out)):]
	}
	if limit, err := strconv.Atoi(q.Get("limit")); err == nil && limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	withFiles, _ := formBool(r, "includeFiles")
	withTrackers, _ := formBool(r, "includeTrackers")
	if !withFiles && !withTrackers {
		writeJSON(w, out)
		return
	}
	// qBittorrent 5.1 and 5.2 add the files and trackers lists on request.
	extended := make([]torrentWithLists, len(out))
	for i, t := range out {
		extended[i].Torrent = t
		et, ok := s.engine.Get(t.Hash)
		if !ok {
			continue
		}
		if withFiles {
			extended[i].Files, _ = s.fileObjects(et, nil)
		}
		if withTrackers {
			extended[i].Trackers, _ = s.trackerObjects(et, false)
		}
	}
	writeJSON(w, extended)
}

// torrentWithLists is a torrents/info entry with includeFiles or
// includeTrackers.
type torrentWithLists struct {
	Torrent
	Files    []map[string]any `json:"files,omitzero"`
	Trackers []map[string]any `json:"trackers,omitzero"`
}

// sortBy sorts by any field name of the response.
func sortBy(items []Torrent, field string, reverse bool) {
	keys := make([]map[string]any, len(items))
	for i, t := range items {
		b, _ := json.Marshal(t)
		_ = json.Unmarshal(b, &keys[i])
	}
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		c := compareAny(keys[a][field], keys[b][field])
		if reverse {
			return -c
		}
		return c
	})
	sorted := make([]Torrent, len(items))
	for i, j := range idx {
		sorted[i] = items[j]
	}
	copy(items, sorted)
}

func compareAny(a, b any) int {
	switch x := a.(type) {
	case float64:
		y, _ := b.(float64)
		return cmp.Compare(x, y)
	case string:
		y, _ := b.(string)
		return strings.Compare(strings.ToLower(x), strings.ToLower(y))
	case bool:
		y, _ := b.(bool)
		switch {
		case x == y:
			return 0
		case !x:
			return -1
		default:
			return 1
		}
	default:
		return 0
	}
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (engine.Torrent, bool) {
	h := strings.ToLower(strings.TrimSpace(r.FormValue("hash")))
	t, ok := s.engine.Get(h)
	if !ok {
		http.Error(w, "Not Found", http.StatusNotFound)
	}
	return t, ok
}

func (s *Server) properties(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	avail, _ := s.availability(t)
	minusOne := func(v int64) int64 {
		if v == 0 {
			return -1
		}
		return v
	}
	avg := func(total, secs int64) int64 {
		if secs <= 0 {
			return 0
		}
		return total / secs
	}
	pieceSize, pieces := t.PieceSize, int64(t.Pieces)
	if !t.HasMetadata {
		pieceSize, pieces = -1, -1
	}
	writeJSON(w, map[string]any{
		"hash": t.Hash, "name": t.Name, "infohash_v1": t.Hash, "infohash_v2": "",
		"save_path": t.SavePath, "download_path": "", "creation_date": minusOne(t.CreatedAt),
		"piece_size": pieceSize, "comment": t.Comment, "total_wasted": 0,
		"total_uploaded": t.Uploaded, "total_uploaded_session": t.SessionUp,
		"total_downloaded": t.Downloaded, "total_downloaded_session": t.SessionDown,
		// piecemeal has no per-torrent speed limits.
		"up_limit": -1, "dl_limit": -1,
		"time_elapsed": t.ActiveTime, "seeding_time": t.SeedingTime,
		"nb_connections": t.Seeds + t.Peers, "nb_connections_limit": s.settings.Get().MaxConnectionsPerTorrent,
		"share_ratio": ratio(t.Ratio), "addition_date": t.AddedAt, "completion_date": minusOne(t.CompletedAt),
		"created_by": t.CreatedBy, "dl_speed_avg": avg(t.Downloaded, t.ActiveTime), "dl_speed": t.DownRate,
		"eta": eta(t), "last_seen": minusOne(t.LastActive), "peers": t.Peers, "peers_total": t.SwarmPeers,
		"pieces_have": t.PiecesHave, "pieces_num": pieces, "reannounce": 0, "seeds": t.Seeds,
		"seeds_total": t.Seeds, "total_size": minusOne(t.TotalSize),
		"up_speed_avg": avg(t.Uploaded, t.ActiveTime), "up_speed": t.UpRate,
		"private": t.Private, "is_private": t.Private, "has_metadata": t.HasMetadata, "progress": t.Progress,
		"popularity": popularity(t), "availability": avail,
	})
}

// availability is libtorrent's distributed copies among connected peers,
// with the per-piece counts it comes from. A complete torrent, or one not
// running, reports -1, as qBittorrent does for seeds.
func (s *Server) availability(t engine.Torrent) (float64, []int) {
	if t.Complete {
		return -1, nil
	}
	counts, err := s.engine.PieceAvailability(t.Hash)
	if err != nil || counts == nil {
		return -1, nil
	}
	return engine.DistributedCopies(counts), counts
}

// popularity is qBittorrent's ratio per month of activity.
func popularity(t engine.Torrent) float64 {
	if t.ActiveTime <= 0 {
		return 0
	}
	const month = 30 * 24 * 60 * 60
	return ratio(t.Ratio) * month / float64(t.ActiveTime)
}

func (s *Server) pieceAvailability(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	_, counts := s.availability(t)
	if counts == nil {
		counts = []int{}
	}
	writeJSON(w, counts)
}

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var only map[int]bool
	if v := r.FormValue("indexes"); v != "" {
		only = map[int]bool{}
		for _, p := range strings.Split(v, "|") {
			if i, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
				only[i] = true
			}
		}
	}
	out, err := s.fileObjects(t, only)
	if err != nil {
		s.engineError(w, err)
		return
	}
	writeJSON(w, out)
}

// fileObjects is a torrent's files as torrents/files lists them, all of
// them when only is nil.
func (s *Server) fileObjects(t engine.Torrent, only map[int]bool) ([]map[string]any, error) {
	files, err := s.engine.Files(t.Hash)
	if err != nil {
		return nil, err
	}
	_, counts := s.availability(t)
	out := []map[string]any{}
	for _, f := range files {
		if only != nil && !only[f.Index] {
			continue
		}
		last := max(f.PieceEnd-1, f.PieceStart)
		// As qBittorrent: -1 for a seed, else the share of the file's
		// pieces some connected peer has.
		avail := -1.0
		if counts != nil {
			avail = engine.AvailableFraction(counts, f.PieceStart, f.PieceEnd)
		}
		out = append(out, map[string]any{
			"index": f.Index, "name": f.Path, "size": f.Size, "progress": f.Progress, "priority": int(f.Priority),
			"is_seed": f.Progress >= 1, "piece_range": []int{f.PieceStart, last}, "availability": avail,
		})
	}
	return out, nil
}

// Tracker status codes.
const (
	trackerDisabled     = 0
	trackerNotContacted = 1
)

func (s *Server) trackers(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	out, err := s.trackerObjects(t, true)
	if err != nil {
		s.engineError(w, err)
		return
	}
	writeJSON(w, out)
}

// trackerObjects is a torrent's trackers as torrents/trackers lists them,
// with the DHT, PeX and LSD rows first when pseudo is set.
func (s *Server) trackerObjects(t engine.Torrent, pseudo bool) ([]map[string]any, error) {
	trs, err := s.engine.Trackers(t.Hash)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	if pseudo {
		st := s.settings.Get()
		row := func(name string, enabled bool) map[string]any {
			status := trackerDisabled
			if enabled && !t.Private {
				status = 2
			}
			return map[string]any{
				"url": name, "status": status, "tier": -1, "num_peers": 0, "num_seeds": 0, "num_leeches": 0,
				"num_downloaded": 0, "msg": "",
			}
		}
		out = append(out, row("** [DHT] **", st.DHT), row("** [PeX] **", st.PEX), row("** [LSD] **", false))
	}
	// anacrolix doesn't expose announce results (#44).
	for _, tr := range trs {
		out = append(out, map[string]any{
			"url": tr.URL, "status": trackerNotContacted, "tier": tr.Tier, "num_peers": -1, "num_seeds": -1,
			"num_leeches": -1, "num_downloaded": -1, "msg": "",
		})
	}
	return out, nil
}

func (s *Server) pieceStates(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	states, err := s.engine.Pieces(t.Hash)
	if err != nil {
		s.engineError(w, err)
		return
	}
	out := make([]int, len(states))
	for i, c := range states {
		switch c {
		case '1':
			out[i] = 1
		case '2':
			out[i] = 2
		}
	}
	writeJSON(w, out)
}

// addResult is torrents/add's answer since WebAPI 2.14.
type addResult struct {
	Success int      `json:"success_count"`
	Pending int      `json:"pending_count"`
	Failure int      `json:"failure_count"`
	Added   []string `json:"added_torrent_ids"`
}

// add adds torrents from URLs and uploaded files. It returns only once
// they are in the engine: the *arrs query the torrent right after.
func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	base := engine.AddParams{
		Category:      r.FormValue("category"),
		ContentLayout: r.FormValue("contentLayout"),
		Tags:          list(r.FormValue("tags")),
		Name:          strings.TrimSpace(r.FormValue("rename")),
	}
	if p := r.FormValue("savepath"); p != "" {
		base.SavePath = s.absolute(p)
	}
	// stopped replaced paused in WebAPI 2.11.
	if v, ok := formBool(r, "stopped"); ok {
		base.Stopped = &v
	} else if v, ok := formBool(r, "paused"); ok {
		base.Stopped = &v
	}
	base.Sequential, _ = formBool(r, "sequentialDownload")
	base.FirstLastPiece, _ = formBool(r, "firstLastPiecePrio")
	if v, ok := formBool(r, "seedMode"); ok {
		base.SkipChecking = v
	} else {
		base.SkipChecking, _ = formBool(r, "skip_checking")
	}
	forced, _ := formBool(r, "forced")
	if v, err := strconv.ParseFloat(r.FormValue("ratioLimit"), 64); err == nil {
		base.RatioLimit = &v
	}
	if v, err := strconv.ParseInt(r.FormValue("seedingTimeLimit"), 10, 64); err == nil {
		base.SeedingTimeLimit = &v
	}
	if v, err := strconv.ParseInt(r.FormValue("inactiveSeedingTimeLimit"), 10, 64); err == nil {
		base.InactiveSeedingTimeLimit = &v
	}
	// dlLimit and upLimit are ignored: piecemeal has no per-torrent speed
	// limits.
	if base.Category != "" {
		if !slices.ContainsFunc(s.engine.Categories(), func(c engine.Category) bool { return c.Name == base.Category }) {
			// qBittorrent creates a category it doesn't know.
			if err := s.engine.PutCategory(r.Context(), engine.Category{Name: base.Category}, true); err != nil && !errors.Is(err, engine.ErrExists) {
				s.engineError(w, err)
				return
			}
		}
	}

	var sources []engine.AddParams
	var invalid int
	for _, u := range strings.Split(r.FormValue("urls"), "\n") {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		p := base
		if strings.HasPrefix(strings.ToLower(u), "magnet:") {
			p.Magnet = u
		} else {
			res, err := s.fetcher.Get(r.Context(), u)
			if err != nil {
				s.log.Warn("torrents/add: fetching URL", "error", err)
				invalid++
				continue
			}
			if res.Magnet != "" {
				p.Magnet = res.Magnet
			} else if p.Magnet, p.Metainfo, err = engine.ParseSource(res.Body); err != nil {
				invalid++
				continue
			}
		}
		sources = append(sources, p)
	}
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["torrents"] {
			f, err := fh.Open()
			if err != nil {
				invalid++
				continue
			}
			b, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil {
				invalid++
				continue
			}
			p := base
			if p.Magnet, p.Metainfo, err = engine.ParseSource(b); err != nil {
				invalid++
				continue
			}
			sources = append(sources, p)
		}
	}
	if len(sources) == 0 && invalid == 0 {
		http.Error(w, "Missing required parameter: urls or torrents", http.StatusBadRequest)
		return
	}

	res := addResult{Added: []string{}, Failure: invalid}
	duplicates := 0
	for _, p := range sources {
		hash, err := s.engine.Add(r.Context(), p)
		switch {
		case errors.Is(err, engine.ErrExists):
			duplicates++
			res.Failure++
		case err != nil:
			s.log.Warn("torrents/add", "error", err)
			res.Failure++
		default:
			res.Success++
			res.Added = append(res.Added, hash)
			if forced {
				_ = s.engine.SetForced(r.Context(), true, hash)
			}
		}
	}
	switch {
	case res.Success > 0:
		writeJSON(w, res)
	case duplicates > 0:
		// The *arrs read 409 as "the client already has this release".
		http.Error(w, "Conflict", http.StatusConflict)
	default:
		http.Error(w, "Torrent file is not valid", http.StatusUnsupportedMediaType)
	}
}

// absolute resolves a path relative to the default save path, as
// qBittorrent does, and strips a trailing slash.
func (s *Server) absolute(p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(s.settings.Get().DefaultSavePath, p)
	}
	return filepath.Clean(p)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	del, _ := formBool(r, "deleteFiles")
	hs := s.known(hashes(r, "hashes"))
	if len(hs) == 0 {
		done(w)
		return
	}
	if err := s.engine.Remove(r.Context(), del, hs...); err != nil {
		// Data the *arr already moved away on import is not an error.
		s.log.Warn("torrents/delete", "error", err)
	}
	done(w)
}

// hashesAction runs an engine action on the torrents in hashes.
func (s *Server) hashesAction(fn func(context.Context, ...string) error) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		s.withHashes(w, r, func(hs []string) error { return fn(r.Context(), hs...) })
	}
}

func (s *Server) setForceStart(w http.ResponseWriter, r *http.Request) {
	s.withHashes(w, r, func(hs []string) error {
		v, _ := formBool(r, "value")
		return s.engine.SetForced(r.Context(), v, hs...)
	})
}

// withHashes runs fn on the known torrents in hashes and answers 200.
func (s *Server) setComment(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "hashes") {
		return
	}
	comment := r.FormValue("comment")
	s.withHashes(w, r, func(hs []string) error {
		return s.engine.SetComment(r.Context(), comment, resolveAll(s, hs)...)
	})
}

func (s *Server) withHashes(w http.ResponseWriter, r *http.Request, fn func(hs []string) error) {
	s.withHashesStatus(w, r, http.StatusOK, fn)
}

// withHashesStatus runs fn on the known torrents in hashes and answers
// status.
func (s *Server) withHashesStatus(w http.ResponseWriter, r *http.Request, status int, fn func(hs []string) error) {
	if hs := s.known(hashes(r, "hashes")); len(hs) > 0 {
		if err := fn(hs); err != nil {
			s.engineError(w, err)
			return
		}
	}
	w.WriteHeader(status)
}

func (s *Server) setShareLimits(w http.ResponseWriter, r *http.Request) {
	s.withHashes(w, r, func(hs []string) error {
		l := engine.ShareLimits{RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2}
		if v, err := strconv.ParseFloat(r.FormValue("ratioLimit"), 64); err == nil {
			l.RatioLimit = v
		}
		if v, err := strconv.ParseInt(r.FormValue("seedingTimeLimit"), 10, 64); err == nil {
			l.SeedingTimeLimit = v
		}
		if v, err := strconv.ParseInt(r.FormValue("inactiveSeedingTimeLimit"), 10, 64); err == nil {
			l.InactiveSeedingTimeLimit = v
		}
		return s.engine.SetShareLimits(r.Context(), l, hs...)
	})
}

// resolveAll expands "all" to every hash.
func resolveAll(s *Server, hs []string) []string {
	if len(hs) == 1 && hs[0] == "all" {
		hs = nil
		for _, t := range s.engine.List() {
			hs = append(hs, t.Hash)
		}
	}
	return hs
}

func (s *Server) toggle(sequential bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		s.withHashes(w, r, func(hs []string) error {
			var errs []error
			for _, h := range resolveAll(s, hs) {
				t, ok := s.engine.Get(h)
				if !ok {
					continue
				}
				if sequential {
					errs = append(errs, s.engine.SetSequential(r.Context(), !t.Sequential, h))
				} else {
					errs = append(errs, s.engine.SetFirstLastPiece(r.Context(), !t.FirstLast, h))
				}
			}
			return errors.Join(errs...)
		})
	}
}

func (s *Server) filePrio(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	prio, err := strconv.Atoi(r.FormValue("priority"))
	if err != nil {
		http.Error(w, "Priority is not valid", http.StatusBadRequest)
		return
	}
	var ids []int
	for _, p := range strings.Split(r.FormValue("id"), "|") {
		i, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			http.Error(w, "File IDs are not valid", http.StatusBadRequest)
			return
		}
		ids = append(ids, i)
	}
	if !t.HasMetadata {
		http.Error(w, "Torrent's metadata has not yet downloaded", http.StatusConflict)
		return
	}
	if err := s.engine.SetFilePriority(r.Context(), t.Hash, ids, engine.Priority(prio)); err != nil {
		if errors.Is(err, engine.ErrInvalid) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.engineError(w, err)
		return
	}
	done(w)
}

func (s *Server) rename(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if err := s.engine.Rename(r.Context(), t.Hash, r.FormValue("name")); err != nil {
		if errors.Is(err, engine.ErrInvalid) {
			http.Error(w, "Incorrect torrent name", http.StatusConflict)
			return
		}
		s.engineError(w, err)
		return
	}
	done(w)
}
