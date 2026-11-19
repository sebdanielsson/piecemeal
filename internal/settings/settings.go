// Package settings holds piecemeal's runtime settings: the ones that can be
// changed through the API while it runs.
//
// Values come from three places, highest priority first:
//
//  1. Environment variables. A setting set this way is pinned: the API
//     reports it as read-only and refuses to change it.
//  2. The settings table in the store, written when the API changes a value.
//  3. The defaults below.
//
// Each setting's environment variable is PIECEMEAL_ followed by its JSON name
// in upper snake case: downloadLimit is PIECEMEAL_DOWNLOAD_LIMIT.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sebdanielsson/piecemeal/internal/proxy"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Settings are the runtime settings. Rates are in bytes per second, where 0
// means unlimited. Seed goals use -1 for unlimited.
type Settings struct {
	// Downloads
	DefaultSavePath string `json:"defaultSavePath" doc:"Where torrents are saved when neither the request nor its category names a path."`
	AddStopped      bool   `json:"addStopped" doc:"Add new torrents stopped."`

	// Incomplete folder
	IncompletePathEnabled bool   `json:"incompletePathEnabled" doc:"Keep incomplete torrents in incompletePath; each moves to its save path once complete. A category or a torrent can choose otherwise."`
	IncompletePath        string `json:"incompletePath" doc:"Where incomplete torrents are kept while incompletePathEnabled is on."`

	// Automatic torrent management
	AutoManagement               bool `json:"autoManagement" doc:"New torrents use automatic management, unless the request says otherwise: their save path and incomplete folder follow their category."`
	RelocateOnCategoryChange     bool `json:"relocateOnCategoryChange" doc:"When an automatically managed torrent's category changes, move its data to the new category's paths. Off switches the torrent to manual management instead."`
	RelocateOnDefaultPathChange  bool `json:"relocateOnDefaultPathChange" doc:"When the default save path or the incomplete folder changes, move the data of automatically managed torrents that use it. Off switches them to manual management instead."`
	RelocateOnCategoryPathChange bool `json:"relocateOnCategoryPathChange" doc:"When a category's save path or incomplete folder changes, move the data of its automatically managed torrents. Off switches them to manual management instead."`

	// Speed
	DownloadLimit    int64 `json:"downloadLimit" minimum:"0" doc:"Global download limit in bytes per second, 0 for unlimited."`
	UploadLimit      int64 `json:"uploadLimit" minimum:"0" doc:"Global upload limit in bytes per second, 0 for unlimited."`
	AltDownloadLimit int64 `json:"altDownloadLimit" minimum:"0" doc:"Alternative download limit, used while altSpeedEnabled is on."`
	AltUploadLimit   int64 `json:"altUploadLimit" minimum:"0" doc:"Alternative upload limit, used while altSpeedEnabled is on."`
	AltSpeedEnabled  bool  `json:"altSpeedEnabled" doc:"Use the alternative limits."`

	// Connection
	PeerPort                 int  `json:"peerPort" minimum:"0" maximum:"65535" doc:"Port to accept peers on. NAT-PMP, when configured, overrides it. 0 picks a random port."`
	MaxConnectionsPerTorrent int  `json:"maxConnectionsPerTorrent" minimum:"1" doc:"Established peer connections per torrent."`
	MaxHalfOpenConnections   int  `json:"maxHalfOpenConnections" minimum:"1" doc:"Outgoing connection attempts in progress, across all torrents."`
	UTP                      bool `json:"utp" doc:"Use uTP as well as TCP for peer connections."`

	// BitTorrent
	DHT        bool   `json:"dht" doc:"Use the DHT to find peers."`
	PEX        bool   `json:"pex" doc:"Exchange peers with other peers."`
	Encryption string `json:"encryption" enum:"prefer,require,disable" doc:"Peer connection encryption: prefer, require or disable."`

	// Proxy
	ProxyType            string `json:"proxyType" enum:"none,socks5,http" doc:"The proxy server's type: none, socks5, or http (an HTTP proxy that allows CONNECT to any port)."`
	ProxyHost            string `json:"proxyHost" doc:"The proxy server's hostname or IP address."`
	ProxyPort            int    `json:"proxyPort" minimum:"0" maximum:"65535" doc:"The proxy server's port."`
	ProxyUsername        string `json:"proxyUsername" maxLength:"255" doc:"The username for the proxy, empty for none."`
	ProxyPassword        string `json:"proxyPassword" maxLength:"255" writeOnly:"true" doc:"The password for the proxy. Write-only: reads return an empty string, and an empty value leaves the password as it is. Clearing proxyUsername clears it."`
	ProxyBitTorrent      bool   `json:"proxyBitTorrent" doc:"Announce to trackers and download from web seeds through the proxy. UDP trackers need a SOCKS5 proxy that relays UDP, and aren't used otherwise."`
	ProxyPeerConnections bool   `json:"proxyPeerConnections" doc:"With proxyBitTorrent, connect to peers through the proxy too. The DHT, uTP and WebRTC peers can't go through it, so they are off meanwhile."`
	ProxyHostnameLookup  bool   `json:"proxyHostnameLookup" doc:"Let the proxy look up the hostnames of trackers and web seeds, so they aren't looked up locally. UDP trackers are always looked up locally."`
	ProxyRSS             bool   `json:"proxyRSS" doc:"Fetch RSS feeds through the proxy."`
	ProxyTorrentFiles    bool   `json:"proxyTorrentFiles" doc:"Download torrent files from URLs through the proxy, whether added by URL or by an RSS rule. Addresses on PIECEMEAL_FETCH_ALLOWED_NETWORKS are reached directly."`

	// Seeding goals. A torrent's own goals override these, then its
	// category's.
	RatioLimit               float64 `json:"ratioLimit" doc:"Stop seeding at this upload ratio, -1 for unlimited."`
	SeedingTimeLimit         int64   `json:"seedingTimeLimit" doc:"Stop seeding after this many minutes, -1 for unlimited."`
	InactiveSeedingTimeLimit int64   `json:"inactiveSeedingTimeLimit" doc:"Stop seeding after this many minutes without upload, -1 for unlimited."`
	SeedGoalAction           string  `json:"seedGoalAction" enum:"stop,remove,removeWithData" doc:"What to do once a torrent reaches its seeding goals."`
	SeedGoalMode             string  `json:"seedGoalMode" enum:"any,all" doc:"Whether a torrent reaches its seeding goals when any one is met, or only when all that are set are."`

	// Queueing
	QueueingEnabled       bool  `json:"queueingEnabled" doc:"Run only a limited number of torrents at a time; the rest wait in the queue."`
	MaxActiveDownloads    int   `json:"maxActiveDownloads" minimum:"-1" doc:"Torrents downloading at once, -1 for unlimited."`
	MaxActiveUploads      int   `json:"maxActiveUploads" minimum:"-1" doc:"Torrents seeding at once, -1 for unlimited."`
	MaxActiveTorrents     int   `json:"maxActiveTorrents" minimum:"-1" doc:"Torrents running at once, -1 for unlimited."`
	DontCountSlowTorrents bool  `json:"dontCountSlowTorrents" doc:"Slow torrents don't count toward the limits."`
	SlowDownloadRate      int64 `json:"slowDownloadRate" minimum:"0" doc:"A torrent below this download rate, in bytes per second, is slow."`
	SlowUploadRate        int64 `json:"slowUploadRate" minimum:"0" doc:"A torrent below this upload rate, in bytes per second, is slow."`
	SlowTorrentTime       int64 `json:"slowTorrentTime" minimum:"0" doc:"Seconds a torrent must stay below the slow rates to count as slow."`

	// Checking
	MaxActiveCheckingTorrents int `json:"maxActiveCheckingTorrents" minimum:"1" doc:"Torrents whose data is checked at once; the rest wait, queued for checking."`

	// IP filter
	IPFilterEnabled bool   `json:"ipFilterEnabled" doc:"Refuse peers in the ranges of the IP filter file."`
	IPFilterPath    string `json:"ipFilterPath" doc:"An IP filter file: eMule .dat, PeerGuardian .p2p, or one IP address or CIDR range per line."`
	BannedIPs       string `json:"bannedIPs" doc:"Peers to refuse: IP addresses or CIDR ranges, one per line."`

	// Alternative speed schedule
	AltSpeedSchedule     bool   `json:"altSpeedSchedule" doc:"Turn the alternative limits on and off on a schedule."`
	AltSpeedScheduleFrom string `json:"altSpeedScheduleFrom" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"When the alternative limits start, HH:MM local time."`
	AltSpeedScheduleTo   string `json:"altSpeedScheduleTo" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"When they end, HH:MM local time; earlier than the start means the next day."`
	AltSpeedScheduleDays string `json:"altSpeedScheduleDays" enum:"everyDay,weekdays,weekends,monday,tuesday,wednesday,thursday,friday,saturday,sunday" doc:"The days the schedule applies, by the day it starts."`

	// RSS
	RSSEnabled             bool   `json:"rssEnabled" doc:"Fetch RSS feeds on their schedule."`
	RSSRefreshInterval     int    `json:"rssRefreshInterval" minimum:"1" doc:"Minutes between fetches of a feed without its own interval."`
	RSSFetchDelay          int    `json:"rssFetchDelay" minimum:"0" doc:"Seconds between requests to the same host."`
	RSSMaxArticles         int    `json:"rssMaxArticles" minimum:"0" doc:"Articles kept per feed; older ones are dropped."`
	RSSAutoDownload        bool   `json:"rssAutoDownload" doc:"Add torrents from new articles that RSS rules match."`
	RSSDownloadRepacks     bool   `json:"rssDownloadRepacks" doc:"With the smart episode filter, also take a REPACK or PROPER of an episode already downloaded."`
	RSSSmartEpisodeFilters string `json:"rssSmartEpisodeFilters" doc:"Regular expressions, one per line, that find the episode in a title for the smart episode filter: two groups for season and episode, or one for a date."`
}

