package qbit

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
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
	mode := r.FormValue("mode")
	if mode != "0" && mode != "1" {
		http.Error(w, "mode must be 0 or 1", http.StatusBadRequest)
		return
	}
	s.setSetting(w, r, map[string]any{"altSpeedEnabled": mode == "1"})
}

// limitSetting is the setting the global limit endpoints work on: the
// alternative one while the alternative limits are on, as in qBittorrent.
func (s *Server) limitSetting(download bool) string {
	alt := s.settings.Get().AltSpeedEnabled
	switch {
	case download && alt:
		return "altDownloadLimit"
	case download:
		return "downloadLimit"
	case alt:
		return "altUploadLimit"
	default:
		return "uploadLimit"
	}
}

func (s *Server) globalLimit(download bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		t := s.engine.Transfer()
		v := t.UploadLimit
		if download {
			v = t.DownloadLimit
		}
		s.text(strconv.FormatInt(v, 10))(w, r)
	}
}

func (s *Server) setGlobalLimit(download bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := parseForm(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		limit, err := strconv.ParseInt(r.FormValue("limit"), 10, 64)
		if err != nil {
			http.Error(w, "limit must be a number", http.StatusBadRequest)
			return
		}
		s.setSetting(w, r, map[string]any{s.limitSetting(download): max(limit, 0)})
	}
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
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

func (s *Server) logMain(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
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
		out = append(out, map[string]any{"id": e.ID, "message": msg, "timestamp": e.Time.UnixMilli(), "type": typ})
	}
	writeJSON(w, out)
}

func (s *Server) logPeers(w http.ResponseWriter, _ *http.Request) {
	// piecemeal doesn't log blocked or banned peer connections.
	writeJSON(w, []any{})
}

// app/*

func (s *Server) notSupported(w http.ResponseWriter, r *http.Request) {
	_ = parseForm(w, r)
	http.Error(w, "Not supported by piecemeal", http.StatusMethodNotAllowed)
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
	name := r.URL.Query().Get("iface")
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
	if !s.engine.PathAllowed(dir) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	mode := r.FormValue("mode")
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	out := []string{}
	for _, e := range entries {
		isDir := e.IsDir()
		if (mode == "dirs" && !isDir) || (mode == "files" && isDir) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	writeJSON(w, out)
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

func (s *Server) addTrackers(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var urls []string
	for _, u := range strings.Split(r.FormValue("urls"), "\n") {
		if u = strings.TrimSpace(u); u != "" {
			urls = append(urls, u)
		}
	}
	if err := s.engine.AddTrackers(r.Context(), t.Hash, urls); err != nil {
		s.trackerError(w, err)
		return
	}
	done(w)
}

func (s *Server) editTracker(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	// origUrl and newUrl since WebAPI 2.10; before that, url and... the same.
	from, to := r.FormValue("origUrl"), r.FormValue("newUrl")
	if err := s.engine.EditTracker(r.Context(), t.Hash, from, to); err != nil {
		s.trackerError(w, err)
		return
	}
	done(w)
}

func (s *Server) removeTrackers(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if err := s.engine.RemoveTrackers(r.Context(), t.Hash, strings.Split(r.FormValue("urls"), "|")); err != nil {
		s.trackerError(w, err)
		return
	}
	done(w)
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
	for _, p := range strings.Split(r.FormValue("peers"), "|") {
		if _, err := netip.ParseAddrPort(strings.TrimSpace(p)); err == nil {
			peers = append(peers, strings.TrimSpace(p))
		}
	}
	if len(peers) == 0 {
		http.Error(w, "None of the supplied peers are valid", http.StatusBadRequest)
		return
	}
	s.withHashes(w, r, func(hs []string) error {
		return s.engine.AddPeers(r.Context(), peers, resolveAll(s, hs)...)
	})
}

func (s *Server) setLocation(param string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := parseForm(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		loc := strings.TrimSpace(r.FormValue(param))
		if loc == "" {
			http.Error(w, "Save path is empty", http.StatusBadRequest)
			return
		}
		loc = s.absolute(loc)
		if !s.engine.PathAllowed(loc) {
			http.Error(w, "Cannot write to directory", http.StatusForbidden)
			return
		}
		hashParam := "hashes"
		if param == "path" {
			hashParam = "id" // setSavePath
		}
		hs := s.known(hashes(r, hashParam))
		var errs []error
		for _, h := range resolveAll(s, hs) {
			errs = append(errs, s.engine.Move(r.Context(), h, loc))
		}
		if err := errors.Join(errs...); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		done(w)
	}
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

func (s *Server) torrentLimits(download bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := parseForm(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := map[string]int64{}
		for _, h := range resolveAll(s, s.known(hashes(r, "hashes"))) {
			if t, ok := s.engine.Get(h); ok {
				v := t.UploadLimit
				if download {
					v = t.DownloadLimit
				}
				out[h] = unlimited(v)
			}
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
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", t.Hash+".torrent"))
	_, _ = w.Write(b)
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
	s.withHashes(w, r, func(hs []string) error {
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

func (s *Server) acceptIgnored(w http.ResponseWriter, r *http.Request) {
	// Settings piecemeal doesn't have, such as automatic torrent management:
	// accepted and ignored so clients that set them keep working.
	_ = parseForm(w, r)
	done(w)
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
	id := r.URL.Query().Get("taskID")
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
