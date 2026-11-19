// Package fetch downloads .torrent files from URLs given to the add
// endpoints, and RSS feeds. It is the one place piecemeal fetches arbitrary
// URLs, so it refuses private, loopback and link-local addresses unless
// allowed, and limits size, redirects and time.
//
// Through a proxy, piecemeal still looks hostnames up itself and checks
// the addresses, then asks the proxy for the address it checked. Addresses
// that are only allowed by the allowed networks are private, so they are
// reached directly: a proxy elsewhere couldn't reach them.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sebdanielsson/piecemeal/internal/proxy"
)

// Fetcher downloads torrent files and feeds.
type Fetcher struct {
	torrents, feeds *http.Client
	jar             *jar
	maxSize         int64
	allowed         []netip.Prefix
	dialer          *net.Dialer
	proxy           atomic.Pointer[Proxy]
	// public reports whether an address is public, rather than allowed
	// only by the allowed networks; tests replace it.
	public func(netip.Addr) bool
}

// Proxy is the proxy the fetcher uses, and for what.
type Proxy struct {
	// Server is the proxy; nil for none.
	Server *proxy.Server
	// TorrentFiles and Feeds choose what goes through it.
	TorrentFiles, Feeds bool
}

// SetProxy changes the proxy. Connections already open are closed once
// idle, so later requests take the new route.
func (f *Fetcher) SetProxy(p Proxy) {
	f.proxy.Store(&p)
	f.torrents.CloseIdleConnections()
	f.feeds.CloseIdleConnections()
}

// ErrForbidden is returned for URLs that resolve to a blocked address.
var ErrForbidden = errors.New("address not allowed")

// New returns a fetcher that may also reach the allowed networks, which
// are otherwise blocked as private.
func New(allowed []netip.Prefix) *Fetcher {
	f := &Fetcher{
		maxSize: 16 << 20,
		jar:     &jar{now: time.Now},
		allowed: allowed,
		public:  func(ip netip.Addr) bool { return Allowed(ip, nil) },
	}
	f.dialer = &net.Dialer{
		Timeout: 10 * time.Second,
		// Control sees the address after DNS resolution, so a hostname that
		// resolves to a private address is caught too.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if !Allowed(ip, allowed) {
				// The address isn't named: it would tell users what internal
				// names resolve to.
				return ErrForbidden
			}
			return nil
		},
	}
	// Torrent files and feeds have their own connections, since they may
	// take different routes.
	f.torrents = f.newClient(func(p *Proxy) bool { return p.TorrentFiles })
	f.feeds = f.newClient(func(p *Proxy) bool { return p.Feeds })
	return f
}

