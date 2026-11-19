package engine

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Category groups torrents and can give them a save path, an incomplete
// folder and seeding goals.
type Category struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath" doc:"Where torrents in this category are saved. Empty uses the default save path, or for automatically managed torrents a folder named after the category in it."`
	// IncompletePathMode and IncompletePath are the category's incomplete
	// folder.
	IncompletePathMode string `json:"incompletePathMode" enum:"default,off,on" doc:"The category's incomplete folder: default follows the global setting, off keeps its torrents in their save path, on uses incompletePath."`
	IncompletePath     string `json:"incompletePath" doc:"With incompletePathMode on: where the category's incomplete torrents are kept."`
	// Seeding goals for the category's torrents that follow the default.
	RatioLimit               float64 `json:"ratioLimit" doc:"-2 follows the global setting, -1 is unlimited."`
	SeedingTimeLimit         int64   `json:"seedingTimeLimit" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
	InactiveSeedingTimeLimit int64   `json:"inactiveSeedingTimeLimit" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
	SeedGoalAction           string  `json:"seedGoalAction" enum:"default,stop,remove,removeWithData" doc:"What happens when a torrent reaches its seeding goals; default follows the global setting."`
	SeedGoalMode             string  `json:"seedGoalMode" enum:"default,any,all" doc:"Whether any goal or all of them must be met; default follows the global setting."`
}

// NewCategory returns a category with no save path whose seeding goals
// follow the global settings.
func NewCategory(name string) Category {
	return Category{
		Name: name, RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2,
		SeedGoalAction: ActionDefault, SeedGoalMode: ModeDefault, IncompletePathMode: IncompleteDefault,
	}
}

func (e *Engine) loadCategoriesAndTags(ctx context.Context) error {
	cats, err := e.store.ListCategories(ctx)
	if err != nil {
		return fmt.Errorf("load categories: %w", err)
	}
	for _, c := range cats {
		e.cats[c.Name] = Category{
			Name: c.Name, SavePath: c.SavePath, RatioLimit: c.RatioLimit, SeedingTimeLimit: c.SeedingTimeLimit,
			InactiveSeedingTimeLimit: c.InactiveSeedingTimeLimit,
			SeedGoalAction:           cmp.Or(c.SeedGoalAction, ActionDefault), SeedGoalMode: cmp.Or(c.SeedGoalMode, ModeDefault),
			IncompletePathMode: cmp.Or(c.IncompletePathMode, IncompleteDefault), IncompletePath: c.IncompletePath,
		}
	}
	tags, err := e.store.ListTags(ctx)
	if err != nil {
		return fmt.Errorf("load tags: %w", err)
	}
	for _, t := range tags {
		e.tags[t] = struct{}{}
	}
	return nil
}

// Categories returns every category, by name.
func (e *Engine) Categories() []Category {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := append([]Category{}, slices.Collect(maps.Values(e.cats))...)
	slices.SortFunc(out, func(a, b Category) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Tags returns every tag, sorted.
func (e *Engine) Tags() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, slices.Sorted(maps.Keys(e.tags))...)
}

func validName(kind, name string) error {
	if strings.TrimSpace(name) != name || name == "" {
		return fmt.Errorf("%w: %s name must be non-empty without surrounding spaces", ErrInvalid, kind)
	}
	if strings.ContainsAny(name, "\x00\n\r") {
		return fmt.Errorf("%w: %s name contains control characters", ErrInvalid, kind)
	}
	if kind == "tag" && strings.Contains(name, ",") {
		return fmt.Errorf("%w: tag names cannot contain commas", ErrInvalid)
	}
	return nil
}