// DefaultSmartEpisodeFilters recognise SxxEyy, NxNN and dates.
const DefaultSmartEpisodeFilters = `s(\d+)e(\d+)
(\d+)x(\d+)
(\d{4}[.\-]\d{1,2}[.\-]\d{1,2})
(\d{1,2}[.\-]\d{1,2}[.\-]\d{4})`

// Defaults returns the built-in defaults. defaultSavePath is the configured
// download directory.
func Defaults(defaultSavePath string) Settings {
	return Settings{
		DefaultSavePath:          defaultSavePath,
		IncompletePath:           filepath.Join(defaultSavePath, "temp"),
		RelocateOnCategoryChange: true,
		PeerPort:                 6881,
		MaxConnectionsPerTorrent: 50,
		MaxHalfOpenConnections:   100,
		UTP:                      true,
		DHT:                      true,
		PEX:                      true,
		Encryption:               "prefer",
		ProxyType:                "none",
		ProxyPort:                1080,
		ProxyBitTorrent:          true,
		ProxyPeerConnections:     true,
		ProxyHostnameLookup:      true,
		ProxyRSS:                 true,
		ProxyTorrentFiles:        true,
		RatioLimit:               -1,
		SeedingTimeLimit:         -1,
		InactiveSeedingTimeLimit: -1,
		SeedGoalAction:           "stop",
		SeedGoalMode:             "any",
		MaxActiveDownloads:       3,
		MaxActiveUploads:         3,
		MaxActiveTorrents:        5,
		SlowDownloadRate:         2 << 10,
		SlowUploadRate:           2 << 10,
		SlowTorrentTime:          60,
		AltSpeedScheduleFrom:     "08:00",
		AltSpeedScheduleTo:       "20:00",
		AltSpeedScheduleDays:     "everyDay",
		RSSRefreshInterval:       30,
		RSSFetchDelay:            2,
		RSSMaxArticles:           50,
		RSSDownloadRepacks:       true,
		RSSSmartEpisodeFilters:   DefaultSmartEpisodeFilters,

		// One at a time, so hard drives read one torrent's data at a time.
		MaxActiveCheckingTorrents: 1,
	}
}

