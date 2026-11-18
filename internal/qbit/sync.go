package qbit

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// sync/maindata and sync/torrentPeers answer with everything, or with what
// changed since a response the client already has, named by its rid.
// Each client (session, API key or Basic user) gets its own rids; the
// snapshots behind the last two rids sent are kept, so a client that lost
// a response and asks again with the older rid still catches up exactly.

// fields is a JSON object as decoded values, for comparing field by field.
type fields = map[string]any

type mainSnapshot struct {
	torrents   map[string]fields
	categories map[string]fields
	tags       []string
	trackers   map[string][]string
	server     fields
}

type peerSnapshot struct {
	peers map[string]fields
}

// syncState is one client's rids and snapshots for one stream: maindata,
// or one torrent's peers.
type syncState[S any] struct {
	rid   int
	snaps map[int]S
	used  time.Time
}

type syncStore[S any] struct {
	mu     sync.Mutex
	states map[string]*syncState[S]
}

const (
	maxSyncClients = 64
	syncIdle       = 30 * time.Minute
	maxRid         = 1_000_000
)

// exchange returns the snapshot the client named with rid, if it is one of
// the last two sent, and records next under a new rid, which it returns.
func (s *syncStore[S]) exchange(key string, rid int, next S) (prev S, found bool, newRid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = map[string]*syncState[S]{}
	}
	now := time.Now()
	st, ok := s.states[key]
	if !ok {
		s.evictLocked(now)
		st = &syncState[S]{snaps: map[int]S{}}
		s.states[key] = st
	}
	st.used = now
	if rid != 0 {
		prev, found = st.snaps[rid]
	}
	st.rid = st.rid%maxRid + 1
	// The client's next request names either the new rid or, if it lost
	// this response, the same one again: keep exactly those two.
	snaps := map[int]S{st.rid: next}
	if found {
		snaps[rid] = prev
	}
	st.snaps = snaps
	return prev, found, st.rid
}

func (s *syncStore[S]) evictLocked(now time.Time) {
	var oldest string
	var oldestAt time.Time
	for k, st := range s.states {
		if now.Sub(st.used) > syncIdle {
			delete(s.states, k)
			continue
		}
		if oldest == "" || st.used.Before(oldestAt) {
			oldest, oldestAt = k, st.used
		}
	}
	if len(s.states) >= maxSyncClients && oldest != "" {
		delete(s.states, oldest)
	}
}

// toFields turns a value into its JSON object, as decoded values.
func toFields(v any) fields {
	b, _ := json.Marshal(v)
	var f fields
	_ = json.Unmarshal(b, &f)
	return f
}

// structFieldNames caches, per struct type, each exported field's JSON
// name.
var structFieldNames sync.Map // reflect.Type → []string

// structFields is toFields for a flat struct of scalars, such as Torrent,
// without encoding it: maindata snapshots every torrent on each poll.
// Values keep their Go types, which encode the same.
func structFields(v any) fields {
	rv := reflect.ValueOf(v)
	names, ok := structFieldNames.Load(rv.Type())
	if !ok {
		t := rv.Type()
		n := make([]string, t.NumField())
		for i := range n {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !f.IsExported() || name == "-" {
				continue
			}
			n[i] = cmp.Or(name, f.Name)
		}
		names, _ = structFieldNames.LoadOrStore(t, n)
	}
	out := make(fields, rv.NumField())
	for i, name := range names.([]string) {
		if name != "" {
			out[name] = rv.Field(i).Interface()
		}
	}
	return out
}

// equalValue is reflect.DeepEqual with a fast path for the scalars that
// make up most fields.
func equalValue(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int64:
		bv, ok := b.(int64)
		return ok && av == bv
	case int:
		bv, ok := b.(int)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	}
	return reflect.DeepEqual(a, b)
}

// diffFields returns the fields of next that differ from prev.
func diffFields(prev, next fields) fields {
	out := fields{}
	for k, v := range next {
		if pv, ok := prev[k]; !ok || !equalValue(pv, v) {
			out[k] = v
		}
	}
	return out
}

