package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// confine resolves path and checks it is inside an allowed root. Symlinks
// in the part of the path that exists are resolved first, so a link can't
// lead out of the roots.
func (e *Engine) confine(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %q is not an absolute path", ErrInvalid, path)
	}
	clean := filepath.Clean(path)
	resolved, err := resolveExisting(clean)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	for _, root := range e.cfg.AllowedRoots {
		r, err := resolveExisting(root)
		if err != nil || !within(r, resolved) {
			continue
		}
		// Rebuild the path from the root and a part checked to stay inside
		// it, so the result is plainly under the root: as given when it is
		// lexically under it, resolved when only a symlink leads there.
		if rel, err := filepath.Rel(root, clean); err == nil && filepath.IsLocal(rel) {
			return filepath.Join(root, rel), nil
		}
		if rel, err := filepath.Rel(r, resolved); err == nil && filepath.IsLocal(rel) {
			return filepath.Join(r, rel), nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrPathForbidden, path)
}

// resolveExisting evaluates symlinks in the longest existing prefix of path
// and appends the rest unchanged.
func resolveExisting(path string) (string, error) {
	rest := []string{}
	p := path
	for {
		r, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(append([]string{r}, rest...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path, nil
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}

// within reports whether path is root or inside it. Both must be clean.
func within(root, path string) bool {
	if root == path {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// PathAllowed reports whether path is inside an allowed root.
func (e *Engine) PathAllowed(path string) bool {
	_, err := e.confine(path)
	return err == nil
}

// Confine returns path, cleaned, if it is inside an allowed root once
// symlinks are resolved, and ErrPathForbidden otherwise.
func (e *Engine) Confine(path string) (string, error) {
	return e.confine(path)
}

// FreeSpace returns the free bytes on the filesystem holding path, which
// must be inside an allowed root, or of its closest existing parent: -1
// when unknown.
func (e *Engine) FreeSpace(path string) (int64, error) {
	p, err := e.confine(path)
	if err != nil {
		return 0, err
	}
	for {
		if _, err := os.Stat(p); err == nil {
			return freeSpace(p), nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return -1, nil
		}
		p = parent
	}
}
