package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Category groups torrents and can give them a save path.
type Category struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath" doc:"Where torrents in this category are saved; empty uses the default save path."`
}

func (e *Engine) loadCategoriesAndTags(ctx context.Context) error {
	cats, err := e.store.ListCategories(ctx)
	if err != nil {
		return fmt.Errorf("load categories: %w", err)
	}
	for _, c := range cats {
		e.cats[c.Name] = Category{Name: c.Name, SavePath: c.SavePath}
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

// PutCategory creates a category or changes its save path. Torrents already
// in it stay where they are.
func (e *Engine) PutCategory(ctx context.Context, c Category, create bool) error {
	if err := validName("category", c.Name); err != nil {
		return err
	}
	if c.SavePath != "" {
		p, err := e.confine(c.SavePath)
		if err != nil {
			return err
		}
		c.SavePath = p
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, exists := e.cats[c.Name]
	if create && exists {
		return fmt.Errorf("%w: category %q", ErrExists, c.Name)
	}
	if !create && !exists {
		return fmt.Errorf("%w: category %q", ErrNotFound, c.Name)
	}
	if err := e.store.UpsertCategory(ctx, store.UpsertCategoryParams{Name: c.Name, SavePath: c.SavePath}); err != nil {
		return fmt.Errorf("save category: %w", err)
	}
	e.cats[c.Name] = c
	e.bus.publish(Event{Type: EventCategories})
	return nil
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
	for _, n := range names {
		delete(e.cats, n)
		for _, m := range e.torrents {
			if m.rec.Category == n {
				m.rec.Category = ""
				e.refreshLocked(m)
			}
		}
	}
	e.bus.publish(Event{Type: EventCategories})
	return nil
}

// SetCategory moves torrents into a category, or out of any with "".
func (e *Engine) SetCategory(ctx context.Context, category string, hashes ...string) error {
	e.mu.Lock()
	_, ok := e.cats[category]
	e.mu.Unlock()
	if category != "" && !ok {
		return fmt.Errorf("%w: category %q", ErrNotFound, category)
	}
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.Category = category
		return nil
	})
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
