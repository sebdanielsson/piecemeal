package engine

import (
	"context"
	"fmt"
)

// Where a torrent's data is. Every torrent has a save path, where its data
// ends up, and may have an incomplete folder (qBittorrent's download path),
// where its data stays until it completes. Its record says which of the two
// holds the data now (InIncompletePath):
//
//   - A torrent added with an incomplete folder downloads there, unless its
//     data is already in the save path.
//   - Once complete, its data moves to the save path, in the background, as
//     with any move; the torrent is in the moving state meanwhile. Stopped
//     torrents move too.
//   - Changing the incomplete folder moves the data of a torrent still in
//     its incomplete folder to the new one, or to the save path when there
//     is none. A torrent that never completed but is in its save path moves
//     to the new incomplete folder.
//   - Changing the save path moves the data of a torrent that is in its
//     save path; a torrent in its incomplete folder stays there and moves to
//     the new save path once complete.
//
// Checks, renames, removal with data, and content_path all look where the
// data is.

// dataDir is the folder that holds the torrent's data now.
func (m *managed) dataDir() string {
	if m.rec.InIncompletePath && m.rec.IncompletePath != "" {
		return m.rec.IncompletePath
	}
	return m.rec.SavePath
}

// placement is a torrent's save path and incomplete folder, and which of
// them its data is in.
type placement struct {
	save, incomplete string
	inIncomplete     bool
}

func (p placement) dataDir() string {
	if p.inIncomplete && p.incomplete != "" {
		return p.incomplete
	}
	return p.save
}

// placeFor returns where a torrent with these paths keeps its data: in the
// incomplete folder while it has one and hasn't completed yet.
func (m *managed) placeFor(save, incomplete string) placement {
	p := placement{save: save, incomplete: incomplete}
	if incomplete != "" {
		p.inIncomplete = m.rec.InIncompletePath ||
			(m.rec.CompletedAt == 0 && (m.info == nil || !m.complete()))
	}
	return p
}

func (m *managed) setPlacement(p placement) {
	if m.rec.SavePath != p.save || m.rec.IncompletePath != p.incomplete || m.rec.InIncompletePath != p.inIncomplete {
		m.dirty = true
	}
	m.rec.SavePath, m.rec.IncompletePath, m.rec.InIncompletePath = p.save, p.incomplete, p.inIncomplete
}

// placeLocked gives a torrent new paths and moves its data where they say
// it belongs. Paths must be confined already. A move runs in the
// background: the torrent is in the moving state until it's done, and runs
// again afterwards if it was running. The paths change once the data is
// there; if the move fails, they stay and the torrent shows the error.
func (e *Engine) placeLocked(m *managed, p placement) error {
	if m.moving {
		return fmt.Errorf("%w: the torrent is being moved", ErrInvalid)
	}
	if p.incomplete == "" {
		p.inIncomplete = false
	}
	src, dest := m.dataDir(), p.dataDir()
	if src == dest || m.info == nil {
		// Nothing to move: the data stays where it is, or there is none
		// yet. A magnet still fetching its metadata opens its storage
		// once it has it, so it goes to the new place.
		m.setPlacement(p)
		e.recordPathsLocked(m)
		if src != dest && m.t != nil {
			e.detachLocked(m)
			if err := e.attachLocked(m); err != nil {
				m.setError(err)
			}
		}
		e.saveLocked(m)
		e.refreshLocked(m)
		return nil
	}
	running := m.t != nil
	m.moving = true
	e.detachLocked(m)
	rel := m.pathsOf().relativePaths(m.info)
	e.refreshLocked(m)

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
			m.setPlacement(p)
			e.recordPathsLocked(m)
			e.log.Info("torrent data moved", "hash", m.rec.InfoHash, "from", src, "to", dest)
		}
		if running && err == nil {
			if err := e.attachLocked(m); err != nil {
				m.setError(err)
			}
		}
		// Paths that changed while the data moved, such as the category of
		// an automatically managed torrent.
		if err == nil && m.rec.AutoManaged {
			e.followCategoryLocked(m, e.settings.Get())
		}
		// A check the move interrupted carries on, or the next one starts.
		e.scheduleChecksLocked()
		e.saveLocked(m)
		e.refreshLocked(m)
	})
	return nil
}

// awaitingMove reports whether a complete torrent's data is still in
// its incomplete folder, about to move to its save path.
func (m *managed) awaitingMove() bool {
	return m.rec.InIncompletePath && m.rec.IncompletePath != "" && !m.moving && m.rec.Error == "" &&
		m.info != nil && m.check == checkNone && m.complete()
}

// moveCompletedLocked moves a complete torrent's data from its incomplete
// folder to its save path.
func (e *Engine) moveCompletedLocked(m *managed) {
	if !m.awaitingMove() {
		return
	}
	e.log.Info("torrent complete, moving it to its save path", "hash", m.rec.InfoHash, "from", m.rec.IncompletePath,
		"to", m.rec.SavePath)
	if err := e.placeLocked(m, placement{save: m.rec.SavePath, incomplete: m.rec.IncompletePath}); err != nil {
		m.setError(err)
	}
}

// Move moves a torrent's data to another save path and turns automatic
// management off, as qBittorrent's setLocation does. It returns once the
// move has started; the torrent is in the moving state until it's done,
// and resumes if it was running. A torrent still in its incomplete folder
// keeps its data there and moves to the new save path once complete.
func (e *Engine) Move(ctx context.Context, hash, savePath string) error {
	dest, err := e.confine(savePath)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	m, err := e.get(hash)
	if err != nil {
		return err
	}
	if m.moving {
		return fmt.Errorf("%w: already being moved", ErrInvalid)
	}
	if m.rec.AutoManaged {
		m.rec.AutoManaged = false
		m.dirty = true
	}
	if dest == m.rec.SavePath {
		e.saveLocked(m)
		e.refreshLocked(m)
		return nil
	}
	_ = ctx
	return e.placeLocked(m, m.placeFor(dest, m.rec.IncompletePath))
}

// SetIncompletePath changes torrents' incomplete folder; "" means none.
// Data still in the old folder moves to the new one, or to the save path
// without one, and a torrent that never completed moves into the new one.
// Automatic management turns off, as with Move.
func (e *Engine) SetIncompletePath(ctx context.Context, path string, hashes ...string) error {
	if path != "" {
		p, err := e.confine(path)
		if err != nil {
			return err
		}
		path = p
	}
	return e.each(ctx, hashes, func(m *managed) error {
		if m.moving {
			return fmt.Errorf("%w: the torrent is being moved", ErrInvalid)
		}
		m.rec.AutoManaged = false
		if path == m.rec.IncompletePath {
			return nil
		}
		return e.placeLocked(m, m.placeFor(m.rec.SavePath, path))
	})
}

// startInSavePathLocked keeps a new torrent's data in its save path instead
// of its incomplete folder when that's where the data already is: added
// with skip checking, or with data in the save path and none in the
// incomplete folder, such as a torrent added again after it was removed.
// It is checked there, and completes there.
func (e *Engine) startInSavePathLocked(m *managed, skipChecking bool) {
	if !m.rec.InIncompletePath || m.rec.CompletedAt != 0 {
		return
	}
	if skipChecking || (m.info != nil && !m.hasDataIn(m.rec.IncompletePath) && m.hasDataIn(m.rec.SavePath)) {
		m.rec.InIncompletePath = false
		m.dirty = true
	}
	e.recordPathsLocked(m)
}
