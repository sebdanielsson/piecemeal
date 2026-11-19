package qbit

import (
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/sebdanielsson/piecemeal/internal/settings"
)

func (s *Server) buildInfo(w http.ResponseWriter, _ *http.Request) {
	platform := "unknown"
	switch runtime.GOOS {
	case "linux", "windows":
		platform = runtime.GOOS
	case "darwin":
		platform = "macos"
	}
	// libtorrent's place is anacrolix/torrent's; piecemeal uses none of
	// the other libraries.
	writeJSON(w, map[string]any{
		"qt": "", "libtorrent": torrentLibrary, "boost": "", "openssl": "", "zlib": "",
		"bitness":  strconv.IntSize,
		"platform": platform,
	})
}

// torrentLibrary names the anacrolix/torrent version piecemeal was built
// with.
var torrentLibrary = func() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/anacrolix/torrent" {
				if d.Replace != nil {
					d = d.Replace
				}
				return "anacrolix/torrent " + d.Version
			}
		}
	}
	return ""
}()

func (s *Server) processInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]int64{"launch_time": s.started.Unix()})
}

// getFreeSpaceAtPath reports the free space for a path inside the allowed
// roots.
func (s *Server) getFreeSpaceAtPath(w http.ResponseWriter, r *http.Request) {
	path := r.FormValue("path")
	if path == "" {
		http.Error(w, "Missing required parameter: path", http.StatusBadRequest)
		return
	}
	free, err := s.engine.FreeSpace(path)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	s.text(strconv.FormatInt(free, 10))(w, r)
}

func (s *Server) defaultSavePath(w http.ResponseWriter, r *http.Request) {
	s.text(s.settings.Get().DefaultSavePath)(w, r)
}

// seedGoalAction maps piecemeal's seed goal action to qBittorrent's
// max_ratio_act: 0 stop, 1 remove, 3 remove with content.
func seedGoalAction(a string) int {
	switch a {
	case "remove":
		return 1
	case "removeWithData":
		return 3
	default:
		return 0
	}
}

func encryptionMode(e string) int {
	switch e {
	case "require":
		return 1
	case "disable":
		return 2
	default:
		return 0
	}
}

// preferences reports piecemeal's settings under qBittorrent's preference
// names. The *arrs read the save path, the seeding goals, the action at the
// goal, queueing and DHT.
func (s *Server) preferences(w http.ResponseWriter, r *http.Request) {
	prefs := defaultPreferences()
	for k, v := range s.mappedPreferences() {
		prefs[k] = v
	}
	prefs["web_ui_api_key"] = s.apiKeyMarker(r)
	writeJSON(w, prefs)
}