// Validate checks values the JSON schema can't express.
func (s Settings) Validate() error {
	var errs []error
	if s.DefaultSavePath == "" {
		errs = append(errs, errors.New("defaultSavePath: must not be empty"))
	}
	if s.IncompletePathEnabled && s.IncompletePath == "" {
		errs = append(errs, errors.New("incompletePath: must not be empty while incompletePathEnabled is on"))
	}
	for name, v := range map[string]int64{
		"downloadLimit": s.DownloadLimit, "uploadLimit": s.UploadLimit,
		"altDownloadLimit": s.AltDownloadLimit, "altUploadLimit": s.AltUploadLimit,
	} {
		if v < 0 {
			errs = append(errs, fmt.Errorf("%s: must not be negative", name))
		}
	}
	if s.PeerPort < 0 || s.PeerPort > 65535 {
		errs = append(errs, errors.New("peerPort: must be between 0 and 65535"))
	}
	if s.MaxConnectionsPerTorrent < 1 {
		errs = append(errs, errors.New("maxConnectionsPerTorrent: must be at least 1"))
	}
	if s.MaxHalfOpenConnections < 1 {
		errs = append(errs, errors.New("maxHalfOpenConnections: must be at least 1"))
	}
	if !slices.Contains([]string{"prefer", "require", "disable"}, s.Encryption) {
		errs = append(errs, errors.New("encryption: must be prefer, require or disable"))
	}
	errs = append(errs, s.validateProxy()...)
	if s.RatioLimit < 0 && s.RatioLimit != -1 {
		errs = append(errs, errors.New("ratioLimit: must be -1 or at least 0"))
	}
	if s.SeedingTimeLimit < -1 || s.InactiveSeedingTimeLimit < -1 {
		errs = append(errs, errors.New("seeding time limits: must be -1 or at least 0"))
	}
	if !slices.Contains([]string{"stop", "remove", "removeWithData"}, s.SeedGoalAction) {
		errs = append(errs, errors.New("seedGoalAction: must be stop, remove or removeWithData"))
	}
	if !slices.Contains([]string{"any", "all"}, s.SeedGoalMode) {
		errs = append(errs, errors.New("seedGoalMode: must be any or all"))
	}
	for name, v := range map[string]int{
		"maxActiveDownloads": s.MaxActiveDownloads, "maxActiveUploads": s.MaxActiveUploads, "maxActiveTorrents": s.MaxActiveTorrents,
	} {
		if v < -1 {
			errs = append(errs, fmt.Errorf("%s: must be -1 or at least 0", name))
		}
	}
	if s.MaxActiveCheckingTorrents < 1 {
		errs = append(errs, errors.New("maxActiveCheckingTorrents: must be at least 1"))
	}
	if s.SlowDownloadRate < 0 || s.SlowUploadRate < 0 || s.SlowTorrentTime < 0 {
		errs = append(errs, errors.New("slow torrent thresholds: must not be negative"))
	}
	for name, v := range map[string]string{"altSpeedScheduleFrom": s.AltSpeedScheduleFrom, "altSpeedScheduleTo": s.AltSpeedScheduleTo} {
		if _, err := ParseClock(v); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if !slices.Contains(scheduleDays, s.AltSpeedScheduleDays) {
		errs = append(errs, errors.New("altSpeedScheduleDays: unknown value"))
	}
	if s.RSSRefreshInterval < 1 {
		errs = append(errs, errors.New("rssRefreshInterval: must be at least 1"))
	}
	if s.RSSFetchDelay < 0 || s.RSSMaxArticles < 0 {
		errs = append(errs, errors.New("rssFetchDelay and rssMaxArticles: must not be negative"))
	}
	return errors.Join(errs...)
}

func (s Settings) validateProxy() []error {
	var errs []error
	if !slices.Contains([]string{"none", proxy.SOCKS5, proxy.HTTP}, s.ProxyType) {
		errs = append(errs, errors.New("proxyType: must be none, socks5 or http"))
	}
	if s.ProxyPort < 0 || s.ProxyPort > 65535 {
		errs = append(errs, errors.New("proxyPort: must be between 0 and 65535"))
	}
	host := s.ProxyHost
	if _, err := netip.ParseAddr(host); err != nil &&
		(strings.ContainsAny(host, ":/@?# \t\r\n[]") || len(host) > 253) {
		errs = append(errs, errors.New("proxyHost: must be a hostname or IP address, without a scheme or port"))
	}
	if s.ProxyType != "none" {
		if host == "" {
			errs = append(errs, errors.New("proxyHost: must not be empty while a proxy type is set"))
		}
		if s.ProxyPort == 0 {
			errs = append(errs, errors.New("proxyPort: must be set while a proxy type is set"))
		}
	}
	// SOCKS5 sends each in a length byte.
	if len(s.ProxyUsername) > 255 || len(s.ProxyPassword) > 255 {
		errs = append(errs, errors.New("proxyUsername and proxyPassword: must be at most 255 bytes each"))
	}
	if s.ProxyType == proxy.HTTP && strings.Contains(s.ProxyUsername, ":") {
		errs = append(errs, errors.New("proxyUsername: an HTTP proxy's username can't contain a colon"))
	}
	return errs
}

// ProxyServer returns the proxy server, or nil when proxyType is none.
func (s Settings) ProxyServer() *proxy.Server {
	if s.ProxyType == "none" || s.ProxyType == "" {
		return nil
	}
	p := &proxy.Server{
		Type: s.ProxyType,
		Addr: net.JoinHostPort(s.ProxyHost, strconv.Itoa(s.ProxyPort)),
	}
	if s.ProxyUsername != "" {
		p.Username, p.Password = s.ProxyUsername, s.ProxyPassword
	}
	return p
}

// Public returns the settings without write-only values, for responses.
func (s Settings) Public() Settings {
	s.ProxyPassword = ""
	return s
}

// RedactPatch returns a settings patch for logs and the audit trail, with
// the values of write-only settings replaced.
func RedactPatch(patch []byte) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(patch, &fields); err != nil {
		return "(not a JSON object)"
	}
	for name := range fields {
		if writeOnly[name] {
			fields[name] = json.RawMessage(`"(hidden)"`)
		}
	}
	b, _ := json.Marshal(fields)
	return string(b)
}

