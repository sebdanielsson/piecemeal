package qbit

import (
	"errors"
	"log/slog"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// transfer/*

func (s *Server) speedLimitsMode(w http.ResponseWriter, r *http.Request) {
	mode := "0"
	if s.settings.Get().AltSpeedEnabled {
		mode = "1"
	}
	s.text(mode)(w, r)
}

func (s *Server) toggleSpeedLimitsMode(w http.ResponseWriter, r *http.Request) {
	s.setSetting(w, r, map[string]any{"altSpeedEnabled": !s.settings.Get().AltSpeedEnabled})
}

func (s *Server) setSpeedLimitsMode(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mode, err := strconv.Atoi(r.FormValue("mode"))
	if err != nil {
		http.Error(w, "mode must be 0 or 1", http.StatusBadRequest)
		return
	}
	// qBittorrent 5.3.0 takes any value but 0 as on.
	s.setSetting(w, r, map[string]any{"altSpeedEnabled": mode != 0})
}

// limitSetting is the setting behind the global limit endpoints: always
// the normal limit, whatever the alternative mode.
func limitSetting(download bool) string {
	if download {
		return "downloadLimit"
	}
	return "uploadLimit"
}

func (s *Server) globalLimit(download bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		st := s.settings.Get()
		v := st.UploadLimit
		if download {
			v = st.DownloadLimit
		}
		s.text(strconv.FormatInt(max(v, 0), 10))(w, r)
	}
}

func (s *Server) setGlobalLimit(download bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, err := strconv.ParseInt(r.FormValue("limit"), 10, 64)
		if err != nil {
			http.Error(w, "limit must be a number", http.StatusBadRequest)
			return
		}
		s.setSetting(w, r, map[string]any{limitSetting(download): roundLimit(limit)})
	}
}

var speedLimitKeys = map[string]string{
	"dl_limit": "downloadLimit", "up_limit": "uploadLimit",
	"alt_dl_limit": "altDownloadLimit", "alt_up_limit": "altUploadLimit",
}

func (s *Server) getSpeedLimits(w http.ResponseWriter, _ *http.Request) {
	st := s.settings.Get()
	writeJSON(w, map[string]int64{
		"dl_limit": max(st.DownloadLimit, 0), "up_limit": max(st.UploadLimit, 0),
		"alt_dl_limit": max(st.AltDownloadLimit, 0), "alt_up_limit": max(st.AltUploadLimit, 0),
	})
}

func (s *Server) setSpeedLimits(w http.ResponseWriter, r *http.Request) {
	values := map[string]any{}
	var missing []string
	for _, k := range slices.Sorted(maps.Keys(speedLimitKeys)) {
		v := r.FormValue(k)
		if v == "" {
			missing = append(missing, k)
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, k+" must be a number", http.StatusBadRequest)
			return
		}
		values[speedLimitKeys[k]] = roundLimit(n)
	}
	if len(missing) > 0 {
		http.Error(w, "Missing required parameters: "+strings.Join(missing, ", "), http.StatusBadRequest)
		return
	}
	if _, err := s.settings.Set(r.Context(), values); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	noContent(w)
}

// setSetting changes settings from a qBittorrent endpoint.
func (s *Server) setSetting(w http.ResponseWriter, r *http.Request, values map[string]any) {
	if _, err := s.settings.Set(r.Context(), values); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	done(w)
}

func (s *Server) banPeers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	banned := strings.Split(s.settings.Get().BannedIPs, "\n")
	for _, p := range strings.Split(r.FormValue("peers"), "|") {
		p = strings.TrimSpace(p)
		if ap, err := netip.ParseAddrPort(p); err == nil {
			p = ap.Addr().String()
		} else if host, _, err := net.SplitHostPort(p); err == nil {
			p = host
		}
		if _, err := netip.ParseAddr(p); err != nil || slices.Contains(banned, p) {
			continue
		}
		banned = append(banned, p)
	}
	banned = slices.DeleteFunc(banned, func(b string) bool { return strings.TrimSpace(b) == "" })
	s.setSetting(w, r, map[string]any{"bannedIPs": strings.Join(banned, "\n")})
}

