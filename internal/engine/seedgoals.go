package engine

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// What happens when a torrent reaches its seeding goals. A torrent or a
// category with ActionDefault follows the next level: the torrent's
// category, then the global seedGoalAction setting.
const (
	ActionDefault        = "default"
	ActionStop           = "stop"
	ActionRemove         = "remove"
	ActionRemoveWithData = "removeWithData"
)

// How a torrent's goals combine: it reaches them when any one is met, or
// only when every goal that is set is. ModeDefault follows the category,
// then the global seedGoalMode setting.
const (
	ModeDefault = "default"
	ModeAny     = "any"
	ModeAll     = "all"
)

// SeedGoals are the seeding goals that apply to a torrent: its own, or
// where it follows the default, its category's, or else the global
// settings. Limits are -1 when unlimited.
type SeedGoals struct {
	RatioLimit               float64 `json:"ratioLimit"`
	SeedingTimeLimit         int64   `json:"seedingTimeLimit" doc:"Minutes."`
	InactiveSeedingTimeLimit int64   `json:"inactiveSeedingTimeLimit" doc:"Minutes."`
	Action                   string  `json:"action" enum:"stop,remove,removeWithData"`
	Mode                     string  `json:"mode" enum:"any,all"`
}

// ShareLimits are per-torrent seed goals. -2 and the default action and
// mode follow the category, then the global settings; -1 is unlimited.
// Times are in minutes.
type ShareLimits struct {
	RatioLimit               float64
	SeedingTimeLimit         int64
	InactiveSeedingTimeLimit int64
	// Action and Mode are left as they are when empty.
	Action string
	Mode   string
}

func validAction(a string, allowDefault bool) bool {
	return slices.Contains([]string{ActionStop, ActionRemove, ActionRemoveWithData}, a) ||
		(allowDefault && a == ActionDefault)
}

func validMode(m string, allowDefault bool) bool {
	return m == ModeAny || m == ModeAll || (allowDefault && m == ModeDefault)
}

// validGoals checks per-torrent or per-category goals: -2 or the default
// follow the next level, -1 is unlimited.
func validGoals(ratio float64, seeding, inactive int64, action, mode string) error {
	if (ratio < 0 && ratio != -1 && ratio != -2) || seeding < -2 || inactive < -2 {
		return fmt.Errorf("%w: share limits must be -2, -1 or at least 0", ErrInvalid)
	}
	if action != "" && !validAction(action, true) {
		return fmt.Errorf("%w: seed goal action %q", ErrInvalid, action)
	}
	if mode != "" && !validMode(mode, true) {
		return fmt.Errorf("%w: seed goal mode %q", ErrInvalid, mode)
	}
	return nil
}

// resolveSeedGoals applies the precedence: the torrent's own value unless
// it follows the default, then the category's unless that does too, then
// the global setting.
func resolveSeedGoals(r store.Torrent, c Category, s settings.Settings) SeedGoals {
	pick := func(own, cat, global float64) float64 {
		switch {
		case own != -2:
			return own
		case cat != -2:
			return cat
		default:
			return global
		}
	}
	str := func(own, cat, global string) string {
		for _, v := range []string{own, cat} {
			if v != "" && v != ActionDefault { // ActionDefault == ModeDefault
				return v
			}
		}
		return global
	}
	return SeedGoals{
		RatioLimit:               pick(r.RatioLimit, c.RatioLimit, s.RatioLimit),
		SeedingTimeLimit:         int64(pick(float64(r.SeedingTimeLimit), float64(c.SeedingTimeLimit), float64(s.SeedingTimeLimit))),
		InactiveSeedingTimeLimit: int64(pick(float64(r.InactiveSeedingTimeLimit), float64(c.InactiveSeedingTimeLimit), float64(s.InactiveSeedingTimeLimit))),
		Action:                   str(r.SeedGoalAction, c.SeedGoalAction, cmp.Or(s.SeedGoalAction, ActionStop)),
		Mode:                     str(r.SeedGoalMode, c.SeedGoalMode, cmp.Or(s.SeedGoalMode, ModeAny)),
	}
}

// seedGoalsLocked resolves a torrent's goals against its category and the
// settings.
func (e *Engine) seedGoalsLocked(m *managed, s settings.Settings) SeedGoals {
	c, ok := e.cats[m.rec.Category]
	if !ok {
		c = NewCategory("")
	}
	return resolveSeedGoals(m.rec, c, s)
}

// reached reports whether a seeding torrent met its goals: any goal that is
// set, or with ModeAll every one of them. A torrent without goals never
// reaches them. Times are in seconds.
func (g SeedGoals) reached(ratio float64, seeding, inactive int64) bool {
	set, met := 0, 0
	check := func(on, hit bool) {
		if on {
			set++
			if hit {
				met++
			}
		}
	}
	check(g.RatioLimit >= 0, ratio >= g.RatioLimit)
	check(g.SeedingTimeLimit >= 0, seeding >= g.SeedingTimeLimit*60)
	check(g.InactiveSeedingTimeLimit >= 0, inactive >= g.InactiveSeedingTimeLimit*60)
	if g.Mode == ModeAll {
		return set > 0 && met == set
	}
	return met > 0
}

// SetShareLimits sets per-torrent seed goals.
func (e *Engine) SetShareLimits(ctx context.Context, l ShareLimits, hashes ...string) error {
	if err := validGoals(l.RatioLimit, l.SeedingTimeLimit, l.InactiveSeedingTimeLimit, l.Action, l.Mode); err != nil {
		return err
	}
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.RatioLimit = l.RatioLimit
		m.rec.SeedingTimeLimit = l.SeedingTimeLimit
		m.rec.InactiveSeedingTimeLimit = l.InactiveSeedingTimeLimit
		if l.Action != "" {
			m.rec.SeedGoalAction = l.Action
		}
		if l.Mode != "" {
			m.rec.SeedGoalMode = l.Mode
		}
		return nil
	})
}

// enforceSeedGoals stops or removes complete torrents that reached their
// seeding goals, each as its own goals say.
func (e *Engine) enforceSeedGoals() {
	s := e.settings.Get()
	now := time.Now().Unix()
	reached := map[string][]string{}
	e.mu.Lock()
	for _, m := range e.torrents {
		// A torrent being checked doesn't know yet what it has.
		if m.t == nil || m.info == nil || m.check != checkNone || !m.complete() {
			continue
		}
		g := e.seedGoalsLocked(m, s)
		_, done := m.wanted()
		r := ratio(m.rec.Uploaded, m.rec.Downloaded, done)
		// A torrent that never transferred anything has been inactive
		// since it completed.
		var inactive int64 = -1
		if lastActive := max(m.rec.LastActivityAt, m.rec.CompletedAt); lastActive > 0 {
			inactive = now - lastActive
		}
		if g.reached(r, m.rec.SeedingSeconds, inactive) {
			reached[g.Action] = append(reached[g.Action], m.rec.InfoHash)
		}
	}
	e.mu.Unlock()
	ctx := e.ctxOrBackground()
	for _, action := range []string{ActionStop, ActionRemove, ActionRemoveWithData} {
		hashes := reached[action]
		if len(hashes) == 0 {
			continue
		}
		slices.Sort(hashes)
		var err error
		switch action {
		case ActionRemove:
			err = e.Remove(ctx, false, hashes...)
		case ActionRemoveWithData:
			err = e.Remove(ctx, true, hashes...)
		default:
			err = e.Stop(ctx, hashes...)
		}
		e.log.Info("seeding goal reached", "torrents", hashes, "action", action, "error", err)
	}
}
