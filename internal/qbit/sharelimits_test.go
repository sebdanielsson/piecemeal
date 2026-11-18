package qbit

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (e *env) info(hash string) Torrent {
	e.t.Helper()
	var list []Torrent
	e.get("torrents/info?hashes="+hash).json(e.t, &list)
	if len(list) != 1 {
		e.t.Fatalf("info %s: %+v", hash, list)
	}
	return list[0]
}

func TestShareLimits(t *testing.T) {
	e := newEnv(t)
	e.login()
	const h1 = "1111111111111111111111111111111111111111"
	const h2 = "2222222222222222222222222222222222222222"

	// Categories take share limits since WebAPI 2.16.2, each optional.
	expect(t, "createCategory", e.post("torrents/createCategory", url.Values{
		"category": {"tv"}, "savePath": {"tv"}, "ratioLimit": {"1.5"}, "seedingTimeLimit": {"60"},
		"shareLimitsMode": {"MatchAll"}, "shareLimitAction": {"Remove"},
	}), http.StatusOK)
	var cats map[string]map[string]any
	e.get("torrents/categories").json(t, &cats)
	want := map[string]any{
		"name": "tv", "savePath": "tv", "download_path": nil, "ratio_limit": 1.5, "seeding_time_limit": 60.0,
		"inactive_seeding_time_limit": -2.0, "share_limit_action": "Remove", "share_limits_mode": "MatchAll",
	}
	if !jsonEqual(t, cats["tv"], want) {
		t.Fatalf("categories: %v", cats)
	}

	// editCategory changes only what it's given, savePath included.
	expect(t, "editCategory", e.post("torrents/editCategory", url.Values{
		"category": {"tv"}, "inactiveSeedingTimeLimit": {"30"}, "shareLimitAction": {"Default"},
	}), http.StatusOK)
	e.get("torrents/categories").json(t, &cats)
	want["inactive_seeding_time_limit"], want["share_limit_action"] = 30.0, "Default"
	if !jsonEqual(t, cats["tv"], want) {
		t.Fatalf("after editCategory: %v", cats)
	}
	expect(t, "editCategory savePath", e.post("torrents/editCategory", url.Values{"category": {"tv"}, "savePath": {""}}), http.StatusOK)
	e.get("torrents/categories").json(t, &cats)
	if cats["tv"]["savePath"] != "" || cats["tv"]["ratio_limit"] != 1.5 {
		t.Fatalf("after clearing savePath: %v", cats)
	}

	// qBittorrent 5.3.0 checks the parameters in this order and names the
	// first bad one.
	for _, tc := range []struct{ form, msg string }{
		{"ratioLimit=-3&shareLimitsMode=x", "'ratioLimit' parameter has invalid value"},
		{"ratioLimit=-0.5", "'ratioLimit' parameter has invalid value"},
		{"ratioLimit=abc", "'ratioLimit' parameter has invalid value"},
		{"seedingTimeLimit=1.5&inactiveSeedingTimeLimit=-3", "'seedingTimeLimit' parameter has invalid value"},
		{"inactiveSeedingTimeLimit=-3&shareLimitsMode=x", "'inactiveSeedingTimeLimit' parameter has invalid value"},
		{"shareLimitsMode=matchall&shareLimitAction=x", "'shareLimitsMode' parameter has invalid value"},
		{"shareLimitsMode=&shareLimitAction=", "'shareLimitsMode' parameter has invalid value"},
		{"shareLimitAction=remove", "'shareLimitAction' parameter has invalid value"},
	} {
		r := e.post("torrents/createCategory", mustQuery(t, "category=bad&"+tc.form))
		if r.status != http.StatusBadRequest || strings.TrimSpace(string(r.body)) != tc.msg {
			t.Errorf("createCategory %s: %d %s, want 400 %s", tc.form, r.status, r.body, tc.msg)
		}
	}
	r := e.post("torrents/createCategory", url.Values{"category": {"seed"}, "shareLimitAction": {"EnableSuperSeeding"}})
	if r.status != http.StatusConflict || !strings.Contains(string(r.body), "Super seeding") {
		t.Fatalf("createCategory with super seeding: %d %s", r.status, r.body)
	}
	expect(t, "createCategory unlimited", e.post("torrents/createCategory", url.Values{
		"category": {"free"}, "ratioLimit": {"-1"}, "seedingTimeLimit": {"-1"}, "inactiveSeedingTimeLimit": {"0"},
	}), http.StatusOK)
	expect(t, "editCategory missing", e.post("torrents/editCategory", url.Values{"category": {"nope"}, "ratioLimit": {"1"}}), http.StatusNotFound)

	// torrents/add takes an action and a mode; a name qBittorrent doesn't
	// know is Default.
	expect(t, "add", e.post("torrents/add", url.Values{
		"urls": {"magnet:?xt=urn:btih:" + h1}, "stopped": {"true"}, "category": {"tv"},
		"shareLimitAction": {"RemoveWithContent"}, "shareLimitsMode": {"Bogus"},
	}), http.StatusOK)
	tor := e.info(h1)
	if tor.ShareLimitAction != "RemoveWithContent" || tor.ShareLimitsMode != "Default" || tor.RatioLimit != -2 ||
		tor.MaxRatio != 1.5 || tor.MaxSeedingTime != 60 || tor.MaxInactiveSeedingTime != 30 {
		t.Fatalf("added torrent: %+v", tor)
	}
	r = e.post("torrents/add", url.Values{"urls": {"magnet:?xt=urn:btih:" + h2}, "shareLimitAction": {"EnableSuperSeeding"}})
	if r.status != http.StatusConflict {
		t.Fatalf("add with super seeding: %d %s", r.status, r.body)
	}
	expect(t, "add plain", e.post("torrents/add", url.Values{"urls": {"magnet:?xt=urn:btih:" + h2}, "stopped": {"true"}}), http.StatusOK)

	// setShareLimits stores the action and mode, as VueTorrent sends them.
	expect(t, "setShareLimits", e.post("torrents/setShareLimits", url.Values{
		"hashes": {h1 + "|" + h2}, "ratioLimit": {"2"}, "seedingTimeLimit": {"-2"}, "inactiveSeedingTimeLimit": {"-1"},
		"shareLimitAction": {"Stop"}, "shareLimitsMode": {"MatchAny"},
	}), http.StatusOK)
	tor = e.info(h1)
	if tor.ShareLimitAction != "Stop" || tor.ShareLimitsMode != "MatchAny" || tor.RatioLimit != 2 || tor.MaxRatio != 2 ||
		tor.MaxSeedingTime != 60 || tor.InactiveSeedingTimeLimit != -1 || tor.MaxInactiveSeedingTime != -1 {
		t.Fatalf("after setShareLimits: %+v", tor)
	}
	if tor := e.info(h2); tor.ShareLimitAction != "Stop" || tor.MaxSeedingTime != -1 {
		t.Fatalf("second torrent: %+v", tor)
	}
	full := e.maindata(0)
	if f := full.Torrents[h1]; f["share_limit_action"] != "Stop" || f["share_limits_mode"] != "MatchAny" {
		t.Fatalf("maindata torrent: %v", f)
	}
	if c := full.Categories["tv"]; c["share_limits_mode"] != "MatchAll" || c["ratio_limit"] != 1.5 {
		t.Fatalf("maindata category: %v", c)
	}

	// Clients older than qBittorrent 5 leave the action and mode as they
	// are; unknown names are Default; super seeding is refused.
	expect(t, "setShareLimits without 5.x fields", e.post("torrents/setShareLimits", url.Values{
		"hashes": {h1}, "ratioLimit": {"3"}, "seedingTimeLimit": {"-2"}, "inactiveSeedingTimeLimit": {"-2"},
	}), http.StatusOK)
	if tor := e.info(h1); tor.ShareLimitAction != "Stop" || tor.ShareLimitsMode != "MatchAny" || tor.RatioLimit != 3 {
		t.Fatalf("after an old client's setShareLimits: %+v", tor)
	}
	expect(t, "setShareLimits unknown names", e.post("torrents/setShareLimits", url.Values{
		"hashes": {h1}, "ratioLimit": {"3"}, "seedingTimeLimit": {"-2"}, "inactiveSeedingTimeLimit": {"-2"},
		"shareLimitAction": {"Pause"}, "shareLimitsMode": {"matchall"},
	}), http.StatusOK)
	if tor := e.info(h1); tor.ShareLimitAction != "Default" || tor.ShareLimitsMode != "Default" {
		t.Fatalf("after unknown names: %+v", tor)
	}
	r = e.post("torrents/setShareLimits", url.Values{
		"hashes": {h1}, "ratioLimit": {"3"}, "seedingTimeLimit": {"-2"}, "inactiveSeedingTimeLimit": {"-2"},
		"shareLimitAction": {"EnableSuperSeeding"}, "shareLimitsMode": {"Default"},
	})
	if r.status != http.StatusConflict || !strings.Contains(string(r.body), "Super seeding") {
		t.Fatalf("setShareLimits with super seeding: %d %s", r.status, r.body)
	}

	// The global mode is the share_limits_mode preference: MatchAll, or
	// MatchAny for anything else, as qBittorrent stores it.
	if p := e.preferences(); p["share_limits_mode"] != "MatchAny" {
		t.Fatalf("share_limits_mode: %v", p["share_limits_mode"])
	}
	for _, tc := range []struct {
		value any
		want  string
	}{{"MatchAll", "MatchAll"}, {"Default", "MatchAny"}, {"MatchAll", "MatchAll"}, {"matchall", "MatchAny"}, {"MatchAll", "MatchAll"}, {2, "MatchAny"}} {
		expect(t, "setPreferences", e.setPreferences(map[string]any{"share_limits_mode": tc.value}), http.StatusOK)
		if got := e.preferences()["share_limits_mode"]; got != tc.want {
			t.Errorf("share_limits_mode %v: %v, want %s", tc.value, got, tc.want)
		}
	}
}

func mustQuery(t *testing.T, q string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