// log/*

// Log message types, a bitmask in qBittorrent.
const (
	logNormal   = 1
	logInfo     = 2
	logWarning  = 4
	logCritical = 8
)

// logMain returns piecemeal's log, to admins as in the native API.
func (s *Server) logMain(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	q := r.Form
	want := func(name string) bool {
		v, err := strconv.ParseBool(q.Get(name))
		return err != nil || v // default true
	}
	after := int64(-1)
	if v, err := strconv.ParseInt(q.Get("last_known_id"), 10, 64); err == nil {
		after = v
	}
	out := []map[string]any{}
	for _, e := range s.logs.Since(max(after, 0), slog.LevelInfo) {
		var typ int
		switch e.Level {
		case "ERROR":
			typ = logCritical
		case "WARN":
			typ = logWarning
		default:
			typ = logNormal
		}
		if (typ == logNormal && !want("normal")) || (typ == logWarning && !want("warning")) ||
			(typ == logCritical && !want("critical")) {
			continue
		}
		msg := e.Message
		if e.Attrs != "" {
			msg += " " + e.Attrs
		}
		out = append(out, map[string]any{"id": e.ID, "message": msg, "timestamp": e.Time.Unix(), "type": typ})
	}
	writeJSON(w, out)
}

func (s *Server) logPeers(w http.ResponseWriter, r *http.Request) {
	after := int64(-1)
	if v, err := strconv.ParseInt(r.FormValue("last_known_id"), 10, 64); err == nil {
		after = v
	}
	out := []map[string]any{}
	for _, e := range s.engine.PeerLog(after) {
		out = append(out, map[string]any{
			"id": e.ID, "ip": e.IP, "timestamp": e.Time.Unix(), "blocked": e.Blocked, "reason": e.Reason,
		})
	}
	writeJSON(w, out)
}

// app/*

// notSupported answers endpoints for features piecemeal doesn't have with
// 409, qBittorrent's status for a valid request it can't carry out.
func (s *Server) notSupported(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Not supported by piecemeal", http.StatusConflict)
}

func (s *Server) networkInterfaceList(w http.ResponseWriter, _ *http.Request) {
	out := []map[string]string{}
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		out = append(out, map[string]string{"name": i.Name, "value": i.Name})
	}
	writeJSON(w, out)
}

func (s *Server) networkInterfaceAddressList(w http.ResponseWriter, r *http.Request) {
	out := []string{}
	name := r.FormValue("iface")
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if name != "" && i.Name != name {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil {
				out = append(out, p.Addr().String())
			}
		}
	}
	writeJSON(w, out)
}

// getDirectoryContent lists a directory inside the allowed roots, for path
// pickers.
func (s *Server) getDirectoryContent(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir := r.FormValue("dirPath")
	if !filepath.IsAbs(dir) {
		http.Error(w, "dirPath must be an absolute path", http.StatusBadRequest)
		return
	}
	dir, err := s.engine.Confine(dir)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	mode := r.FormValue("mode")
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	withMetadata, _ := formBool(r, "withMetadata")
	paths := []string{}
	objects := []map[string]any{}
	for _, e := range entries {
		isDir := e.IsDir()
		if (mode == "dirs" && !isDir) || (mode == "files" && isDir) {
			continue
		}
		if !withMetadata {
			paths = append(paths, filepath.Join(dir, e.Name()))
			continue
		}
		// qBittorrent 5.1's objects: dates in Unix seconds, size for files.
		info, err := e.Info()
		if err != nil {
			continue
		}
		created, accessed := fileTimes(info)
		obj := map[string]any{
			"name": e.Name(), "type": "file", "creation_date": created.Unix(),
			"last_access_date": accessed.Unix(), "last_modification_date": info.ModTime().Unix(),
		}
		if isDir {
			obj["type"] = "dir"
		} else {
			obj["size"] = info.Size()
		}
		objects = append(objects, obj)
	}
	if withMetadata {
		writeJSON(w, objects)
		return
	}
	writeJSON(w, paths)
}