func (f *Fetcher) newClient(proxied func(*Proxy) bool) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if p := f.proxy.Load(); p != nil && p.Server != nil && proxied(p) {
				return f.dialProxied(ctx, p.Server, network, addr)
			}
			return f.dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		// Never the environment's proxy: DialContext checks addresses and
		// uses the configured proxy itself.
		Proxy: nil,
	}
	return &http.Client{
		Transport: transport,
		Jar:       f.jar,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// dialProxied looks addr up, checks its addresses as a direct dial would,
// and connects to the first that works: through the proxy, or directly to
// a private address that the allowed networks let through.
func (f *Fetcher) dialProxied(ctx context.Context, srv *proxy.Server, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else if ips, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
		return nil, err
	}
	var errs []error
	for _, ip := range ips {
		ip = ip.Unmap()
		if !Allowed(ip, f.allowed) {
			errs = append(errs, ErrForbidden)
			continue
		}
		target := net.JoinHostPort(ip.String(), port)
		var c net.Conn
		if f.public(ip) {
			c, err = srv.DialContext(ctx, "tcp", target)
		} else {
			c, err = f.dialer.DialContext(ctx, network, target)
		}
		if err == nil {
			return c, nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil || errors.Is(err, proxy.ErrUnreachable) {
			break
		}
	}
	return nil, errors.Join(errs...)
}

// Allowed reports whether ip may be fetched from.
func Allowed(ip netip.Addr, allowed []netip.Prefix) bool {
	ip = ip.Unmap()
	for _, p := range allowed {
		if p.Contains(ip) {
			return true
		}
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	// A NAT64 gateway or a 6to4 or Teredo relay forwards these to the IPv4
	// address inside, which may be a private one.
	if v4, ok := embeddedIPv4(ip); ok {
		return Allowed(v4, allowed)
	}
	return true
}

var (
	// Special-purpose ranges (RFC 6890) that hold no public hosts.
	blockedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		// Carrier-grade NAT (RFC 6598), where Tailscale addresses live.
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("240.0.0.0/4"),
		// Deprecated site-local.
		netip.MustParsePrefix("fec0::/10"),
		// Local-use NAT64 (RFC 8215), which can put the IPv4 address
		// anywhere in the prefix.
		netip.MustParsePrefix("64:ff9b:1::/48"),
	}
	nat64          = netip.MustParsePrefix("64:ff9b::/96")
	ipv4Compatible = netip.MustParsePrefix("::/96")
	sixToFour      = netip.MustParsePrefix("2002::/16")
	teredo         = netip.MustParsePrefix("2001::/32")
)

// embeddedIPv4 returns the IPv4 address that traffic to an IPv6 address is
// forwarded to, for the ranges that embed one.
func embeddedIPv4(ip netip.Addr) (netip.Addr, bool) {
	if !ip.Is6() {
		return netip.Addr{}, false
	}
	b := ip.As16()
	switch {
	case nat64.Contains(ip), ipv4Compatible.Contains(ip):
		return netip.AddrFrom4([4]byte(b[12:16])), true
	case sixToFour.Contains(ip):
		return netip.AddrFrom4([4]byte(b[2:6])), true
	case teredo.Contains(ip):
		// The client's address, inverted.
		return netip.AddrFrom4([4]byte{^b[12], ^b[13], ^b[14], ^b[15]}), true
	}
	return netip.Addr{}, false
}

// Result is either a magnet URI (when the URL redirects to one) or the
// body of a torrent file.
type Result struct {
	Magnet string
	Body   []byte
}

// fetchableURL is an http(s) URL with a host. Which addresses the host
// may resolve to is checked when dialing.
var fetchableURL = regexp.MustCompile(`(?i)^https?://[^/?#\s]+([/?#]\S*)?$`)

// Get downloads a torrent file, or finds the magnet URI rawURL leads to.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (Result, error) {
	return f.get(ctx, f.torrents, rawURL)
}

// GetFeed downloads an RSS feed.
func (f *Fetcher) GetFeed(ctx context.Context, rawURL string) (Result, error) {
	return f.get(ctx, f.feeds, rawURL)
}

func (f *Fetcher) get(ctx context.Context, client *http.Client, rawURL string) (Result, error) {
	if strings.HasPrefix(strings.ToLower(rawURL), "magnet:") {
		return Result{Magnet: rawURL}, nil
	}
	if !fetchableURL.MatchString(rawURL) {
		return Result{}, fmt.Errorf("bad URL %s: only http and https URLs with a host can be fetched", RedactString(rawURL))
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return Result{}, errors.New("bad URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "piecemeal")
	resp, err := client.Do(req)
	if err != nil {
		// The dial error names the blocked address the host resolved to.
		if errors.Is(err, ErrForbidden) {
			return Result{}, fmt.Errorf("%s: %w", Redact(u), ErrForbidden)
		}
		// *url.Error names the URL, query and all.
		var ue *url.Error
		if errors.As(err, &ue) {
			return Result{}, fmt.Errorf("%s: %w", Redact(u), ue.Err)
		}
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Indexers often redirect a download link to a magnet URI.
	if loc := resp.Header.Get("Location"); resp.StatusCode/100 == 3 && strings.HasPrefix(loc, "magnet:") {
		return Result{Magnet: loc}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("%s: %s", Redact(u), resp.Status)
	}
	if resp.ContentLength > f.maxSize {
		return Result{}, fmt.Errorf("%s: larger than %d bytes", Redact(u), f.maxSize)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, f.maxSize+1))
	if err != nil {
		return Result{}, err
	}
	if int64(len(b)) > f.maxSize {
		return Result{}, fmt.Errorf("%s: larger than %d bytes", Redact(u), f.maxSize)
	}
	return Result{Body: b}, nil
}

// Redact is a URL for messages: scheme, host and path. Credentials such as
// API keys and passkeys usually sit in the query or the user info.
func Redact(u *url.URL) string {
	r := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	if u.RawQuery != "" {
		r.RawQuery = "…"
	}
	return r.String()
}

// urlInText finds http(s) URLs in text, such as error messages, which
// quote them in double quotes.
var urlInText = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

// RedactText redacts every http(s) URL in s, as Redact.
func RedactText(s string) string {
	return urlInText.ReplaceAllStringFunc(s, RedactString)
}

// RedactString is Redact for a URL in text; anything that doesn't parse
// is replaced whole.
func RedactString(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "(a URL)"
	}
	return Redact(u)
}