// PutCategory creates a category or changes its paths and seeding goals.
// Manually managed torrents in it stay where they are; automatically
// managed ones follow a change of its paths, or switch to manual
// management, as relocateOnCategoryPathChange says. Start from
// NewCategory: zero limits are goals of 0. An empty action, mode or
// incomplete folder mode is the default.
func (e *Engine) PutCategory(ctx context.Context, c Category, create bool) error {
	if err := validName("category", c.Name); err != nil {
		return err
	}
	c.SeedGoalAction, c.SeedGoalMode = cmp.Or(c.SeedGoalAction, ActionDefault), cmp.Or(c.SeedGoalMode, ModeDefault)
	if err := validGoals(c.RatioLimit, c.SeedingTimeLimit, c.InactiveSeedingTimeLimit, c.SeedGoalAction, c.SeedGoalMode); err != nil {
		return err
	}
	c.IncompletePathMode = cmp.Or(c.IncompletePathMode, IncompleteDefault)
	if !validIncompleteMode(c.IncompletePathMode) {
		return fmt.Errorf("%w: incomplete folder mode %q", ErrInvalid, c.IncompletePathMode)
	}
	if c.IncompletePathMode != IncompleteOn {
		c.IncompletePath = ""
	}
	var err error
	if c.SavePath, err = e.categoryPath(c.SavePath); err != nil {
		return fmt.Errorf("save path: %w", err)
	}
	if c.IncompletePath, err = e.categoryPath(c.IncompletePath); err != nil {
		return fmt.Errorf("incomplete folder: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	old, exists := e.cats[c.Name]
	if create && exists {
		return fmt.Errorf("%w: category %q", ErrExists, c.Name)
	}
	if !create && !exists {
		return fmt.Errorf("%w: category %q", ErrNotFound, c.Name)
	}
	err = e.store.UpsertCategory(ctx, store.UpsertCategoryParams{
		Name: c.Name, SavePath: c.SavePath, RatioLimit: c.RatioLimit, SeedingTimeLimit: c.SeedingTimeLimit,
		InactiveSeedingTimeLimit: c.InactiveSeedingTimeLimit, SeedGoalAction: c.SeedGoalAction, SeedGoalMode: c.SeedGoalMode,
		IncompletePathMode: c.IncompletePathMode, IncompletePath: c.IncompletePath,
	})
	if err != nil {
		return fmt.Errorf("save category: %w", err)
	}
	e.cats[c.Name] = c
	if exists && (old.SavePath != c.SavePath || old.IncompletePathMode != c.IncompletePathMode ||
		old.IncompletePath != c.IncompletePath) {
		s := e.settings.Get()
		changed := e.pathsChangedLocked(s, s.RelocateOnCategoryPathChange, func(m *managed) bool { return m.rec.Category == c.Name })
		for _, m := range changed {
			e.saveLocked(m)
		}
		e.refreshLocked(changed...)
	}
	e.bus.publish(Event{Type: EventCategories})
	return nil
}

// categoryPath checks a category's save path or incomplete folder: an
// absolute path inside the allowed roots, or a relative one, which is
// resolved against the default save path or the global incomplete folder
// when used.
func (e *Engine) categoryPath(p string) (string, error) {
	switch {
	case p == "":
		return "", nil
	case filepath.IsAbs(p):
		return e.confine(p)
	default:
		c := filepath.Clean(filepath.FromSlash(p))
		if !filepath.IsLocal(c) {
			return "", fmt.Errorf("%w: %q leads outside the folder it's relative to", ErrInvalid, p)
		}
		return c, nil
	}
}

// DeleteCategories deletes categories; their torrents are left without one.
func (e *Engine) DeleteCategories(ctx context.Context, names ...string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.store.Tx(ctx, func(q *store.Queries) error {
		for _, n := range names {
			if _, err := q.DeleteCategory(ctx, n); err != nil {
				return err
			}
			if err := q.ClearCategory(ctx, n); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete categories: %w", err)
	}
	s := e.settings.Get()
	for _, n := range names {
		delete(e.cats, n)
		for _, m := range e.torrents {
			if m.rec.Category == n {
				m.rec.Category = ""
				e.categoryChangedLocked(m, s)
				e.saveLocked(m)
				e.refreshLocked(m)
			}
		}
	}
	e.bus.publish(Event{Type: EventCategories})
	return nil
}

// SetCategory moves torrents into a category, or out of any with "".
// Automatically managed torrents follow the category's paths, or switch to
// manual management, as relocateOnCategoryChange says.
func (e *Engine) SetCategory(ctx context.Context, category string, hashes ...string) error {
	e.mu.Lock()
	_, ok := e.cats[category]
	e.mu.Unlock()
	if category != "" && !ok {
		return fmt.Errorf("%w: category %q", ErrNotFound, category)
	}
	s := e.settings.Get()
	return e.each(ctx, hashes, func(m *managed) error {
		if m.rec.Category == category {
			return nil
		}
		m.rec.Category = category
		e.categoryChangedLocked(m, s)
		return nil
	})
}

// categoryChangedLocked applies a torrent's new category to its paths if
// it's automatically managed.
func (e *Engine) categoryChangedLocked(m *managed, s settings.Settings) {
	if !m.rec.AutoManaged {
		return
	}
	if s.RelocateOnCategoryChange {
		e.followCategoryLocked(m, s)
		return
	}
	m.rec.AutoManaged = false
	m.dirty = true
}

// CreateTags creates tags.
func (e *Engine) CreateTags(ctx context.Context, tags ...string) error {
	for _, t := range tags {
		if err := validName("tag", t); err != nil {
			return err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.store.Tx(ctx, func(q *store.Queries) error {
		for _, t := range tags {
			if err := q.CreateTag(ctx, t); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create tags: %w", err)
	}
	for _, t := range tags {
		e.tags[t] = struct{}{}
	}
	e.bus.publish(Event{Type: EventTags})
	return nil
}

// DeleteTags deletes tags and removes them from every torrent.
func (e *Engine) DeleteTags(ctx context.Context, tags ...string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.store.Tx(ctx, func(q *store.Queries) error {
		for _, t := range tags {
			if _, err := q.DeleteTag(ctx, t); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete tags: %w", err)
	}
	for _, t := range tags {
		delete(e.tags, t)
	}
	for _, m := range e.torrents {
		n := len(m.tags)
		m.tags = slices.DeleteFunc(slices.Clone(m.tags), func(t string) bool { return slices.Contains(tags, t) })
		if len(m.tags) != n {
			e.refreshLocked(m)
		}
	}
	e.bus.publish(Event{Type: EventTags})
	return nil
}

// AddTags tags torrents, creating tags that don't exist.
func (e *Engine) AddTags(ctx context.Context, tags []string, hashes ...string) error {
	for _, t := range tags {
		if err := validName("tag", t); err != nil {
			return err
		}
	}
	created := false
	err := e.each(ctx, hashes, func(m *managed) error {
		for _, t := range tags {
			if _, ok := e.tags[t]; !ok {
				if err := e.store.CreateTag(ctx, t); err != nil {
					return err
				}
				e.tags[t] = struct{}{}
				created = true
			}
			if slices.Contains(m.tags, t) {
				continue
			}
			if err := e.store.AddTorrentTag(ctx, store.AddTorrentTagParams{InfoHash: m.rec.InfoHash, Tag: t}); err != nil {
				return err
			}
			m.tags = append(slices.Clone(m.tags), t)
			slices.Sort(m.tags)
		}
		return nil
	})
	if created {
		e.bus.publish(Event{Type: EventTags})
	}
	return err
}

// RemoveTags removes tags from torrents. No tags means all of them.
func (e *Engine) RemoveTags(ctx context.Context, tags []string, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		remove := tags
		if len(remove) == 0 {
			remove = m.tags
		}
		for _, t := range remove {
			if err := e.store.RemoveTorrentTag(ctx, store.RemoveTorrentTagParams{InfoHash: m.rec.InfoHash, Tag: t}); err != nil {
				return err
			}
		}
		m.tags = slices.DeleteFunc(slices.Clone(m.tags), func(t string) bool { return slices.Contains(remove, t) })
		return nil
	})
}