// torrents/*

func (s *Server) webseeds(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	urls, err := s.engine.WebSeeds(t.Hash)
	if err != nil {
		s.engineError(w, err)
		return
	}
	out := make([]map[string]string, 0, len(urls))
	for _, u := range urls {
		out = append(out, map[string]string{"url": u})
	}
	writeJSON(w, out)
}

func (s *Server) editWebSeeds(how string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := parseForm(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		t, ok := s.lookup(w, r)
		if !ok {
			return
		}
		urls, _ := s.engine.WebSeeds(t.Hash)
		switch how {
		case "add":
			for _, u := range strings.Split(r.FormValue("urls"), "|") {
				if u = strings.TrimSpace(u); u != "" && !slices.Contains(urls, u) {
					urls = append(urls, u)
				}
			}
		case "edit":
			i := slices.Index(urls, r.FormValue("origUrl"))
			if i < 0 {
				http.Error(w, "Web seed not found", http.StatusConflict)
				return
			}
			urls[i] = r.FormValue("newUrl")
		case "remove":
			remove := strings.Split(r.FormValue("urls"), "|")
			urls = slices.DeleteFunc(urls, func(u string) bool { return slices.Contains(remove, u) })
		}
		if err := s.engine.SetWebSeeds(r.Context(), t.Hash, urls); err != nil {
			if errors.Is(err, engine.ErrInvalid) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.engineError(w, err)
			return
		}
		done(w)
	}
}

// trackerTargets resolves the hash parameter of addTrackers and
// removeTrackers: one hash, several separated by |, or all (also * for
// removeTrackers). single is set when one hash was named, which must then
// exist.
func (s *Server) trackerTargets(w http.ResponseWriter, r *http.Request) (hs []string, single, ok bool) {
	v := strings.TrimSpace(r.FormValue("hash"))
	if v == "" {
		http.Error(w, "Missing required parameter: hash", http.StatusBadRequest)
		return nil, false, false
	}
	if v == "*" {
		v = "all"
	}
	r.Form.Set("hash", v)
	all := hashes(r, "hash")
	if len(all) == 1 && all[0] != "all" {
		t, ok := s.lookup(w, r)
		return []string{t.Hash}, true, ok
	}
	return resolveAll(s, s.known(all)), false, true
}

// trackerURLs splits a list of tracker URLs. VueTorrent percent-encodes
// each URL before joining them, so encoded ones are decoded.
func trackerURLs(v, sep string) []string {
	var out []string
	for _, u := range strings.Split(v, sep) {
		u = strings.TrimSpace(u)
		if !strings.Contains(u, "://") {
			if d, err := url.QueryUnescape(u); err == nil {
				u = d
			}
		}
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

// eachTracker runs fn on the target torrents. With one torrent named its
// error is the answer; with several, torrents fn fails on are skipped.
func (s *Server) eachTracker(w http.ResponseWriter, r *http.Request, fn func(hash string) error) {
	hs, single, ok := s.trackerTargets(w, r)
	if !ok {
		return
	}
	for _, h := range hs {
		err := fn(h)
		if err != nil && (single || errors.Is(err, engine.ErrInvalid)) {
			s.trackerError(w, err)
			return
		}
	}
	noContent(w)
}

func (s *Server) addTrackers(w http.ResponseWriter, r *http.Request) {
	urls := trackerURLs(r.FormValue("urls"), "\n")
	s.eachTracker(w, r, func(h string) error { return s.engine.AddTrackers(r.Context(), h, urls) })
}

func (s *Server) editTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	// The old URL is url since WebAPI 2.13 and origUrl before; clients send
	// both.
	from := r.FormValue("url")
	if from == "" {
		from = r.FormValue("origUrl")
	}
	if err := s.engine.EditTracker(r.Context(), t.Hash, from, r.FormValue("newUrl")); err != nil {
		s.trackerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeTrackers(w http.ResponseWriter, r *http.Request) {
	urls := trackerURLs(r.FormValue("urls"), "|")
	s.eachTracker(w, r, func(h string) error { return s.engine.RemoveTrackers(r.Context(), h, urls) })
}

func (s *Server) trackerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, engine.ErrExists), errors.Is(err, engine.ErrNotFound):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		s.engineError(w, err)
	}
}