// mappedPreferences are the preferences that come from piecemeal's own
// settings and state.
func (s *Server) mappedPreferences() map[string]any {
	st := s.settings.Get()
	boundInterface, _ := s.engine.BoundInterface()
	limit := func(v int64) int64 {
		if v <= 0 {
			return 0
		}
		return v
	}
	orDefault := func(v, def float64) float64 {
		if v < 0 {
			return def
		}
		return v
	}
	p := map[string]any{
		// Downloads
		"save_path":                     st.DefaultSavePath,
		"temp_path_enabled":             st.IncompletePathEnabled,
		"temp_path":                     st.IncompletePath,
		"auto_tmm_enabled":              st.AutoManagement,
		"torrent_content_layout":        "Original",
		"add_stopped_enabled":           st.AddStopped,
		"add_to_top_of_queue":           false,
		"preallocate_all":               false,
		"incomplete_files_ext":          false,
		"use_unwanted_folder":           false,
		"excluded_file_names_enabled":   false,
		"excluded_file_names":           "",
		"torrent_changed_tmm_enabled":   st.RelocateOnCategoryChange,
		"save_path_changed_tmm_enabled": st.RelocateOnDefaultPathChange,
		"category_changed_tmm_enabled":  st.RelocateOnCategoryPathChange,

		// Speed
		"dl_limit":                limit(st.DownloadLimit),
		"up_limit":                limit(st.UploadLimit),
		"alt_dl_limit":            limit(st.AltDownloadLimit),
		"alt_up_limit":            limit(st.AltUploadLimit),
		"limit_utp_rate":          true,
		"limit_tcp_overhead":      false,
		"limit_lan_peers":         true,
		"bittorrent_protocol":     btProtocol(st),
		"listen_port":             s.engine.Port(),
		"upnp":                    false,
		"random_port":             false,
		"max_connec":              -1,
		"max_connec_per_torrent":  st.MaxConnectionsPerTorrent,
		"max_uploads":             -1,
		"max_uploads_per_torrent": -1,

		// Proxy. The password is write-only, as in piecemeal's own API.
		"proxy_type":             proxyType(st.ProxyType),
		"proxy_ip":               st.ProxyHost,
		"proxy_port":             st.ProxyPort,
		"proxy_auth_enabled":     st.ProxyUsername != "",
		"proxy_username":         st.ProxyUsername,
		"proxy_password":         "",
		"proxy_hostname_lookup":  st.ProxyHostnameLookup,
		"proxy_bittorrent":       st.ProxyBitTorrent,
		"proxy_peer_connections": st.ProxyPeerConnections,
		"proxy_rss":              st.ProxyRSS,
		"proxy_misc":             st.ProxyTorrentFiles,

		// The interface PIECEMEAL_BIND_INTERFACE binds to; setting it
		// here has no effect.
		"current_network_interface": boundInterface,
		"current_interface_name":    boundInterface,

		// BitTorrent
		"dht":                            st.DHT,
		"pex":                            st.PEX,
		"lsd":                            false,
		"encryption":                     encryptionMode(st.Encryption),
		"anonymous_mode":                 false,
		"max_active_checking_torrents":   st.MaxActiveCheckingTorrents,
		"queueing_enabled":               st.QueueingEnabled,
		"max_active_downloads":           st.MaxActiveDownloads,
		"max_active_uploads":             st.MaxActiveUploads,
		"max_active_torrents":            st.MaxActiveTorrents,
		"dont_count_slow_torrents":       st.DontCountSlowTorrents,
		"slow_torrent_dl_rate_threshold": st.SlowDownloadRate / 1024,
		"slow_torrent_ul_rate_threshold": st.SlowUploadRate / 1024,
		"slow_torrent_inactive_timer":    st.SlowTorrentTime,
		"ip_filter_enabled":              st.IPFilterEnabled,
		"ip_filter_path":                 st.IPFilterPath,
		"ip_filter_trackers":             false,
		"banned_IPs":                     st.BannedIPs,
		"scheduler_enabled":              st.AltSpeedSchedule,
		"schedule_from_hour":             clockPart(st.AltSpeedScheduleFrom, 0),
		"schedule_from_min":              clockPart(st.AltSpeedScheduleFrom, 1),
		"schedule_to_hour":               clockPart(st.AltSpeedScheduleTo, 0),
		"schedule_to_min":                clockPart(st.AltSpeedScheduleTo, 1),
		"scheduler_days":                 schedulerDays(st.AltSpeedScheduleDays),

		// Seeding goals.
		"max_ratio_enabled":                 st.RatioLimit >= 0,
		"max_ratio":                         orDefault(st.RatioLimit, 1),
		"max_seeding_time_enabled":          st.SeedingTimeLimit >= 0,
		"max_seeding_time":                  int64(orDefault(float64(st.SeedingTimeLimit), 1440)),
		"max_inactive_seeding_time_enabled": st.InactiveSeedingTimeLimit >= 0,
		"max_inactive_seeding_time":         int64(orDefault(float64(st.InactiveSeedingTimeLimit), 1440)),
		"max_ratio_act":                     seedGoalAction(st.SeedGoalAction),
		"share_limits_mode":                 shareLimitsMode(st.SeedGoalMode),

		// Web UI
		"locale":                                 "en",
		"web_ui_address":                         "*",
		"web_ui_port":                            s.port,
		"web_ui_username":                        s.auth.Config().AdminUsername,
		"web_ui_session_timeout":                 int(s.auth.Config().IdleTTL.Seconds()),
		"web_ui_max_auth_fail_count":             5,
		"web_ui_ban_duration":                    900,
		"web_ui_csrf_protection_enabled":         true,
		"web_ui_clickjacking_protection_enabled": true,
		"web_ui_secure_cookie_enabled":           s.auth.Config().SecureCookies,
		"web_ui_host_header_validation_enabled":  len(s.allowedHosts) > 0,
		"web_ui_domain_list":                     domainList(s.allowedHosts),
		"bypass_local_auth":                      false,
		"bypass_auth_subnet_whitelist_enabled":   false,
		"bypass_auth_subnet_whitelist":           "",
		"alternative_webui_enabled":              s.altWebUI,

		// RSS
		"rss_processing_enabled":              st.RSSEnabled,
		"rss_refresh_interval":                st.RSSRefreshInterval,
		"rss_fetch_delay":                     st.RSSFetchDelay,
		"rss_max_articles_per_feed":           st.RSSMaxArticles,
		"rss_auto_downloading_enabled":        st.RSSAutoDownload,
		"rss_download_repack_proper_episodes": st.RSSDownloadRepacks,
		"rss_smart_episode_filters":           st.RSSSmartEpisodeFilters,
		"use_https":                           false,
	}
	return p
}

// proxyType is qBittorrent's proxy_type for a piecemeal proxy type.
func proxyType(t string) string {
	switch t {
	case "socks5":
		return "SOCKS5"
	case "http":
		return "HTTP"
	}
	return "None"
}

// btProtocol is qBittorrent's bittorrent_protocol: 0 TCP and uTP, 1 TCP.
func btProtocol(st settings.Settings) int {
	if st.UTP {
		return 0
	}
	return 1
}

func (s *Server) transferInfo(w http.ResponseWriter, _ *http.Request) {
	t := s.engine.Transfer()
	status := t.Connection
	writeJSON(w, map[string]any{
		"dl_info_speed":     t.DownRate,
		"dl_info_data":      t.SessionDownloaded,
		"up_info_speed":     t.UpRate,
		"up_info_data":      t.SessionUploaded,
		"dl_rate_limit":     t.DownloadLimit,
		"up_rate_limit":     t.UploadLimit,
		"dht_nodes":         t.DHTNodes,
		"connection_status": status,
		// piecemeal doesn't learn its external address.
		"last_external_address_v4": "",
		"last_external_address_v6": "",
	})
}

// clockPart is the hour (0) or minute (1) of an HH:MM time.
func clockPart(hhmm string, part int) int {
	parts := strings.Split(hhmm, ":")
	if len(parts) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(parts[part])
	return n
}

// schedulerDays maps the schedule's days to qBittorrent's scheduler_days:
// 0 every day, 1 weekdays, 2 weekends, 3 Monday to 9 Sunday.
func schedulerDays(days string) int {
	order := []string{"everyDay", "weekdays", "weekends", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	for i, d := range order {
		if d == days {
			return i
		}
	}
	return 0
}

// domainList is web_ui_domain_list: qBittorrent separates names with
// semicolons, and "*" accepts any.
func domainList(hosts []string) string {
	if len(hosts) == 0 {
		return "*"
	}
	return strings.Join(hosts, ";")
}
