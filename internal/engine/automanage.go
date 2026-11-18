package engine

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sebdanielsson/piecemeal/internal/settings"
)

// Automatic torrent management, as qBittorrent calls it: an automatically
// managed torrent's save path and incomplete folder follow its category,
// and change when its category does, when the category's paths do, and
// when the default save path or the incomplete folder setting do. Three
// settings decide whether such a change moves the torrent's data
// (relocateOnCategoryChange, relocateOnCategoryPathChange and
// relocateOnDefaultPathChange), or instead switches the torrent to manual
// management, where its paths stay as they are. Moving a torrent or
// changing its incomplete folder by hand switches it to manual management
// too.

// A category's incomplete folder modes.
const (
	// IncompleteDefault follows the global setting.
	IncompleteDefault = "default"
	// IncompleteOff keeps the category's torrents in their save path.
	IncompleteOff = "off"
	// IncompleteOn uses the category's own incomplete folder.
	IncompleteOn = "on"
)

func validIncompleteMode(m string) bool {
	return m == IncompleteDefault || m == IncompleteOff || m == IncompleteOn
}

// resolveUnder makes a path absolute against base, as qBittorrent resolves
// a relative category path against the default save path, and confines it.
func (e *Engine) resolveUnder(base, p string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return e.confine(p)
}

// autoPathsLocked returns the save path and incomplete folder ("" for none)
// of an automatically managed torrent in category: the category's save
// path, or a folder named after the category in the default save path; and
// the category's incomplete folder or the global one.
func (e *Engine) autoPathsLocked(category string, s settings.Settings) (save, incomplete string, err error) {
	c, ok := e.cats[category]
	if category == "" || !ok {
		c = NewCategory("")
	}
	switch {
	case c.Name == "":
		save, err = e.confine(s.DefaultSavePath)
	case c.SavePath != "":
		save, err = e.resolveUnder(s.DefaultSavePath, c.SavePath)
	default:
		save, err = e.resolveUnder(s.DefaultSavePath, filepath.FromSlash(c.Name))
	}
	if err != nil {
		return "", "", fmt.Errorf("save path of category %q: %w", category, err)
	}
	incomplete, err = e.categoryIncompleteLocked(c, s, true)
	if err != nil {
		return "", "", fmt.Errorf("incomplete folder of category %q: %w", category, err)
	}
	return save, incomplete, nil
}

// categoryIncompleteLocked returns the incomplete folder a category gives
// its torrents, "" for none. A category with its own folder gives that,
// relative to the global one, or a folder named after the category in the
// global one when it names no path; this applies even with the global
// setting off. A category that follows the global setting gives the global
// folder while it's on: for automatically managed torrents, the folder
// named after the category in it.
func (e *Engine) categoryIncompleteLocked(c Category, s settings.Settings, auto bool) (string, error) {
	switch {
	case c.IncompletePathMode == IncompleteOff:
		return "", nil
	case c.IncompletePathMode == IncompleteOn && c.IncompletePath != "":
		return e.resolveUnder(s.IncompletePath, c.IncompletePath)
	case c.IncompletePathMode != IncompleteOn && !s.IncompletePathEnabled:
		return "", nil
	case c.Name == "" || (c.IncompletePathMode != IncompleteOn && !auto):
		return e.confine(s.IncompletePath)
	default:
		return e.resolveUnder(s.IncompletePath, filepath.FromSlash(c.Name))
	}
}

// followCategoryLocked moves an automatically managed torrent to its
// category's paths, if they differ from its own. A torrent being moved
// follows once the move is done.
func (e *Engine) followCategoryLocked(m *managed, s settings.Settings) {
	if m.moving {
		return
	}
	save, incomplete, err := e.autoPathsLocked(m.rec.Category, s)
	if err != nil {
		e.log.Warn("automatic management: switching to manual", "hash", m.rec.InfoHash, "error", err)
		m.rec.AutoManaged = false
		m.dirty = true
		return
	}
	if save == m.rec.SavePath && incomplete == m.rec.IncompletePath {
		return
	}
	if err := e.placeLocked(m, m.placeFor(save, incomplete)); err != nil {
		m.setError(err)
	}
}

