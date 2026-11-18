package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

func TestResolveSeedGoals(t *testing.T) {
	global := settings.Defaults("/downloads")
	global.RatioLimit, global.SeedingTimeLimit, global.InactiveSeedingTimeLimit = 1, 60, 30
	global.SeedGoalAction, global.SeedGoalMode = ActionStop, ModeAny
	follow := store.Torrent{
		RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2,
		SeedGoalAction: ActionDefault, SeedGoalMode: ModeDefault,
	}
	cat := Category{
		RatioLimit: 2, SeedingTimeLimit: -1, InactiveSeedingTimeLimit: -2,
		SeedGoalAction: ActionRemove, SeedGoalMode: ModeDefault,
	}
	own := store.Torrent{
		RatioLimit: 3, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: 5,
		SeedGoalAction: ActionDefault, SeedGoalMode: ModeAll,
	}
	for _, tc := range []struct {
		name string
		rec  store.Torrent
		cat  Category
		want SeedGoals
	}{
		{"everything follows the global settings", follow, NewCategory("c"), SeedGoals{1, 60, 30, ActionStop, ModeAny}},
		{"the category's goals before the global ones", follow, cat, SeedGoals{2, -1, 30, ActionRemove, ModeAny}},
		{"the torrent's own goals first", own, cat, SeedGoals{3, -1, 5, ActionRemove, ModeAll}},
		{"empty action and mode are the default", store.Torrent{RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2}, Category{RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2}, SeedGoals{1, 60, 30, ActionStop, ModeAny}},
	} {
		if got := resolveSeedGoals(tc.rec, tc.cat, global); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestSeedGoalsReached(t *testing.T) {
	const minute = 60
	for _, tc := range []struct {
		name              string
		goals             SeedGoals
		ratio             float64
		seeding, inactive int64
		any, all          bool
	}{
		{"no goals", SeedGoals{-1, -1, -1, "", ""}, 5, 1e6, 1e6, false, false},
		{"one goal met", SeedGoals{1, -1, -1, "", ""}, 1, 0, 0, true, true},
		{"one goal not met", SeedGoals{1, -1, -1, "", ""}, 0.5, 0, 0, false, false},
		{"one of two met", SeedGoals{1, 10, -1, "", ""}, 2, 5 * minute, 0, true, false},
		{"both met", SeedGoals{1, 10, -1, "", ""}, 2, 10 * minute, 0, true, true},
		{"all three met", SeedGoals{0, 0, 0, "", ""}, 0, 0, 0, true, true},
		{"inactive only", SeedGoals{-1, -1, 5, "", ""}, 0, 0, 5 * minute, true, true},
		{"never active", SeedGoals{-1, -1, 0, "", ""}, 0, 0, -1, false, false},
		{"never active, time met", SeedGoals{-1, 0, 0, "", ""}, 0, 0, -1, true, false},
	} {
		tc.goals.Mode = ModeAny
		if got := tc.goals.reached(tc.ratio, tc.seeding, tc.inactive); got != tc.any {
			t.Errorf("%s, any: %v, want %v", tc.name, got, tc.any)
		}
		tc.goals.Mode = ModeAll
		if got := tc.goals.reached(tc.ratio, tc.seeding, tc.inactive); got != tc.all {
			t.Errorf("%s, all: %v, want %v", tc.name, got, tc.all)
		}
	}
}

// TestSeedGoalsInStatus checks a torrent reports the goals in effect as
// its category and the settings change.
func TestSeedGoalsInStatus(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	stopped := true
	cat := NewCategory("tv")
	cat.RatioLimit, cat.SeedGoalAction = 2, ActionRemoveWithData
	if err := h.engine.PutCategory(ctx, cat, true); err != nil {
		t.Fatal(err)
	}
	hash, err := h.engine.Add(ctx, AddParams{
		Magnet: "magnet:?xt=urn:btih:" + "1234567890123456789012345678901234567890", Category: "tv", Stopped: &stopped,
		SeedGoalMode: ModeAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := h.engine.Get(hash)
	if st.SeedGoalAction != ActionDefault || st.SeedGoalMode != ModeAll ||
		st.SeedGoals != (SeedGoals{2, -1, -1, ActionRemoveWithData, ModeAll}) {
		t.Fatalf("status %s %s %+v", st.SeedGoalAction, st.SeedGoalMode, st.SeedGoals)
	}

	cat.RatioLimit, cat.SeedingTimeLimit = -2, 90
	if err := h.engine.PutCategory(ctx, cat, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.settings.Set(ctx, map[string]any{"ratioLimit": 1.5, "seedGoalAction": "remove"}); err != nil {
		t.Fatal(err)
	}
	h.waitFor(hash, "the category's and the global goals", func(st Torrent) bool {
		return st.SeedGoals == SeedGoals{1.5, 90, -1, ActionRemoveWithData, ModeAll}
	})

	if err := h.engine.SetShareLimits(ctx, ShareLimits{RatioLimit: 3, SeedingTimeLimit: -1, InactiveSeedingTimeLimit: -2, Action: ActionStop}, hash); err != nil {
		t.Fatal(err)
	}
	st, _ = h.engine.Get(hash)
	if st.SeedGoalMode != ModeAll || st.SeedGoals != (SeedGoals{3, -1, -1, ActionStop, ModeAll}) {
		t.Fatalf("after SetShareLimits: %s %+v", st.SeedGoalMode, st.SeedGoals)
	}

	for _, l := range []ShareLimits{{RatioLimit: -3}, {Action: "superSeed"}, {Mode: "most"}} {
		if err := h.engine.SetShareLimits(ctx, l, hash); !errors.Is(err, ErrInvalid) {
			t.Errorf("SetShareLimits(%+v): %v", l, err)
		}
	}
	bad := NewCategory("bad")
	bad.SeedGoalAction = "pause"
	if err := h.engine.PutCategory(ctx, bad, true); !errors.Is(err, ErrInvalid) {
		t.Errorf("PutCategory with a bad action: %v", err)
	}
	if _, err := h.engine.Add(ctx, AddParams{Magnet: "magnet:?xt=urn:btih:" + "2234567890123456789012345678901234567890", SeedGoalMode: "most"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Add with a bad mode: %v", err)
	}

	// Kept across a restart.
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	st, _ = h.engine.Get(hash)
	if st.SeedGoalAction != ActionStop || st.SeedGoalMode != ModeAll || st.SeedGoals.SeedingTimeLimit != -1 {
		t.Fatalf("after restart: %s %s %+v", st.SeedGoalAction, st.SeedGoalMode, st.SeedGoals)
	}
	for _, c := range h.engine.Categories() {
		if c.Name == "tv" && c != cat {
			t.Fatalf("category after restart: %+v, want %+v", c, cat)
		}
	}
}

// seedToGoal downloads a torrent from a new seeder with the given add
// parameters and returns the harness, the seeder and the hash.
func seedToGoal(t *testing.T, h *harness, p AddParams) (*seeder, string) {
	t.Helper()
	ctx := context.Background()
	s := newSeeder(t, map[string]int{"x.bin": 64 << 10})
	p.Metainfo = s.Metainfo
	hash, err := h.engine.Add(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.AddPeers(ctx, []string{s.Addr}, hash); err != nil {
		t.Fatal(err)
	}
	return s, hash
}

func (h *harness) waitRemoved(hash string) {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, ok := h.engine.Get(hash); !ok {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s to be removed", hash)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSeedGoalActions(t *testing.T) {
	ctx := context.Background()
	data := func(h *harness) string { return filepath.Join(h.saveDir, "payload", "x.bin") }

	t.Run("the category's action", func(t *testing.T) {
		h := newHarness(t)
		c := NewCategory("movies")
		c.SeedingTimeLimit, c.SeedGoalAction = 0, ActionRemove
		if err := h.engine.PutCategory(ctx, c, true); err != nil {
			t.Fatal(err)
		}
		_, hash := seedToGoal(t, h, AddParams{Category: "movies"})
		h.waitRemoved(hash)
		if _, err := os.Stat(data(h)); err != nil {
			t.Fatalf("remove deleted the data: %v", err)
		}
	})

	t.Run("the torrent's action before the category's", func(t *testing.T) {
		h := newHarness(t)
		c := NewCategory("movies")
		c.SeedingTimeLimit, c.SeedGoalAction = 0, ActionStop
		if err := h.engine.PutCategory(ctx, c, true); err != nil {
			t.Fatal(err)
		}
		_, hash := seedToGoal(t, h, AddParams{Category: "movies", SeedGoalAction: ActionRemoveWithData})
		h.waitRemoved(hash)
		if _, err := os.Stat(data(h)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("removeWithData left the data: %v", err)
		}
	})

	t.Run("all goals must be met", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.settings.Set(ctx, map[string]any{"seedingTimeLimit": 0, "seedGoalMode": "all"}); err != nil {
			t.Fatal(err)
		}
		ratio := 100.0
		_, hash := seedToGoal(t, h, AddParams{RatioLimit: &ratio})
		h.waitFor(hash, "complete", func(st Torrent) bool { return st.Complete })
		// The seeding time goal is met at once; the ratio never is.
		time.Sleep(10 * h.engine.cfg.TickInterval)
		if st, _ := h.engine.Get(hash); st.State == StateStopped {
			t.Fatal("stopped with only one of two goals met")
		}
		if err := h.engine.SetShareLimits(ctx, ShareLimits{RatioLimit: 100, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2, Mode: ModeAny}, hash); err != nil {
			t.Fatal(err)
		}
		h.waitFor(hash, "stop once any goal is enough", func(st Torrent) bool { return st.State == StateStopped })
	})
}
