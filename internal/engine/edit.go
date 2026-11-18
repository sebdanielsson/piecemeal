package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/anacrolix/torrent/metainfo"
)

// currentTrackersLocked is the torrent's announce list: the edited one, or
// the torrent file's, or the magnet's.
func (m *managed) currentTrackers() [][]string {
	if m.trackers != nil {
		return m.trackers
	}
	return m.meta.announce
}

// TrackerMap returns every announce URL in use with the torrents using it.
func (e *Engine) TrackerMap() map[string][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string][]string{}
	for _, m := range e.torrents {
		for _, tier := range m.currentTrackers() {
			for _, u := range tier {
				if !slices.Contains(out[u], m.rec.InfoHash) {
					out[u] = append(out[u], m.rec.InfoHash)
				}
			}
		}
	}
	for _, hs := range out {
		slices.Sort(hs)
	}
	return out
}

func (m *managed) currentWebSeeds() []string {
	if m.webSeeds != nil {
		return m.webSeeds
	}
	if m.rec.Metainfo != nil {
		if mi, err := metainfo.Load(bytes.NewReader(m.rec.Metainfo)); err == nil {
			return mi.UrlList
		}
	}
	return nil
}

func validAnnounceURL(u string) error {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return fmt.Errorf("%w: tracker URL %q", ErrInvalid, u)
	}
	switch p.Scheme {
	case "http", "https", "udp", "ws", "wss":
		return nil
	default:
		return fmt.Errorf("%w: tracker URL scheme %q", ErrInvalid, p.Scheme)
	}
}

// setTrackersLocked stores and applies a new announce list.
func (e *Engine) setTrackersLocked(m *managed, al [][]string) {
	al = slices.DeleteFunc(al, func(tier []string) bool { return len(tier) == 0 })
	if al == nil {
		al = [][]string{}
	}
	m.trackers = al
	announce := m.meta.announce
	m.meta.tracker, m.meta.trackers = "", 0
	m.setMagnetTrackers(al)
	m.meta.announce = announce
	if m.t != nil {
		m.t.ModifyTrackers(al)
	}
}

// AddTrackers adds announce URLs, each in a tier of its own after the
// existing ones. URLs the torrent already has are skipped.
func (e *Engine) AddTrackers(ctx context.Context, hash string, urls []string) error {
	for _, u := range urls {
		if err := validAnnounceURL(u); err != nil {
			return err
		}
	}
	return e.each(ctx, []string{hash}, func(m *managed) error {
		al := cloneTiers(m.currentTrackers())
		for _, u := range urls {
			if !slices.ContainsFunc(al, func(tier []string) bool { return slices.Contains(tier, u) }) {
				al = append(al, []string{u})
			}
		}
		e.setTrackersLocked(m, al)
		return nil
	})
}

// EditTracker replaces one announce URL with another.
func (e *Engine) EditTracker(ctx context.Context, hash, from, to string) error {
	if err := validAnnounceURL(to); err != nil {
		return err
	}
	return e.each(ctx, []string{hash}, func(m *managed) error {
		al := cloneTiers(m.currentTrackers())
		found := false
		for _, tier := range al {
			if slices.Contains(tier, to) {
				return fmt.Errorf("%w: tracker %q", ErrExists, to)
			}
			if i := slices.Index(tier, from); i >= 0 {
				tier[i] = to
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%w: tracker %q", ErrNotFound, from)
		}
		e.setTrackersLocked(m, al)
		return nil
	})
}

// RemoveTrackers removes announce URLs.
func (e *Engine) RemoveTrackers(ctx context.Context, hash string, urls []string) error {
	return e.each(ctx, []string{hash}, func(m *managed) error {
		al := cloneTiers(m.currentTrackers())
		removed := false
		for i, tier := range al {
			n := len(tier)
			al[i] = slices.DeleteFunc(tier, func(u string) bool { return slices.Contains(urls, u) })
			removed = removed || len(al[i]) != n
		}
		if !removed {
			return fmt.Errorf("%w: none of the trackers", ErrNotFound)
		}
		e.setTrackersLocked(m, al)
		return nil
	})
}

func cloneTiers(al [][]string) [][]string {
	out := make([][]string, len(al))
	for i, tier := range al {
		out[i] = slices.Clone(tier)
	}
	return out
}

// WebSeeds returns a torrent's web seed URLs.
func (e *Engine) WebSeeds(hash string) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m, err := e.get(hash)
	if err != nil {
		return nil, err
	}
	return append([]string{}, m.currentWebSeeds()...), nil
}