// pathsChangedLocked handles a change of the paths automatically managed
// torrents follow, for those that match: with relocate on they follow,
// otherwise they switch to manual management and stay where they are.
func (e *Engine) pathsChangedLocked(s settings.Settings, relocate bool, match func(*managed) bool) []*managed {
	var changed []*managed
	for _, m := range e.torrents {
		if !m.rec.AutoManaged || !match(m) {
			continue
		}
		save, incomplete, err := e.autoPathsLocked(m.rec.Category, s)
		if err == nil && save == m.rec.SavePath && incomplete == m.rec.IncompletePath {
			continue
		}
		if relocate && err == nil {
			e.followCategoryLocked(m, s)
		} else {
			m.rec.AutoManaged = false
			m.dirty = true
		}
		changed = append(changed, m)
	}
	return changed
}

// SetAutoManaged turns automatic management on or off. Turning it on moves
// the torrents to their category's paths; turning it off leaves them where
// they are.
func (e *Engine) SetAutoManaged(ctx context.Context, on bool, hashes ...string) error {
	s := e.settings.Get()
	return e.each(ctx, hashes, func(m *managed) error {
		if !on {
			m.rec.AutoManaged = false
			return nil
		}
		save, incomplete, err := e.autoPathsLocked(m.rec.Category, s)
		if err != nil {
			return err
		}
		m.rec.AutoManaged = true
		if m.moving || (save == m.rec.SavePath && incomplete == m.rec.IncompletePath) {
			return nil // a torrent being moved follows once it's done
		}
		return e.placeLocked(m, m.placeFor(save, incomplete))
	})
}

// onPathSettingsChangedLocked applies a change of the default save path or
// the incomplete folder setting to automatically managed torrents: they
// follow a new path, or switch to manual management, as
// relocateOnDefaultPathChange says. Turning the incomplete folder on or off
// alone they always follow, as in qBittorrent.
func (e *Engine) onPathSettingsChangedLocked(old, cur settings.Settings) {
	pathChanged := old.DefaultSavePath != cur.DefaultSavePath || old.IncompletePath != cur.IncompletePath
	if !pathChanged && old.IncompletePathEnabled == cur.IncompletePathEnabled {
		return
	}
	relocate := cur.RelocateOnDefaultPathChange || !pathChanged
	changed := e.pathsChangedLocked(cur, relocate, func(*managed) bool { return true })
	for _, m := range changed {
		e.saveLocked(m)
	}
	e.refreshLocked(changed...)
}

// addPathsLocked decides a new torrent's save path and incomplete folder:
// an automatically managed torrent takes its category's, whatever the
// request says. Otherwise the request's come first, then what the category
// sets (its save path, its own incomplete folder or none), then the
// defaults: the default save path, and the global incomplete folder while
// it's on. A relative incomplete folder is inside the global one.
func (e *Engine) addPathsLocked(p AddParams, auto bool, s settings.Settings) (save, incomplete string, err error) {
	if auto {
		return e.autoPathsLocked(p.Category, s)
	}
	c, ok := e.cats[p.Category]
	if p.Category == "" || !ok {
		c = NewCategory("")
	}
	save = p.SavePath
	if save == "" && c.SavePath != "" {
		save = c.SavePath
		if !filepath.IsAbs(save) {
			save = filepath.Join(s.DefaultSavePath, save)
		}
	}
	if save == "" {
		save = s.DefaultSavePath
	}
	if save, err = e.confine(save); err != nil {
		return "", "", err
	}
	switch {
	case p.IncompletePath == nil:
		incomplete, err = e.categoryIncompleteLocked(c, s, false)
	case *p.IncompletePath != "":
		incomplete, err = e.resolveUnder(s.IncompletePath, *p.IncompletePath)
	}
	if err != nil {
		return "", "", fmt.Errorf("incomplete folder: %w", err)
	}
	return save, incomplete, nil
}

// IncompleteFromQBittorrent turns qBittorrent's useDownloadPath and
// downloadPath into AddParams.IncompletePath: none when useDownloadPath is
// false, the path when there is one (even without useDownloadPath), the
// global folder when useDownloadPath is true without a path, and the
// default when neither is given.
func IncompleteFromQBittorrent(use *bool, path, global string) *string {
	switch {
	case use != nil && !*use:
		none := ""
		return &none
	case path != "":
		return &path
	case use != nil:
		return &global
	default:
		return nil
	}
}