// diffObjects compares two maps of objects: changed holds new objects
// whole and changed ones with their changed fields; removed lists keys
// that are gone.
func diffObjects(prev, next map[string]fields, whole bool) (changed map[string]fields, removed []string) {
	changed = map[string]fields{}
	for k, v := range next {
		p, ok := prev[k]
		switch {
		case !ok:
			changed[k] = v
		case whole:
			if !reflect.DeepEqual(p, v) {
				changed[k] = v
			}
		default:
			if d := diffFields(p, v); len(d) > 0 {
				changed[k] = d
			}
		}
	}
	for k := range prev {
		if _, ok := next[k]; !ok {
			removed = append(removed, k)
		}
	}
	slices.Sort(removed)
	return changed, removed
}

func ridParam(r *http.Request) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("rid")))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (s *Server) mainSnapshot() mainSnapshot {
	snap := mainSnapshot{
		torrents: map[string]fields{}, categories: map[string]fields{}, trackers: s.engine.TrackerMap(),
		tags: s.engine.Tags(),
	}
	for _, t := range s.engine.List() {
		f := structFields(s.torrent(t))
		delete(f, "hash")
		// Tracker status flags; piecemeal doesn't know announce results.
		f["has_tracker_warning"], f["has_tracker_error"], f["has_other_announce_error"] = false, false, false
		snap.torrents[t.Hash] = f
	}
	for _, c := range s.engine.Categories() {
		snap.categories[c.Name] = toFields(categoryObject(c))
	}
	snap.server = s.serverState()
	return snap
}

func (s *Server) serverState() fields {
	t := s.engine.Transfer()
	st := s.settings.Get()
	var allDown, allUp, peers int64
	for _, tor := range s.engine.List() {
		allDown += tor.Downloaded
		allUp += tor.Uploaded
		peers += int64(tor.Seeds + tor.Peers)
	}
	ratio := "0.00"
	if allDown > 0 {
		ratio = strconv.FormatFloat(float64(allUp)/float64(allDown), 'f', 2, 64)
	}
	return fields{
		"alltime_dl": allDown, "alltime_ul": allUp, "average_time_queue": 0, "connection_status": t.Connection,
		"dht_nodes": t.DHTNodes, "dl_info_data": t.SessionDownloaded, "dl_info_speed": t.DownRate,
		"dl_rate_limit": t.DownloadLimit, "free_space_on_disk": max(t.FreeSpace, 0), "global_ratio": ratio,
		"last_external_address_v4": "", "last_external_address_v6": "", "queued_io_jobs": 0,
		"queued_tracker_announces": 0, "queueing": st.QueueingEnabled, "read_cache_hits": "0",
		"read_cache_overload": "0", "refresh_interval": 1500, "request_latency": 0, "session_state": false,
		"total_buffers_size": 0, "total_peer_connections": peers, "total_queued_size": 0,
		"total_wasted_session": 0, "up_info_data": t.SessionUploaded, "up_info_speed": t.UpRate,
		"up_rate_limit": t.UploadLimit, "use_alt_speed_limits": st.AltSpeedEnabled, "write_cache_overload": "0",
	}
}

func (s *Server) maindata(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	next := s.mainSnapshot()
	// Normalise through JSON so later comparisons see the same types.
	next.server = toFields(next.server)
	prev, found, rid := s.mainSync.exchange(principal(r).ClientKey(), ridParam(r), next)
	out := map[string]any{"rid": rid}
	if !found {
		out["full_update"] = true
		out["torrents"] = next.torrents
		out["categories"] = next.categories
		out["tags"] = next.tags
		out["server_state"] = next.server
		if len(next.trackers) > 0 {
			out["trackers"] = next.trackers
		}
		writeJSON(w, out)
		return
	}
	if changed, removed := diffObjects(prev.torrents, next.torrents, false); len(changed) > 0 || len(removed) > 0 {
		if len(changed) > 0 {
			out["torrents"] = changed
		}
		if len(removed) > 0 {
			out["torrents_removed"] = removed
		}
	}
	if changed, removed := diffObjects(prev.categories, next.categories, true); len(changed) > 0 || len(removed) > 0 {
		if len(changed) > 0 {
			out["categories"] = changed
		}
		if len(removed) > 0 {
			out["categories_removed"] = removed
		}
	}
	var added, gone []string
	for _, t := range next.tags {
		if !slices.Contains(prev.tags, t) {
			added = append(added, t)
		}
	}
	for _, t := range prev.tags {
		if !slices.Contains(next.tags, t) {
			gone = append(gone, t)
		}
	}
	if len(added) > 0 {
		out["tags"] = added
	}
	if len(gone) > 0 {
		out["tags_removed"] = gone
	}
	// Trackers: each changed URL with its complete list of torrents.
	trackers := map[string][]string{}
	var trackersGone []string
	for u, hs := range next.trackers {
		if !slices.Equal(prev.trackers[u], hs) {
			trackers[u] = hs
		}
	}
	for u := range prev.trackers {
		if _, ok := next.trackers[u]; !ok {
			trackersGone = append(trackersGone, u)
		}
	}
	if len(trackers) > 0 {
		out["trackers"] = trackers
	}
	if len(trackersGone) > 0 {
		slices.Sort(trackersGone)
		out["trackers_removed"] = trackersGone
	}
	if d := diffFields(prev.server, next.server); len(d) > 0 {
		out["server_state"] = d
	}
	writeJSON(w, out)
}

