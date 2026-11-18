package qbit

import (
	"math"
	"net/http"
	"strconv"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// qBittorrent's names for the seed goal actions and modes.
var (
	actionNames = map[string]string{
		engine.ActionDefault: "Default", engine.ActionStop: "Stop", engine.ActionRemove: "Remove",
		engine.ActionRemoveWithData: "RemoveWithContent",
	}
	modeNames = map[string]string{
		engine.ModeDefault: "Default", engine.ModeAny: "MatchAny", engine.ModeAll: "MatchAll",
	}
)

// superSeeding is the share limit action piecemeal can't carry out:
// anacrolix has no super seeding.
const superSeeding = "EnableSuperSeeding"

func refuseSuperSeeding(w http.ResponseWriter) {
	http.Error(w, "Super seeding is not supported by piecemeal", http.StatusConflict)
}

// shareLimitAction is qBittorrent's share_limit_action for an action.
func shareLimitAction(a string) string {
	if n, ok := actionNames[a]; ok {
		return n
	}
	return "Default"
}

// shareLimitsMode is qBittorrent's share_limits_mode for a mode.
func shareLimitsMode(m string) string {
	if n, ok := modeNames[m]; ok {
		return n
	}
	return "Default"
}

// parseShareLimitAction reads qBittorrent's name for an action; ok is
// false for a name it doesn't know, including EnableSuperSeeding.
func parseShareLimitAction(name string) (action string, ok bool) {
	return parseName(actionNames, name)
}

// parseShareLimitsMode reads qBittorrent's name for a mode.
func parseShareLimitsMode(name string) (mode string, ok bool) {
	return parseName(modeNames, name)
}

func parseName(names map[string]string, name string) (string, bool) {
	for k, n := range names {
		if n == name {
			return k, true
		}
	}
	return "", false
}

// lenientShareLimits reads shareLimitAction and shareLimitsMode the way
// torrents/setShareLimits and torrents/add do in qBittorrent 5.3.0: a name
// it doesn't know is Default. An absent one stays empty, which leaves the
// torrent's as it is.
func lenientShareLimits(r *http.Request) (action, mode string) {
	if r.Form.Has("shareLimitAction") {
		var ok bool
		if action, ok = parseShareLimitAction(r.FormValue("shareLimitAction")); !ok {
			action = engine.ActionDefault
		}
	}
	if r.Form.Has("shareLimitsMode") {
		var ok bool
		if mode, ok = parseShareLimitsMode(r.FormValue("shareLimitsMode")); !ok {
			mode = engine.ModeDefault
		}
	}
	return action, mode
}

// categoryShareLimits reads the share limits of torrents/createCategory and
// torrents/editCategory (WebAPI 2.16.2) into c. Each is optional; a value
// qBittorrent 5.3.0 rejects answers 400 naming the first bad parameter, in
// its order. It reports false once it has answered.
func categoryShareLimits(w http.ResponseWriter, r *http.Request, c *engine.Category) bool {
	invalid := func(name string) bool {
		http.Error(w, "'"+name+"' parameter has invalid value", http.StatusBadRequest)
		return false
	}
	if r.Form.Has("ratioLimit") {
		v, err := strconv.ParseFloat(r.FormValue("ratioLimit"), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || (v < 0 && v != -1 && v != -2) {
			return invalid("ratioLimit")
		}
		c.RatioLimit = v
	}
	for _, p := range []struct {
		name string
		dst  *int64
	}{{"seedingTimeLimit", &c.SeedingTimeLimit}, {"inactiveSeedingTimeLimit", &c.InactiveSeedingTimeLimit}} {
		if !r.Form.Has(p.name) {
			continue
		}
		v, err := strconv.ParseInt(r.FormValue(p.name), 10, 64)
		if err != nil || v < -2 {
			return invalid(p.name)
		}
		*p.dst = v
	}
	if r.Form.Has("shareLimitsMode") {
		m, ok := parseShareLimitsMode(r.FormValue("shareLimitsMode"))
		if !ok {
			return invalid("shareLimitsMode")
		}
		c.SeedGoalMode = m
	}
	if r.Form.Has("shareLimitAction") {
		name := r.FormValue("shareLimitAction")
		if name == superSeeding {
			refuseSuperSeeding(w)
			return false
		}
		a, ok := parseShareLimitAction(name)
		if !ok {
			return invalid("shareLimitAction")
		}
		c.SeedGoalAction = a
	}
	return true
}