// SetWebSeeds replaces a torrent's web seed URLs.
func (e *Engine) SetWebSeeds(ctx context.Context, hash string, urls []string) error {
	for _, u := range urls {
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return fmt.Errorf("%w: web seed URL %q", ErrInvalid, u)
		}
	}
	return e.each(ctx, []string{hash}, func(m *managed) error {
		m.webSeeds = append([]string{}, urls...)
		if m.t != nil {
			m.t.RemoveWebSeeds()
			m.t.AddWebSeeds(m.webSeeds)
		}
		return nil
	})
}

// Move moves a torrent's data to another save path. It returns once the
// move has started; the torrent is in the moving state until it's done,
// and resumes if it was running.
func (e *Engine) Move(ctx context.Context, hash, savePath string) error {
	dest, err := e.confine(savePath)
	if err != nil {
		return err
	}
	e.mu.Lock()
	m, err := e.get(hash)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	if m.moving {
		e.mu.Unlock()
		return fmt.Errorf("%w: already being moved", ErrInvalid)
	}
	if dest == m.rec.SavePath {
		e.mu.Unlock()
		return nil
	}
	if m.info == nil {
		// Nothing on disk yet: just change where it will go.
		m.rec.SavePath = dest
		e.recordPathsLocked(m)
		if m.t != nil {
			e.detachLocked(m)
			err = e.attachLocked(m)
		}
		e.saveLocked(m)
		e.refreshLocked(m)
		e.mu.Unlock()
		return err
	}
	running := m.t != nil
	m.moving = true
	e.detachLocked(m)
	src := m.rec.SavePath
	rel := m.pathsOf().relativePaths(m.info)
	e.refreshLocked(m)
	e.mu.Unlock()

	e.wg.Go(func() {
		err := moveFiles(src, dest, rel)
		e.mu.Lock()
		defer e.mu.Unlock()
		m.moving = false
		if e.torrents[m.hash] != m {
			return // removed meanwhile
		}
		if err != nil {
			e.log.Error("moving torrent data", "hash", m.rec.InfoHash, "from", src, "to", dest, "error", err)
			m.setError(fmt.Errorf("moving to %s: %w", dest, err))
		} else {
			m.rec.SavePath = dest
			e.recordPathsLocked(m)
			e.log.Info("torrent data moved", "hash", m.rec.InfoHash, "from", src, "to", dest)
		}
		if running && err == nil {
			if err := e.attachLocked(m); err != nil {
				m.setError(err)
			}
		}
		e.saveLocked(m)
		e.refreshLocked(m)
	})
	_ = ctx
	return nil
}

// moveFiles moves files from one directory to another, both given relative
// paths, renaming where it can and copying across filesystems. Files that
// don't exist yet are skipped. Emptied source folders are removed.
func moveFiles(src, dest string, rel []string) error {
	var moved []string
	for _, r := range rel {
		from := filepath.Join(src, filepath.FromSlash(r))
		to := filepath.Join(dest, filepath.FromSlash(r))
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			return fmt.Errorf("%w: %s", ErrExists, to)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil { //nolint:gosec // download folders are shared with media apps
			return err
		}
		err := os.Rename(from, to)
		if errors.Is(err, syscall.EXDEV) {
			err = copyFile(from, to)
			if err == nil {
				err = os.Remove(from)
			}
		}
		if err != nil {
			return err
		}
		moved = append(moved, from)
	}
	removeEmptyDirs(src, moved)
	return nil
}

func copyFile(from, to string) (err error) {
	in, err := os.Open(from) //nolint:gosec // a torrent's own file
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm()) //nolint:gosec // a torrent's own file
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(to)
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// SetTrackers replaces a torrent's announce list. Each inner list is a
// tier; empty tiers are dropped.
func (e *Engine) SetTrackers(ctx context.Context, hash string, tiers [][]string) error {
	for _, tier := range tiers {
		for _, u := range tier {
			if err := validAnnounceURL(u); err != nil {
				return err
			}
		}
	}
	return e.each(ctx, []string{hash}, func(m *managed) error {
		e.setTrackersLocked(m, cloneTiers(tiers))
		return nil
	})
}