func categoryObject(c engine.Category) map[string]any {
	return map[string]any{
		"name": c.Name, "savePath": c.SavePath, "download_path": nil,
		"ratio_limit": -2, "seeding_time_limit": -2, "inactive_seeding_time_limit": -2,
		"share_limit_action": "Default", "share_limits_mode": "Default",
	}
}

// Peer flags piecemeal can tell from anacrolix.
var flagDesc = map[string]string{
	"I": "incoming connection",
	"X": "peer from PEX",
	"H": "peer from DHT",
	"E": "encrypted traffic",
	"e": "encrypted handshake",
	"P": "μTP",
}

// peerKey is qBittorrent's peer key: ip:port, IPv6 in brackets.
func peerKey(addr string) (key, ip string, port int) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, addr, 0
	}
	port, _ = strconv.Atoi(p)
	return net.JoinHostPort(host, p), host, port
}

func peerFields(p engine.Peer) (string, fields) {
	key, ip, port := peerKey(p.Address)
	var flags []string
	switch p.Source {
	case "incoming":
		flags = append(flags, "I")
	case "pex":
		flags = append(flags, "X")
	case "dht":
		flags = append(flags, "H")
	}
	switch p.Encryption {
	case "rc4":
		flags = append(flags, "E")
	case "header":
		flags = append(flags, "e")
	}
	connection := "BT"
	switch {
	case strings.Contains(p.Network, "utp"):
		connection = "μTP"
		flags = append(flags, "P")
	case p.Network == "webseed":
		connection = "Web"
	}
	var desc []string
	for _, f := range flags {
		desc = append(desc, fmt.Sprintf("%s = %s", f, flagDesc[f]))
	}
	return key, toFields(map[string]any{
		"ip": ip, "port": port, "client": p.Client, "peer_id_client": p.Client, "connection": connection,
		"flags": strings.Join(flags, " "), "flags_desc": strings.Join(desc, "\n"), "country": "", "country_code": "",
		"progress": p.Progress, "relevance": 0, "dl_speed": p.DownRate, "up_speed": p.UpRate,
		"downloaded": p.Downloaded, "uploaded": p.Uploaded, "files": "",
	})
}

func (s *Server) torrentPeers(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.FormValue("hash") == "" {
		http.Error(w, "Missing required parameter: hash", http.StatusBadRequest)
		return
	}
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	peers, err := s.engine.Peers(t.Hash)
	if err != nil {
		s.engineError(w, err)
		return
	}
	next := peerSnapshot{peers: map[string]fields{}}
	for _, p := range peers {
		k, f := peerFields(p)
		next.peers[k] = f
	}
	prev, found, rid := s.peerSync.exchange(principal(r).ClientKey()+"|"+t.Hash, ridParam(r), next)
	out := map[string]any{"rid": rid, "show_flags": false}
	if !found {
		out["full_update"] = true
		out["peers"] = next.peers
		writeJSON(w, out)
		return
	}
	changed, removed := diffObjects(prev.peers, next.peers, false)
	if len(changed) > 0 {
		out["peers"] = changed
	}
	if len(removed) > 0 {
		out["peers_removed"] = removed
	}
	writeJSON(w, out)
}
