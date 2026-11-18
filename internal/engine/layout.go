package engine

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// Content layouts, as qBittorrent names them.
const (
	// LayoutOriginal keeps the torrent's own structure: multi-file torrents
	// in their root folder, single files directly in the save path.
	LayoutOriginal = "Original"
	// LayoutSubfolder also puts a single file in a folder named after it.
	LayoutSubfolder = "Subfolder"
	// LayoutNoSubfolder drops a multi-file torrent's root folder.
	LayoutNoSubfolder = "NoSubfolder"
)

func validLayout(l string) bool {
	return l == "" || l == LayoutOriginal || l == LayoutSubfolder || l == LayoutNoSubfolder
}

// torrentPaths is what the storage needs to place a torrent's files.
type torrentPaths struct {
	savePath string
	layout   string
	// renames maps a file's original path to its new one, both relative to
	// the save path with / separators.
	renames map[string]string
}

// originalPath is a file's path relative to the save path under the
// Original layout, with / separators: name/dir/file, or name for a
// single-file torrent.
func originalPath(info *metainfo.Info, fi metainfo.FileInfo) string {
	if !info.IsDir() {
		return info.BestName()
	}
	return strings.Join(append([]string{info.BestName()}, fi.BestPath()...), "/")
}

// layoutPath is a file's path relative to the save path under a layout,
// before renames.
func layoutPath(info *metainfo.Info, fi metainfo.FileInfo, layout string) string {
	switch {
	case layout == LayoutSubfolder && !info.IsDir():
		name := info.BestName()
		// A name that is only an extension, such as .nfo, keeps it.
		folder := cmp.Or(strings.TrimSuffix(name, path.Ext(name)), name)
		return folder + "/" + name
	case layout == LayoutNoSubfolder && info.IsDir():
		return strings.Join(fi.BestPath(), "/")
	default:
		return originalPath(info, fi)
	}
}

// relativePaths returns every file's current path relative to the save
// path, in the torrent's file order.
func (p torrentPaths) relativePaths(info *metainfo.Info) []string {
	files := info.UpvertedFiles()
	out := make([]string, len(files))
	for i, fi := range files {
		out[i] = p.path(info, fi)
	}
	return out
}

func (p torrentPaths) path(info *metainfo.Info, fi metainfo.FileInfo) string {
	if r, ok := p.renames[originalPath(info, fi)]; ok {
		return r
	}
	return layoutPath(info, fi, p.layout)
}

// contentAndRoot works out qBittorrent's content_path and root_path from
// the files' paths: content is the single file, or the common top folder,
// or the save path when files don't share one; root is the top folder, or
// empty when there is none.
func contentAndRoot(savePath string, rel []string) (content, root string) {
	if len(rel) == 0 {
		return savePath, ""
	}
	first := func(p string) (string, bool) {
		top, _, nested := strings.Cut(p, "/")
		return top, nested
	}
	top, nested := first(rel[0])
	shared := nested
	for _, p := range rel[1:] {
		t, n := first(p)
		if t != top || !n {
			shared = false
			break
		}
	}
	if len(rel) == 1 {
		content = filepath.Join(savePath, filepath.FromSlash(rel[0]))
		if nested {
			root = filepath.Join(savePath, top)
		}
		return content, root
	}
	if shared {
		root = filepath.Join(savePath, top)
		return root, root
	}
	return savePath, ""
}

// storagePaths sets up the anacrolix file storage to ask the engine where
// each torrent and file goes.
func (e *Engine) storagePaths(opts *storage.NewFileClientOpts) {
	// anacrolix calls these once per file while it opens a torrent's
	// storage; forgetInfo drops the entry when the torrent leaves the
	// client.
	opts.TorrentDirMaker = func(_ string, info *metainfo.Info, ih metainfo.Hash) string {
		e.infoOwners.Store(info, ih)
		if p, ok := e.paths.Load(ih); ok {
			return p.(torrentPaths).savePath
		}
		// Unreachable: paths are recorded before any torrent is added. Fail
		// closed into the data dir rather than anywhere else.
		return filepath.Join(e.cfg.DataDir, "orphaned", ih.HexString())
	}
	opts.FilePathMaker = func(o storage.FilePathMakerOpts) string {
		var tp torrentPaths
		if ih, ok := e.infoOwners.Load(o.Info); ok {
			if p, ok := e.paths.Load(ih); ok {
				tp = p.(torrentPaths)
			}
		}
		return filepath.FromSlash(tp.path(o.Info, *o.File))
	}
}

// forgetInfo drops the storage's record of a torrent's open info, once
// the torrent has left the client.
func (e *Engine) forgetInfo(ih metainfo.Hash) {
	e.infoOwners.Range(func(k, v any) bool {
		if v == ih {
			e.infoOwners.Delete(k)
		}
		return true
	})
}