func (s *Server) addPeers(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var peers []string
	failed := 0
	for _, p := range strings.Split(r.FormValue("peers"), "|") {
		if _, err := netip.ParseAddrPort(strings.TrimSpace(p)); err == nil {
			peers = append(peers, strings.TrimSpace(p))
		} else {
			failed++
		}
	}
	if len(peers) == 0 {
		http.Error(w, "None of the supplied peers are valid", http.StatusBadRequest)
		return
	}
	hs := resolveAll(s, s.known(hashes(r, "hashes")))
	if len(hs) > 0 {
		if err := s.engine.AddPeers(r.Context(), peers, hs...); err != nil {
			s.engineError(w, err)
			return
		}
	}
	// Per torrent, how many peers were added and how many weren't valid.
	out := map[string]map[string]int{}
	for _, h := range hs {
		out[h] = map[string]int{"added": len(peers), "failed": failed}
	}
	writeJSON(w, out)
}

// setLocation moves torrents' data and turns automatic management off,
// as in qBittorrent 5.3.0. A relative location is refused.
func (s *Server) setLocation(w http.ResponseWriter, r *http.Request) {
	loc := strings.TrimSpace(r.FormValue("location"))
	switch {
	case loc == "":
		http.Error(w, "Save path cannot be empty", http.StatusBadRequest)
		return
	case !filepath.IsAbs(loc):
		http.Error(w, "Cannot make save path", http.StatusConflict)
		return
	case !s.engine.PathAllowed(loc):
		http.Error(w, "Cannot write to directory", http.StatusForbidden)
		return
	}
	var errs []error
	for _, h := range resolveAll(s, s.known(hashes(r, "hashes"))) {
		errs = append(errs, s.engine.Move(r.Context(), h, filepath.Clean(loc)))
	}
	if err := errors.Join(errs...); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	done(w)
}

// manualTorrents are the known torrents in the id parameter that aren't
// automatically managed: setSavePath and setDownloadPath leave those alone,
// as qBittorrent 5.3.0 does.
func (s *Server) manualTorrents(r *http.Request) []engine.Torrent {
	var out []engine.Torrent
	for _, h := range resolveAll(s, s.known(hashes(r, "id"))) {
		if t, ok := s.engine.Get(h); ok && !t.AutoManaged {
			out = append(out, t)
		}
	}
	return out
}

// pathParam checks setSavePath's and setDownloadPath's path: absolute,
// inside the allowed roots. It answers and returns false otherwise.
func (s *Server) pathParam(w http.ResponseWriter, p string) bool {
	switch {
	case !filepath.IsAbs(p):
		http.Error(w, "Cannot create target directory", http.StatusConflict)
		return false
	case !s.engine.PathAllowed(p):
		http.Error(w, "Cannot write to directory", http.StatusForbidden)
		return false
	}
	return true
}

// setSavePath changes the save path of torrents that aren't automatically
// managed; data in the save path moves with it.
func (s *Server) setSavePath(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "id", "path") {
		return
	}
	p := strings.TrimSpace(r.FormValue("path"))
	if p == "" {
		http.Error(w, "Save path cannot be empty", http.StatusBadRequest)
		return
	}
	if !s.pathParam(w, p) {
		return
	}
	var errs []error
	for _, t := range s.manualTorrents(r) {
		errs = append(errs, s.engine.Move(r.Context(), t.Hash, filepath.Clean(p)))
	}
	if err := errors.Join(errs...); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	done(w)
}

