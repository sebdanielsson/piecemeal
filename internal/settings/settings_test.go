package settings

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

func TestEnvName(t *testing.T) {
	for in, want := range map[string]string{
		"downloadLimit":            "PIECEMEAL_DOWNLOAD_LIMIT",
		"dht":                      "PIECEMEAL_DHT",
		"maxConnectionsPerTorrent": "PIECEMEAL_MAX_CONNECTIONS_PER_TORRENT",
		"proxyRSS":                 "PIECEMEAL_PROXY_RSS",
		"bannedIPs":                "PIECEMEAL_BANNED_IPS",
		"ipFilterPath":             "PIECEMEAL_IP_FILTER_PATH",
	} {
		if got := EnvName(in); got != want {
			t.Errorf("EnvName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadUpdateAndEnvOverrides(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	env := []string{"PIECEMEAL_UPLOAD_LIMIT=1000", "PIECEMEAL_DHT=false", "UNRELATED=1"}
	m, err := Load(ctx, st, Defaults("/downloads"), env)
	if err != nil {
		t.Fatal(err)
	}
	if s := m.Get(); s.UploadLimit != 1000 || s.DHT || s.DefaultSavePath != "/downloads" {
		t.Fatalf("loaded %+v", s)
	}
	if l := m.Locked(); len(l) != 2 || l["uploadLimit"] != "PIECEMEAL_UPLOAD_LIMIT" {
		t.Fatalf("locked %v", l)
	}

	var notified bool
	m.Subscribe(func(old, cur Settings) { notified = old.DownloadLimit == 0 && cur.DownloadLimit == 500 })
	if _, err := m.Update(ctx, json.RawMessage(`{"downloadLimit": 500}`)); err != nil {
		t.Fatal(err)
	}
	if !notified {
		t.Fatal("subscriber not notified")
	}

	for _, patch := range []string{
		`{"uploadLimit": 1}`,  // pinned
		`{"nope": 1}`,         // unknown
		`{"encryption": "x"}`, // invalid
		`{"maxActiveCheckingTorrents": 0}`,
		`[1]`, // not an object
	} {
		_, err := m.Update(ctx, json.RawMessage(patch))
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("Update(%s): %v, want a ValidationError", patch, err)
		}
	}

	// Stored values survive a reload; the env still wins.
	m2, err := Load(ctx, st, Defaults("/downloads"), []string{"PIECEMEAL_UPLOAD_LIMIT=7"})
	if err != nil {
		t.Fatal(err)
	}
	if s := m2.Get(); s.DownloadLimit != 500 || s.UploadLimit != 7 || !s.DHT {
		t.Fatalf("reloaded %+v", s)
	}
}

func TestBadEnvValue(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := Load(ctx, st, Defaults("/d"), []string{"PIECEMEAL_DHT=maybe"}); err == nil {
		t.Fatal("no error for a bad boolean")
	}
}

func TestEmptyEnvIsUnset(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := Load(ctx, st, Defaults("/d"), []string{"PIECEMEAL_DOWNLOAD_LIMIT="})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Locked()) != 0 {
		t.Fatalf("locked: %v", m.Locked())
	}
}

func TestAltSpeedWindow(t *testing.T) {
	at := func(day, clock string) time.Time {
		tm, err := time.Parse("2006-01-02 15:04", day+" "+clock)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	// 2026-11-16 is a Monday.
	for _, tc := range []struct {
		from, to, days string
		now            time.Time
		want           bool
	}{
		{"08:00", "20:00", "everyDay", at("2026-11-16", "12:00"), true},
		{"08:00", "20:00", "everyDay", at("2026-11-16", "20:00"), false},
		{"22:00", "06:00", "everyDay", at("2026-11-16", "23:00"), true},
		{"22:00", "06:00", "everyDay", at("2026-11-16", "05:59"), true},
		{"22:00", "06:00", "everyDay", at("2026-11-16", "12:00"), false},
		{"08:00", "20:00", "weekends", at("2026-11-16", "12:00"), false},
		{"08:00", "20:00", "weekdays", at("2026-11-16", "12:00"), true},
		// Friday night's window runs into Saturday morning: it is Friday's.
		{"22:00", "06:00", "friday", at("2026-11-21", "03:00"), true},
		{"22:00", "06:00", "saturday", at("2026-11-21", "03:00"), false},
		{"08:00", "08:00", "everyDay", at("2026-11-16", "08:00"), false},
	} {
		s := Settings{AltSpeedScheduleFrom: tc.from, AltSpeedScheduleTo: tc.to, AltSpeedScheduleDays: tc.days}
		if got := s.InAltSpeedWindow(tc.now); got != tc.want {
			t.Errorf("%s-%s %s at %s: %v, want %v", tc.from, tc.to, tc.days, tc.now.Format("Mon 15:04"), got, tc.want)
		}
	}
}

func TestProxySettings(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := Load(ctx, st, Defaults("/downloads"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Get().ProxyServer() != nil {
		t.Fatal("a proxy by default")
	}

	for _, patch := range []string{
		`{"proxyType": "socks4"}`,
		`{"proxyType": "socks5"}`, // no host
		`{"proxyType": "socks5", "proxyHost": "proxy", "proxyPort": 0}`,
		`{"proxyHost": "socks5://proxy"}`,
		`{"proxyHost": "proxy:1080"}`,
		`{"proxyHost": "pro xy"}`,
		`{"proxyType": "http", "proxyHost": "proxy", "proxyUsername": "a:b"}`,
	} {
		var ve *ValidationError
		if _, err := m.Update(ctx, json.RawMessage(patch)); !errors.As(err, &ve) {
			t.Errorf("%s: got %v, want a validation error", patch, err)
		}
	}

	s, err := m.Update(ctx, json.RawMessage(`{"proxyType": "socks5", "proxyHost": "::1", "proxyPort": 1081,
		"proxyUsername": "user", "proxyPassword": "secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	p := s.ProxyServer()
	if p == nil || p.Addr != "[::1]:1081" || p.Username != "user" || p.Password != "secret" {
		t.Fatalf("proxy %+v", p)
	}
	if s.Public().ProxyPassword != "" || s.Public().ProxyUsername != "user" {
		t.Fatalf("public %+v", s.Public())
	}

	// An empty password, as a client sends back what it read, leaves it.
	if s, err = m.Update(ctx, json.RawMessage(`{"proxyPassword": "", "proxyPort": 1082}`)); err != nil {
		t.Fatal(err)
	}
	if s.ProxyPassword != "secret" || s.ProxyPort != 1082 {
		t.Fatalf("after an empty password: %+v", s)
	}

	// Clearing the username clears the password, in the store too.
	if s, err = m.Update(ctx, json.RawMessage(`{"proxyUsername": ""}`)); err != nil {
		t.Fatal(err)
	}
	if s.ProxyPassword != "" || s.ProxyServer().Username != "" {
		t.Fatalf("after clearing the username: %+v", s)
	}
	m2, err := Load(ctx, st, Defaults("/downloads"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := m2.Get(); got.ProxyPassword != "" || got.ProxyPort != 1082 || got.ProxyHost != "::1" {
		t.Fatalf("reloaded %+v", got)
	}
}

// TestPublicClearsWriteOnly checks that Public clears every write-only
// setting, so a new one can't be added without it.
func TestPublicClearsWriteOnly(t *testing.T) {
	var s Settings
	rv := reflect.ValueOf(&s).Elem()
	for name := range writeOnly {
		rv.FieldByIndex(fieldsByName[name].Index).SetString("secret")
	}
	if len(writeOnly) == 0 {
		t.Fatal("no write-only settings")
	}
	pub := toFields(s.Public())
	for name := range writeOnly {
		if string(pub[name]) != `""` {
			t.Errorf("Public leaves %s: %s", name, pub[name])
		}
	}
}

// TestPinnedProxyPassword checks that a password set by the environment
// survives until a username is entered, which it can't be without.
func TestPinnedProxyPassword(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := Load(ctx, st, Defaults("/downloads"), []string{"PIECEMEAL_PROXY_PASSWORD=pinned"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.Update(ctx, json.RawMessage(`{"proxyType": "socks5", "proxyHost": "proxy", "proxyUsername": "user"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := s.ProxyServer(); p.Username != "user" || p.Password != "pinned" {
		t.Fatalf("proxy %+v", p)
	}
	// Clearing and setting the username again keeps it too.
	for _, patch := range []string{`{"proxyUsername": ""}`, `{"proxyUsername": "user"}`} {
		if s, err = m.Update(ctx, json.RawMessage(patch)); err != nil {
			t.Fatal(err)
		}
	}
	if s.ProxyPassword != "pinned" {
		t.Fatalf("password %q", s.ProxyPassword)
	}
}