// pathsOf returns a torrent's file placement.
func (m *managed) pathsOf() torrentPaths {
	return torrentPaths{savePath: m.rec.SavePath, layout: m.rec.ContentLayout, renames: m.renames}
}

// recordPathsLocked publishes a torrent's file placement to the storage.
func (e *Engine) recordPathsLocked(m *managed) {
	e.paths.Store(m.hash, m.pathsOf())
}

// cleanRelative checks a path relative to the save path from a client.
func cleanRelative(p string) (string, error) {
	p = strings.ReplaceAll(p, `\`, "/")
	c := path.Clean(p)
	if p == "" || c == "." || strings.ContainsRune(c, 0) || !filepath.IsLocal(c) {
		return "", fmt.Errorf("%w: path %q", ErrInvalid, p)
	}
	return c, nil
}

// RenamePath renames a file, or a folder and everything in it. Paths are
// relative to the save path with / separators, as torrents/files reports
// them. The files on disk move with it.
func (e *Engine) RenamePath(hash, oldPath, newPath string) error {
	oldPath, err := cleanRelative(oldPath)
	if err != nil {
		return err
	}
	newPath, err = cleanRelative(newPath)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	m, err := e.get(hash)
	if err != nil {
		return err
	}
	if m.info == nil {
		return fmt.Errorf("%w: metadata not received yet", ErrInvalid)
	}
	if m.moving {
		return fmt.Errorf("%w: the torrent is being moved", ErrInvalid)
	}
	paths := m.pathsOf()
	current := paths.relativePaths(m.info)
	files := m.info.UpvertedFiles()
	moves := map[int]string{}
	for i, p := range current {
		switch {
		case p == oldPath:
			moves[i] = newPath
		case strings.HasPrefix(p, oldPath+"/"):
			moves[i] = newPath + strings.TrimPrefix(p, oldPath)
		}
	}
	if len(moves) == 0 {
		return fmt.Errorf("%w: no file or folder %q in the torrent", ErrNotFound, oldPath)
	}
	for i, p := range current {
		if _, moving := moves[i]; moving {
			continue
		}
		for _, np := range moves {
			if p == np || strings.HasPrefix(p, np+"/") || strings.HasPrefix(np, p+"/") {
				return fmt.Errorf("%w: %q already exists", ErrExists, np)
			}
		}
	}
	// os.Rename replaces an existing file, which in a shared save path can
	// be another torrent's. The same file is fine: a case-only rename on a
	// case-insensitive filesystem.
	type rename struct{ from, to string }
	disk := make(map[int]rename, len(moves))
	for i, np := range moves {
		cur := filepath.FromSlash(current[i])
		if !filepath.IsLocal(cur) {
			return fmt.Errorf("%w: file path %q", ErrInvalid, current[i])
		}
		from := filepath.Join(m.rec.SavePath, cur)
		to := filepath.Join(m.rec.SavePath, filepath.FromSlash(np))
		if toInfo, err := os.Lstat(to); err == nil {
			if fromInfo, err := os.Lstat(from); err != nil || !os.SameFile(fromInfo, toInfo) {
				return fmt.Errorf("%w: %q already exists on disk", ErrExists, np)
			}
		}
		disk[i] = rename{from, to}
	}

	running := m.t != nil
	e.detachLocked(m)
	renames := make(map[string]string, len(m.renames)+len(moves))
	for k, v := range m.renames {
		renames[k] = v
	}
	var errs []error
	for i, np := range moves {
		from, to := disk[i].from, disk[i].to
		if !within(m.rec.SavePath, to) {
			errs = append(errs, fmt.Errorf("%w: %s", ErrPathForbidden, np))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil { //nolint:gosec // download folders are shared with media apps
			errs = append(errs, err)
			continue
		}
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		renames[originalPath(m.info, files[i])] = np
	}
	// Folders the move emptied.
	var oldDirs []string
	for i := range moves {
		oldDirs = append(oldDirs, disk[i].from)
	}
	removeEmptyDirs(m.rec.SavePath, oldDirs)

	m.renames = renames
	m.dirty = true
	e.recordPathsLocked(m)
	if running {
		if err := e.attachLocked(m); err != nil {
			errs = append(errs, err)
		}
	}
	e.scheduleChecksLocked() // a check the rename interrupted starts over
	e.saveLocked(m)
	e.refreshLocked(m)
	return errors.Join(errs...)
}

// removeEmptyDirs removes the directories of paths, all inside root, that
// are empty, deepest first, up to root.
func removeEmptyDirs(root string, paths []string) {
	dirs := map[string]struct{}{}
	for _, p := range paths {
		for d := filepath.Dir(p); d != root && within(root, d); d = filepath.Dir(d) {
			dirs[d] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	slices.SortFunc(sorted, func(a, b string) int { return len(b) - len(a) })
	for _, d := range sorted {
		if empty, _ := isEmptyDir(d); empty {
			_ = os.Remove(d)
		}
	}
}
