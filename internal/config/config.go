// Package config reads piecemeal's startup configuration from environment
// variables. Everything here needs a restart to change; runtime settings
// live in internal/settings.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the startup configuration.
type Config struct {
	// Addr is the HTTP listen address for the APIs and web UI.
	Addr string
	// DataDir holds the database and piece completion state.
	DataDir string
	// DownloadDir is the default save path.
	DownloadDir string
	// WatchDir, when set, is polled for .torrent and .magnet files.
	WatchDir string
	// AllowedRoots confine every save path. Default: DownloadDir.
	AllowedRoots []string

	// NATPMPGateway, when set, forwards the peer port through NAT-PMP.
	NATPMPGateway net.IP
	// ListenHost binds the peer listeners to one address.
	ListenHost  string
	DisableIPv6 bool
	Mmap        bool

	AdminUsername     string
	AdminPasswordHash string
	LocalLogin        bool
	SecureCookies     bool
	TrustedProxies    []netip.Prefix

	// FetchAllowedNetworks may be fetched from when adding by URL, though
	// private.
	FetchAllowedNetworks []netip.Prefix

	// Metrics is "public", "auth" (an API key or session is needed) or
	// "off".
	Metrics string

	// PublicURL is where people reach piecemeal, such as
	// https://piecemeal.example.com. OIDC needs it for its callback.
	PublicURL string
	OIDC      OIDC

	LogLevel  string
	LogFormat string
}

// OIDC is sign-in through an OpenID Connect provider.
type OIDC struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Name          string
	GroupsClaim   string
	AllowedGroups []string
	AdminGroups   []string
}

// OIDCCallbackPath is the OIDC redirect path, under the public URL.
const OIDCCallbackPath = "/api/piecemeal/v1/auth/oidc/callback"

// FromEnv reads the configuration from getenv (os.Getenv in production).
func FromEnv(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := getenv("PIECEMEAL_" + key); v != "" {
			return v
		}
		return def
	}
	var errs []error
	boolean := func(key string, def bool) bool {
		v := getenv("PIECEMEAL_" + key)
		if v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("PIECEMEAL_%s: %w", key, err))
		}
		return b
	}
	prefixes := func(key string) []netip.Prefix {
		var out []netip.Prefix
		for _, s := range list(get(key, "")) {
			p, err := netip.ParsePrefix(s)
			if err != nil {
				a, aerr := netip.ParseAddr(s)
				if aerr != nil {
					errs = append(errs, fmt.Errorf("PIECEMEAL_%s: %q is not a CIDR or address", key, s))
					continue
				}
				p = netip.PrefixFrom(a, a.BitLen())
			}
			out = append(out, p)
		}
		return out
	}

	c := Config{
		Addr:                 get("ADDR", ":8080"),
		DataDir:              get("DATA_DIR", "data"),
		DownloadDir:          get("DOWNLOAD_DIR", "downloads"),
		WatchDir:             get("WATCH_DIR", ""),
		ListenHost:           get("LISTEN_HOST", ""),
		DisableIPv6:          boolean("DISABLE_IPV6", false),
		Mmap:                 boolean("MMAP", false),
		AdminUsername:        get("ADMIN_USERNAME", "admin"),
		AdminPasswordHash:    get("ADMIN_PASSWORD_HASH", ""),
		LocalLogin:           boolean("LOCAL_LOGIN", true),
		SecureCookies:        boolean("SECURE_COOKIES", true),
		TrustedProxies:       prefixes("TRUSTED_PROXIES"),
		FetchAllowedNetworks: prefixes("FETCH_ALLOWED_NETWORKS"),
		Metrics:              get("METRICS", "auth"),
		LogLevel:             get("LOG_LEVEL", "info"),
		LogFormat:            get("LOG_FORMAT", "json"),
		PublicURL:            strings.TrimRight(get("PUBLIC_URL", ""), "/"),
		OIDC: OIDC{
			Issuer:        get("OIDC_ISSUER", ""),
			ClientID:      get("OIDC_CLIENT_ID", ""),
			ClientSecret:  get("OIDC_CLIENT_SECRET", ""),
			RedirectURL:   get("OIDC_REDIRECT_URL", ""),
			Name:          get("OIDC_NAME", "SSO"),
			GroupsClaim:   get("OIDC_GROUPS_CLAIM", "groups"),
			AllowedGroups: list(get("OIDC_ALLOWED_GROUPS", "")),
			AdminGroups:   list(get("OIDC_ADMIN_GROUPS", "")),
		},
	}
	if c.OIDC.Issuer != "" {
		if c.OIDC.ClientID == "" {
			errs = append(errs, errors.New("PIECEMEAL_OIDC_CLIENT_ID: required with PIECEMEAL_OIDC_ISSUER"))
		}
		if c.OIDC.RedirectURL == "" {
			if c.PublicURL == "" {
				errs = append(errs, errors.New("PIECEMEAL_PUBLIC_URL: required with PIECEMEAL_OIDC_ISSUER, for the callback URL"))
			}
			c.OIDC.RedirectURL = c.PublicURL + OIDCCallbackPath
		}
	}
	if gw := get("NATPMP_GATEWAY", ""); gw != "" {
		c.NATPMPGateway = net.ParseIP(gw)
		if c.NATPMPGateway == nil {
			errs = append(errs, fmt.Errorf("PIECEMEAL_NATPMP_GATEWAY: %q is not an IP address", gw))
		}
	}
	var err error
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		errs = append(errs, err)
	}
	if c.DownloadDir, err = filepath.Abs(c.DownloadDir); err != nil {
		errs = append(errs, err)
	}
	if c.WatchDir != "" {
		if c.WatchDir, err = filepath.Abs(c.WatchDir); err != nil {
			errs = append(errs, err)
		}
	}
	c.AllowedRoots = list(get("ALLOWED_ROOTS", ""))
	if len(c.AllowedRoots) == 0 {
		c.AllowedRoots = []string{c.DownloadDir}
	}
	for i, r := range c.AllowedRoots {
		if !filepath.IsAbs(r) {
			errs = append(errs, fmt.Errorf("PIECEMEAL_ALLOWED_ROOTS: %q is not an absolute path", r))
		}
		c.AllowedRoots[i] = filepath.Clean(r)
	}
	switch c.Metrics {
	case "public", "auth", "off":
	default:
		errs = append(errs, fmt.Errorf("PIECEMEAL_METRICS: %q is not public, auth or off", c.Metrics))
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("PIECEMEAL_LOG_LEVEL: %q is not debug, info, warn or error", c.LogLevel))
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("PIECEMEAL_LOG_FORMAT: %q is not json or text", c.LogFormat))
	}
	return c, errors.Join(errs...)
}

// FromOS reads the configuration from the process environment.
func FromOS() (Config, error) { return FromEnv(os.Getenv) }

func list(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