// normalize clears values that mean nothing without another: the proxy
// password without a username.
func (s *Settings) normalize() {
	if s.ProxyUsername == "" {
		s.ProxyPassword = ""
	}
}

// Manager holds the current settings and persists changes.
type Manager struct {
	store *store.Store

	// update serialises changes, including the subscriber calls, so
	// subscribers see them in the order they were stored.
	update sync.Mutex

	mu         sync.RWMutex
	cur        Settings
	locked     map[string]string // JSON name -> environment variable
	subs       []func(old, cur Settings)
	validators []func(Settings) error
}

// Load reads stored settings over the defaults and applies environment
// overrides from environ (os.Environ's format).
func Load(ctx context.Context, st *store.Store, defaults Settings, environ []string) (*Manager, error) {
	cur := defaults
	rows, err := st.ListSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	stored := map[string]json.RawMessage{}
	for _, r := range rows {
		stored[r.Key] = json.RawMessage(r.Value)
	}
	if len(stored) > 0 {
		// Settings that no longer exist are ignored rather than fatal, so a
		// downgrade keeps working.
		for k := range stored {
			if _, ok := fieldsByName[k]; !ok {
				delete(stored, k)
			}
		}
		b, _ := json.Marshal(stored)
		if err := json.Unmarshal(b, &cur); err != nil {
			return nil, fmt.Errorf("stored settings: %w", err)
		}
	}

	env := map[string]string{}
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	locked := map[string]string{}
	rv := reflect.ValueOf(&cur).Elem()
	for name, f := range fieldsByName {
		envName := EnvName(name)
		v, ok := env[envName]
		if !ok || v == "" {
			// Empty means unset, as for the startup configuration.
			continue
		}
		if err := setFromString(rv.FieldByIndex(f.Index), v); err != nil {
			return nil, fmt.Errorf("%s: %w", envName, err)
		}
		locked[name] = envName
	}
	cur.normalize()
	if err := cur.Validate(); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	return &Manager{store: st, cur: cur, locked: locked}, nil
}