// setDownloadPath changes the incomplete folder of torrents that aren't
// automatically managed; data still in the old one moves. An empty path
// sets each torrent's save path, as qBittorrent 5.3.0 does.
func (s *Server) setDownloadPath(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "id", "path") {
		return
	}
	p := strings.TrimSpace(r.FormValue("path"))
	if p != "" && !s.pathParam(w, p) {
		return
	}
	var errs []error
	for _, t := range s.manualTorrents(r) {
		dest := filepath.Clean(p)
		if p == "" {
			dest = t.SavePath
		}
		errs = append(errs, s.engine.SetIncompletePath(r.Context(), dest, t.Hash))
	}
	if err := errors.Join(errs...); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	done(w)
}

// setAutoManagement turns automatic torrent management on, moving the
// torrents to their category's paths, or off. An enable that isn't a
// boolean changes nothing.
func (s *Server) setAutoManagement(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "hashes", "enable") {
		return
	}
	on, err := strconv.ParseBool(r.FormValue("enable"))
	if err != nil {
		done(w)
		return
	}
	s.withHashes(w, r, func(hs []string) error { return s.engine.SetAutoManaged(r.Context(), on, hs...) })
}

func (s *Server) renamePath(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if err := s.engine.RenamePath(t.Hash, r.FormValue("oldPath"), r.FormValue("newPath")); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	done(w)
}

// queue answers the queue position endpoints: 409 while queueing is off,
// as qBittorrent does.
func (s *Server) queue(how string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.settings.Get().QueueingEnabled {
			_ = parseForm(w, r)
			http.Error(w, "Torrent queueing must be enabled", http.StatusConflict)
			return
		}
		s.withHashes(w, r, func(hs []string) error { return s.engine.QueueMove(r.Context(), how, hs...) })
	}
}

func (s *Server) torrentLimits() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := parseForm(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// piecemeal has no per-torrent speed limits: none applies.
		out := map[string]int64{}
		for _, h := range resolveAll(s, s.known(hashes(r, "hashes"))) {
			out[h] = 0
		}
		writeJSON(w, out)
	}
}

