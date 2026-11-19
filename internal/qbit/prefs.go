package qbit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"reflect"

	"github.com/sebdanielsson/piecemeal/internal/auth"
	"github.com/sebdanielsson/piecemeal/internal/settings"
)

// roundLimit rounds a rate limit as qBittorrent does: down to a multiple
// of 1024 bytes per second, at least 1024, and 0 (unlimited) for 0 or less.
func roundLimit(v int64) int64 {
	if v <= 0 {
		return 0
	}
	return max(v/1024*1024, 1024)
}

// prefInt decodes a whole-number preference value.
func prefInt(v any) (int64, bool) {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

// setPreferences applies the preferences piecemeal maps to its settings.
// Clients send the whole object they read back, so keys piecemeal can't
// apply, keys pinned by environment variables and values that didn't
// change are ignored; mapped keys with bad values fail the whole request.
func (s *Server) setPreferences(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw := r.FormValue("json")
	if raw == "" {
		http.Error(w, "Missing required parameter: json", http.StatusBadRequest)
		return
	}
	var in map[string]any
	if err := json.Unmarshal([]byte(raw), &in); err != nil || in == nil {
		// qBittorrent 5.3.0 ignores a value that isn't a JSON object.
		done(w)
		return
	}
	cur := s.settings.Get()
	values, err := preferencesToSettings(in, cur)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// listen_port reads the port in use, which differs from the setting
	// when that is 0 (random).
	if values["peerPort"] == int64(s.engine.Port()) {
		delete(values, "peerPort")
	}
	locked := s.settings.Locked()
	admin := isAdmin(principal(r))
	for k, v := range values {
		_, pinned := locked[k]
		// Users who aren't admins may change the speed limits only, as in
		// piecemeal's own API.
		if pinned || (!admin && !userSettings[k]) || reflect.DeepEqual(settingValue(cur, k), v) {
			delete(values, k)
		}
	}
	if len(values) > 0 {
		if _, err := s.settings.Set(r.Context(), values); err != nil {
			var ve *settings.ValidationError
			if errors.As(err, &ve) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.log.Error("setPreferences", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
	done(w)
}

// userSettings are the settings any user may change; the rest need an
// admin.
var userSettings = map[string]bool{
	"downloadLimit": true, "uploadLimit": true, "altDownloadLimit": true, "altUploadLimit": true,
	"altSpeedEnabled": true,
}

// isAdmin reports whether the caller may use admin endpoints: an admin
// signed in, with Basic credentials, or with a full-scope key, as in the
// native API. A key for an *arr app (qbittorrent scope) or a dashboard
// (read-only) doesn't count, even an admin's.
func isAdmin(p *auth.Principal) bool {
	return p != nil && p.IsAdmin && (p.Method != "apiKey" || p.Scope == auth.ScopeFull)
}

// requireAdmin answers 403 unless the caller is an admin.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !isAdmin(principal(r)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// settingValue reads a setting by JSON name, in the type Set takes.
func settingValue(st settings.Settings, name string) any {
	b, _ := json.Marshal(st)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	v := m[name]
	if f, ok := v.(float64); ok && f == math.Trunc(f) {
		return int64(f)
	}
	return v
}

// preferencesToSettings maps qBittorrent preference keys to piecemeal
// settings. Values are compared and applied as int64, float64, bool or
// string.
func preferencesToSettings(in map[string]any, cur settings.Settings) (map[string]any, error) {
	out := map[string]any{}
	var errs []error
	bad := func(k string) { errs = append(errs, fmt.Errorf("%s: invalid value %v", k, in[k])) }
	str := func(k, setting string) {
		if v, ok := in[k]; ok {
			if s, ok := v.(string); ok {
				out[setting] = s
			} else {
				bad(k)
			}
		}
	}
	boolean := func(k, setting string) {
		if v, ok := in[k]; ok {
			if b, ok := v.(bool); ok {
				out[setting] = b
			} else {
				bad(k)
			}
		}
	}
	integer := func(k, setting string, conv func(int64) int64) {
		if v, ok := in[k]; ok {
			n, ok := prefInt(v)
			if !ok {
				bad(k)
				return
			}
			if conv != nil {
				n = conv(n)
			}
			out[setting] = n
		}
	}
	kib := func(n int64) int64 { return max(n, 0) * 1024 }

	str("save_path", "defaultSavePath")
	boolean("temp_path_enabled", "incompletePathEnabled")
	str("temp_path", "incompletePath")
	// qBittorrent resolves a relative or empty temp_path against the temp
	// folder in the default save path, the new one if it changes too.
	if v, ok := out["incompletePath"].(string); ok && !filepath.IsAbs(v) {
		base := cur.DefaultSavePath
		if s, ok := out["defaultSavePath"].(string); ok {
			base = s
		}
		out["incompletePath"] = filepath.Join(base, "temp", v)
	}
	boolean("auto_tmm_enabled", "autoManagement")
	boolean("torrent_changed_tmm_enabled", "relocateOnCategoryChange")
	boolean("save_path_changed_tmm_enabled", "relocateOnDefaultPathChange")
	boolean("category_changed_tmm_enabled", "relocateOnCategoryPathChange")
	boolean("start_paused_enabled", "addStopped")
	boolean("add_stopped_enabled", "addStopped")
	integer("dl_limit", "downloadLimit", roundLimit)
	integer("up_limit", "uploadLimit", roundLimit)
	integer("alt_dl_limit", "altDownloadLimit", roundLimit)
	integer("alt_up_limit", "altUploadLimit", roundLimit)
	integer("listen_port", "peerPort", nil)
	if v, ok := in["max_connec_per_torrent"]; ok {
		if n, ok := prefInt(v); ok && n > 0 {
			out["maxConnectionsPerTorrent"] = n
		} else if !ok {
			bad("max_connec_per_torrent")
		}
	}
	boolean("dht", "dht")
	boolean("pex", "pex")
	if v, ok := in["encryption"]; ok && v != nil {
		n, ok := prefInt(v)
		switch {
		case !ok:
			bad("encryption")
		case n == 0:
			out["encryption"] = "prefer"
		case n == 1:
			out["encryption"] = "require"
		case n == 2:
			out["encryption"] = "disable"
		default:
			bad("encryption")
		}
	}
	if v, ok := in["bittorrent_protocol"]; ok {
		n, ok := prefInt(v)
		if !ok || n < 0 || n > 2 {
			bad("bittorrent_protocol")
		} else {
			out["utp"] = n != 1
		}
	}

	proxyPreferences(in, out, bad, &errs)

	// Seeding goals come in flag and value pairs; either may come alone.
	// Off stores -1; a value of 0 or more turns the goal on.
	goal := func(flag, value, setting string, curValue float64, ratio bool) {
		f, hasFlag := in[flag]
		v, hasValue := in[value]
		enabled := curValue >= 0
		if hasFlag {
			b, ok := f.(bool)
			if !ok {
				bad(flag)
				return
			}
			enabled = b
		}
		val := curValue
		if hasValue {
			n, ok := v.(float64)
			if !ok || (!ratio && n != math.Trunc(n)) {
				bad(value)
				return
			}
			val = n
			if !hasFlag {
				enabled = n >= 0
			}
		}
		if !hasFlag && !hasValue {
			return
		}
		switch {
		case !enabled:
			val = -1
		case val < 0:
			// Turned on without a value: qBittorrent's defaults.
			if ratio {
				val = 1
			} else {
				val = 1440
			}
		}
		if ratio {
			out[setting] = val
		} else {
			out[setting] = int64(val)
		}
	}
	goal("max_ratio_enabled", "max_ratio", "ratioLimit", cur.RatioLimit, true)
	goal("max_seeding_time_enabled", "max_seeding_time", "seedingTimeLimit", float64(cur.SeedingTimeLimit), false)
	goal("max_inactive_seeding_time_enabled", "max_inactive_seeding_time", "inactiveSeedingTimeLimit",
		float64(cur.InactiveSeedingTimeLimit), false)
	if v, ok := in["max_ratio_act"]; ok {
		n, ok := prefInt(v)
		switch {
		case !ok:
			bad("max_ratio_act")
		case n == 0:
			out["seedGoalAction"] = "stop"
		case n == 1:
			out["seedGoalAction"] = "remove"
		case n == 3:
			out["seedGoalAction"] = "removeWithData"
		case n == 2:
			// Super seeding: not supported, ignored.
		default:
			bad("max_ratio_act")
		}
	}
	if v, ok := in["share_limits_mode"]; ok {
		// qBittorrent stores MatchAny for anything but MatchAll, Default
		// included.
		out["seedGoalMode"] = "any"
		if v == "MatchAll" {
			out["seedGoalMode"] = "all"
		}
	}

	// qBittorrent takes any number; piecemeal checks at least one torrent
	// at a time.
	integer("max_active_checking_torrents", "maxActiveCheckingTorrents", func(n int64) int64 { return max(n, 1) })
	boolean("queueing_enabled", "queueingEnabled")
	integer("max_active_downloads", "maxActiveDownloads", nil)
	integer("max_active_uploads", "maxActiveUploads", nil)
	integer("max_active_torrents", "maxActiveTorrents", nil)
	boolean("dont_count_slow_torrents", "dontCountSlowTorrents")
	integer("slow_torrent_dl_rate_threshold", "slowDownloadRate", kib)
	integer("slow_torrent_ul_rate_threshold", "slowUploadRate", kib)
	integer("slow_torrent_inactive_timer", "slowTorrentTime", nil)

	boolean("ip_filter_enabled", "ipFilterEnabled")
	str("ip_filter_path", "ipFilterPath")
	str("banned_IPs", "bannedIPs")

	boolean("rss_processing_enabled", "rssEnabled")
	boolean("rss_auto_downloading_enabled", "rssAutoDownload")
	boolean("rss_download_repack_proper_episodes", "rssDownloadRepacks")
	str("rss_smart_episode_filters", "rssSmartEpisodeFilters")
	// qBittorrent stores what it's given; piecemeal keeps the values in
	// range instead of failing a client's whole save.
	integer("rss_refresh_interval", "rssRefreshInterval", func(n int64) int64 { return max(n, 1) })
	integer("rss_fetch_delay", "rssFetchDelay", func(n int64) int64 { return max(n, 0) })
	integer("rss_max_articles_per_feed", "rssMaxArticles", func(n int64) int64 { return max(n, 0) })

	boolean("scheduler_enabled", "altSpeedSchedule")
	clock := func(hourKey, minKey, setting, current string) {
		h, hasH := in[hourKey]
		m, hasM := in[minKey]
		if !hasH && !hasM {
			return
		}
		hour, minute := int64(clockPart(current, 0)), int64(clockPart(current, 1))
		if hasH {
			n, ok := prefInt(h)
			if !ok || n < 0 || n > 23 {
				bad(hourKey)
				return
			}
			hour = n
		}
		if hasM {
			n, ok := prefInt(m)
			if !ok || n < 0 || n > 59 {
				bad(minKey)
				return
			}
			minute = n
		}
		out[setting] = fmt.Sprintf("%02d:%02d", hour, minute)
	}
	clock("schedule_from_hour", "schedule_from_min", "altSpeedScheduleFrom", cur.AltSpeedScheduleFrom)
	clock("schedule_to_hour", "schedule_to_min", "altSpeedScheduleTo", cur.AltSpeedScheduleTo)
	if v, ok := in["scheduler_days"]; ok {
		days := []string{"everyDay", "weekdays", "weekends", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
		n, ok := prefInt(v)
		if !ok || n < 0 || int(n) >= len(days) {
			bad("scheduler_days")
		} else {
			out["altSpeedScheduleDays"] = days[n]
		}
	}
	return out, errors.Join(errs...)
}

// proxyPreferences maps the proxy preferences. proxy_type is a name since
// qBittorrent 4.6 and a number before: -1 none, 1 HTTP, 2 SOCKS5, 3 HTTP
// and 4 SOCKS5 with authentication, 5 SOCKS4. piecemeal has no SOCKS4.
// Authentication is on when there is a username; proxy_password reads
// back empty, so an empty one leaves it as it is.
func proxyPreferences(in, out map[string]any, bad func(string), errs *[]error) {
	if v, ok := in["proxy_type"]; ok {
		types := map[string]string{"None": "none", "HTTP": "http", "SOCKS5": "socks5"}
		legacy := map[int64]string{-1: "none", 0: "none", 1: "http", 2: "socks5", 3: "http", 4: "socks5"}
		var t string
		switch v := v.(type) {
		case string:
			t = types[v]
		case float64:
			if n, ok := prefInt(v); ok {
				t = legacy[n]
			}
		}
		switch {
		case v == "SOCKS4" || v == float64(5):
			*errs = append(*errs, errors.New("proxy_type: SOCKS4 isn't supported; use SOCKS5"))
		case t == "":
			bad("proxy_type")
		default:
			out["proxyType"] = t
		}
	}
	if v, ok := in["proxy_ip"]; ok {
		if s, ok := v.(string); ok {
			out["proxyHost"] = s
		} else {
			bad("proxy_ip")
		}
	}
	if v, ok := in["proxy_port"]; ok {
		if n, ok := prefInt(v); ok {
			out["proxyPort"] = n
		} else {
			bad("proxy_port")
		}
	}
	authOff := in["proxy_auth_enabled"] == false
	switch {
	case authOff:
		out["proxyUsername"] = ""
	case in["proxy_username"] != nil:
		if s, ok := in["proxy_username"].(string); ok {
			out["proxyUsername"] = s
		} else {
			bad("proxy_username")
		}
	}
	if v, ok := in["proxy_password"]; ok && !authOff {
		if s, ok := v.(string); !ok {
			bad("proxy_password")
		} else if s != "" {
			out["proxyPassword"] = s
		}
	}
	for k, setting := range map[string]string{
		"proxy_hostname_lookup": "proxyHostnameLookup", "proxy_bittorrent": "proxyBitTorrent",
		"proxy_peer_connections": "proxyPeerConnections", "proxy_rss": "proxyRSS", "proxy_misc": "proxyTorrentFiles",
	} {
		if v, ok := in[k]; ok {
			if b, ok := v.(bool); ok {
				out[setting] = b
			} else {
				bad(k)
			}
		}
	}
}

// apiKeyName is the name of the key rotateAPIKey manages for a user.
const apiKeyName = "qBittorrent API key" //nolint:gosec // a key's display name, not a credential

// apiKeyMarker is web_ui_api_key: piecemeal stores only key hashes, so it
// reports the caller's qBittorrent key's prefix, or "" without one.
func (s *Server) apiKeyMarker(r *http.Request) string {
	p := principal(r)
	if p == nil {
		return ""
	}
	keys, err := s.auth.ListAPIKeys(r.Context(), p)
	if err != nil {
		return ""
	}
	for _, k := range keys {
		if k.UserID == p.UserID && k.Name == apiKeyName {
			return k.Prefix + "…"
		}
	}
	return ""
}

func (s *Server) deleteOwnKeys(ctx context.Context, p *auth.Principal) error {
	keys, err := s.auth.ListAPIKeys(ctx, p)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k.UserID == p.UserID && k.Name == apiKeyName {
			if err := s.auth.DeleteAPIKey(ctx, p, k.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// rotateAPIKey replaces the caller's qBittorrent key with a new one and
// returns it; it can't be shown again.
func (s *Server) rotateAPIKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Method == "apiKey" && p.Scope != auth.ScopeFull {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := s.deleteOwnKeys(r.Context(), p); err != nil {
		s.engineError(w, err)
		return
	}
	_, secret, err := s.auth.CreateAPIKey(r.Context(), p, apiKeyName, auth.ScopeQBittorrent)
	if err != nil {
		s.engineError(w, err)
		return
	}
	writeJSON(w, map[string]string{"apiKey": secret})
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Method == "apiKey" && p.Scope != auth.ScopeFull {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := s.deleteOwnKeys(r.Context(), p); err != nil {
		s.engineError(w, err)
		return
	}
	noContent(w)
}