// Get returns the current settings.
func (m *Manager) Get() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Locked returns the JSON names of settings pinned by environment variables,
// mapped to the variable.
func (m *Manager) Locked() map[string]string {
	return maps.Clone(m.locked)
}

// AddValidator adds a check that changes must pass, for constraints from
// outside this package such as the allowed save roots.
func (m *Manager) AddValidator(fn func(Settings) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validators = append(m.validators, fn)
}

// Subscribe registers fn to run after every change, with the old and new
// settings. It runs synchronously in Update's caller.
func (m *Manager) Subscribe(fn func(old, cur Settings)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs = append(m.subs, fn)
}

// ErrLocked is returned when a change touches a setting pinned by an
// environment variable.
var ErrLocked = errors.New("set by an environment variable")

// ValidationError wraps errors in the requested values.
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

// Update applies a partial JSON object of settings, persists the changed
// values and returns the result. Unknown and pinned settings are rejected.
func (m *Manager) Update(ctx context.Context, patch json.RawMessage) (Settings, error) {
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(patch))
	if err := dec.Decode(&fields); err != nil {
		return Settings{}, &ValidationError{fmt.Errorf("settings must be a JSON object: %w", err)}
	}
	return m.apply(ctx, fields)
}

// Set changes settings from Go values keyed by JSON name.
func (m *Manager) Set(ctx context.Context, values map[string]any) (Settings, error) {
	fields := map[string]json.RawMessage{}
	for k, v := range values {
		b, err := json.Marshal(v)
		if err != nil {
			return Settings{}, &ValidationError{fmt.Errorf("%s: %w", k, err)}
		}
		fields[k] = b
	}
	return m.apply(ctx, fields)
}