func (s *Server) count(w http.ResponseWriter, r *http.Request) {
	s.text(strconv.Itoa(len(s.engine.List())))(w, r)
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	b, err := s.engine.TorrentFile(t.Hash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", attachment(t.Name, t.Hash, ".torrent"))
	_, _ = w.Write(b)
}

// attachment is a Content-Disposition naming the file after the torrent,
// or its hash when the name is unusable.
func attachment(name, hash, ext string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name != "" {
		if v := mime.FormatMediaType("attachment", map[string]string{"filename": name + ext}); v != "" {
			return v
		}
	}
	return mime.FormatMediaType("attachment", map[string]string{"filename": hash + ext})
}

// downloadFile serves a complete file of a torrent, named by its index or
// its path as in torrents/files.
func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	want := r.FormValue("file")
	if want == "" {
		http.Error(w, "Missing required parameter: file", http.StatusBadRequest)
		return
	}
	files, err := s.engine.Files(t.Hash)
	if err != nil {
		s.engineError(w, err)
		return
	}
	index := slices.IndexFunc(files, func(f engine.File) bool { return f.Path == want })
	if n, err := strconv.Atoi(want); index < 0 && err == nil {
		index = n
	}
	path, err := s.engine.CompleteFile(t.Hash, index)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			s.engineError(w, err)
		} else {
			http.Error(w, err.Error(), http.StatusConflict)
		}
		return
	}
	f, err := os.Open(path) //nolint:gosec // CompleteFile confines the path to the allowed roots
	if err != nil {
		http.Error(w, "file not found on disk", http.StatusConflict)
		return
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.Error(w, "file not found on disk", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", attachment(filepath.Base(path), t.Hash, ""))
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// sslParameters reports that no torrent has SSL parameters: SSL torrents
// are a libtorrent extension piecemeal doesn't support.
func (s *Server) sslParameters(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookup(w, r); ok {
		writeJSON(w, map[string]string{"ssl_certificate": "", "ssl_private_key": "", "ssl_dh_params": ""})
	}
}

func (s *Server) pieceHashes(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	hs, err := s.engine.PieceHashes(t.Hash)
	if err != nil {
		s.engineError(w, err)
		return
	}
	writeJSON(w, hs)
}

func (s *Server) setTags(w http.ResponseWriter, r *http.Request) {
	s.withHashesStatus(w, r, http.StatusNoContent, func(hs []string) error {
		want := list(r.FormValue("tags"))
		var errs []error
		for _, h := range resolveAll(s, hs) {
			t, ok := s.engine.Get(h)
			if !ok {
				continue
			}
			var remove []string
			for _, tag := range t.Tags {
				if !slices.Contains(want, tag) {
					remove = append(remove, tag)
				}
			}
			if len(remove) > 0 {
				errs = append(errs, s.engine.RemoveTags(r.Context(), remove, h))
			}
			if len(want) > 0 {
				errs = append(errs, s.engine.AddTags(r.Context(), want, h))
			}
		}
		return errors.Join(errs...)
	})
}

// torrentcreator/*

func (s *Server) creatorAddTask(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := engine.CreateParams{
		SourcePath: r.FormValue("sourcePath"), Comment: r.FormValue("comment"), Source: r.FormValue("source"),
	}
	p.Private, _ = formBool(r, "private")
	p.StartSeeding, _ = formBool(r, "startSeeding")
	if v, err := strconv.ParseInt(r.FormValue("pieceSize"), 10, 64); err == nil {
		p.PieceSize = v
	}
	// One tracker per line; a blank line starts a new tier.
	tier := []string{}
	for _, l := range strings.Split(r.FormValue("trackers"), "\n") {
		if l = strings.TrimSpace(l); l == "" {
			if len(tier) > 0 {
				p.Trackers = append(p.Trackers, tier)
				tier = []string{}
			}
			continue
		}
		tier = append(tier, l)
	}
	if len(tier) > 0 {
		p.Trackers = append(p.Trackers, tier)
	}
	for _, u := range strings.Split(r.FormValue("urlSeeds"), "\n") {
		if u = strings.TrimSpace(u); u != "" {
			p.WebSeeds = append(p.WebSeeds, u)
		}
	}
	id, err := s.engine.CreateTorrent(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"taskID": id})
}

func (s *Server) creatorStatus(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("taskID")
	out := []map[string]any{}
	stamp := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return t.Format(time.RFC1123)
	}
	for _, t := range s.engine.CreateTasks() {
		if id != "" && t.ID != id {
			continue
		}
		status := map[string]string{"queued": "Queued", "running": "Running", "finished": "Finished", "failed": "Failed"}[t.Status]
		trackers := []string{}
		for _, tier := range t.Params.Trackers {
			trackers = append(trackers, tier...)
		}
		out = append(out, map[string]any{
			"taskID": t.ID, "sourcePath": t.Params.SourcePath, "pieceSize": t.Params.PieceSize,
			"private": t.Params.Private, "timeAdded": t.AddedAt.Format(time.RFC1123), "timeStarted": stamp(t.StartedAt),
			"timeFinished": stamp(t.FinishedAt), "status": status, "progress": t.Progress * 100,
			"errorMessage": t.Error, "comment": t.Params.Comment, "source": t.Params.Source, "trackers": trackers,
			"urlSeeds": t.Params.WebSeeds, "format": "v1",
		})
	}
	if id != "" && len(out) == 0 {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	writeJSON(w, out)
}

func (s *Server) creatorTorrentFile(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b, err := s.engine.CreatedTorrent(r.FormValue("taskID"))
	switch {
	case errors.Is(err, engine.ErrNotFound):
		http.Error(w, "Not Found", http.StatusNotFound)
	case err != nil:
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(b) //nolint:gosec // a .torrent file, served as one
	}
}

func (s *Server) creatorDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.engine.DeleteCreateTask(r.FormValue("taskID")); err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	done(w)
}
