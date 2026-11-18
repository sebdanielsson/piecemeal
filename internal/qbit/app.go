package qbit

import (
	"net/http"
	"runtime"
	"strconv"

	"github.com/sebdanielsson/piecemeal/internal/settings"
)

func (s *Server) buildInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"qt": "", "libtorrent": "", "boost": "", "openssl": "", "zlib": "",
		"bitness":  strconv.IntSize,
		"platform": runtime.GOOS,
	})
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
func (s *Server) preferences(w http.ResponseWriter, _ *http.Request) {
	st := s.settings.Get()
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
		"temp_path_enabled":             false,
		"temp_path":                     "",
		"auto_tmm_enabled":              false,
		"torrent_content_layout":        "Original",
		"add_stopped_enabled":           st.AddStopped,
		"add_to_top_of_queue":           false,
		"preallocate_all":               false,
		"incomplete_files_ext":          false,
		"use_unwanted_folder":           false,
		"excluded_file_names_enabled":   false,
		"excluded_file_names":           "",
		"torrent_changed_tmm_enabled":   true,
		"save_path_changed_tmm_enabled": false,
		"category_changed_tmm_enabled":  false,

		// Speed
		"dl_limit":                limit(st.DownloadLimit),
		"up_limit":                limit(st.UploadLimit),
		"alt_dl_limit":            limit(st.AltDownloadLimit),
		"alt_up_limit":            limit(st.AltUploadLimit),
		"scheduler_enabled":       false,
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

		// BitTorrent
		"dht":                      st.DHT,
		"pex":                      st.PEX,
		"lsd":                      false,
		"encryption":               encryptionMode(st.Encryption),
		"anonymous_mode":           false,
		"queueing_enabled":         false,
		"max_active_downloads":     -1,
		"max_active_uploads":       -1,
		"max_active_torrents":      -1,
		"dont_count_slow_torrents": false,

		// Seeding goals. piecemeal applies any limit that is reached, like
		// qBittorrent's default share limits mode.
		"max_ratio_enabled":                 st.RatioLimit >= 0,
		"max_ratio":                         orDefault(st.RatioLimit, 1),
		"max_seeding_time_enabled":          st.SeedingTimeLimit >= 0,
		"max_seeding_time":                  int64(orDefault(float64(st.SeedingTimeLimit), 1440)),
		"max_inactive_seeding_time_enabled": st.InactiveSeedingTimeLimit >= 0,
		"max_inactive_seeding_time":         int64(orDefault(float64(st.InactiveSeedingTimeLimit), 1440)),
		"max_ratio_act":                     seedGoalAction(st.SeedGoalAction),
		"share_limits_mode":                 "Default",

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
		"web_ui_host_header_validation_enabled":  false,
		"bypass_local_auth":                      false,
		"bypass_auth_subnet_whitelist_enabled":   false,
		"bypass_auth_subnet_whitelist":           "",
		"alternative_webui_enabled":              false,
		"use_https":                              false,
	}
	writeJSON(w, p)
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
	})
}