func (m *Manager) apply(ctx context.Context, fields map[string]json.RawMessage) (Settings, error) {
	m.update.Lock()
	defer m.update.Unlock()
	// A write-only setting reads back empty, so a client that sends back
	// what it read sends it empty: that leaves it as it is.
	for name := range writeOnly {
		if v, ok := fields[name]; ok && string(bytes.TrimSpace(v)) == `""` {
			delete(fields, name)
		}
	}
	m.mu.Lock()
	old := m.cur
	next := m.cur
	var errs []error
	for name := range fields {
		if _, ok := fieldsByName[name]; !ok {
			errs = append(errs, fmt.Errorf("%s: unknown setting", name))
			continue
		}
		if env, ok := m.locked[name]; ok {
			errs = append(errs, fmt.Errorf("%s: %w %s", name, ErrLocked, env))
		}
	}
	if len(errs) > 0 {
		m.mu.Unlock()
		return Settings{}, &ValidationError{errors.Join(errs...)}
	}
	b, _ := json.Marshal(fields)
	if err := json.Unmarshal(b, &next); err != nil {
		m.mu.Unlock()
		return Settings{}, &ValidationError{err}
	}
	next.normalize()
	if err := next.Validate(); err != nil {
		m.mu.Unlock()
		return Settings{}, &ValidationError{err}
	}
	for _, v := range m.validators {
		if err := v(next); err != nil {
			m.mu.Unlock()
			return Settings{}, &ValidationError{err}
		}
	}

	// The settings in the patch are stored, and any that normalize changed
	// with them, unless an environment variable sets those.
	oldJSON, nextJSON := toFields(old), toFields(next)
	err := m.store.Tx(ctx, func(q *store.Queries) error {
		for name := range fieldsByName {
			_, inPatch := fields[name]
			_, pinned := m.locked[name]
			if !inPatch && (pinned || bytes.Equal(oldJSON[name], nextJSON[name])) {
				continue
			}
			if err := q.UpsertSetting(ctx, store.UpsertSettingParams{Key: name, Value: string(nextJSON[name])}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		m.mu.Unlock()
		return Settings{}, fmt.Errorf("save settings: %w", err)
	}
	m.cur = next
	subs := slices.Clone(m.subs)
	m.mu.Unlock()

	if old != next {
		for _, fn := range subs {
			fn(old, next)
		}
	}
	return next, nil
}

// EnvName is the environment variable for a setting's JSON name.
func EnvName(jsonName string) string {
	var b strings.Builder
	b.WriteString("PIECEMEAL_")
	for i, r := range jsonName {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// Names returns every setting's JSON name.
func Names() []string {
	return slices.Sorted(maps.Keys(fieldsByName))
}

var fieldsByName = func() map[string]reflect.StructField {
	m := map[string]reflect.StructField{}
	t := reflect.TypeFor[Settings]()
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		m[name] = f
	}
	return m
}()

// writeOnly are the settings that responses leave out (see Public).
var writeOnly = func() map[string]bool {
	m := map[string]bool{}
	for name, f := range fieldsByName {
		if f.Tag.Get("writeOnly") == "true" {
			m[name] = true
		}
	}
	return m
}()

func toFields(s Settings) map[string]json.RawMessage {
	b, _ := json.Marshal(s)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	return m
}

func setFromString(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		v.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("unsupported setting type %s", v.Kind())
	}
	return nil
}

var scheduleDays = []string{
	"everyDay", "weekdays", "weekends", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
}

// ParseClock parses HH:MM into minutes after midnight.
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// InAltSpeedWindow reports whether the alternative speed schedule is on at
// now, in now's location.
func (s Settings) InAltSpeedWindow(now time.Time) bool {
	from, err1 := ParseClock(s.AltSpeedScheduleFrom)
	to, err2 := ParseClock(s.AltSpeedScheduleTo)
	if err1 != nil || err2 != nil || from == to {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	// The window belongs to the day it starts on: after midnight, an
	// overnight window is yesterday's.
	day := now.Weekday()
	var in bool
	switch {
	case from < to:
		in = minute >= from && minute < to
	case minute >= from:
		in = true
	case minute < to:
		in = true
		day = (day + 6) % 7
	}
	return in && dayMatches(s.AltSpeedScheduleDays, day)
}

func dayMatches(days string, d time.Weekday) bool {
	switch days {
	case "everyDay":
		return true
	case "weekdays":
		return d >= time.Monday && d <= time.Friday
	case "weekends":
		return d == time.Saturday || d == time.Sunday
	default:
		return strings.EqualFold(days, d.String())
	}
}
