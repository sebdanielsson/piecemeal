// Package fetch downloads .torrent files from URLs given to the add
// endpoints. It is the one place piecemeal fetches arbitrary URLs, so it
// refuses private, loopback and link-local addresses unless allowed, and
// limits size, redirects and time.
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
	"strings"
	"syscall"
	"time"
)

// Fetcher downloads torrent files.
type Fetcher struct {
	client  *http.Client
	maxSize int64
}

// ErrForbidden is returned for URLs that resolve to a blocked address.
var ErrForbidden = errors.New("address not allowed")

// New returns a fetcher that may also reach the allowed networks, which
// are otherwise blocked as private.
func New(allowed []netip.Prefix) *Fetcher {
	dialer := &net.Dialer{
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
				return fmt.Errorf("%w: %s", ErrForbidden, ip)
			}
			return nil
		},
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		// Proxies would make the address check meaningless.
		Proxy: nil,
	}
	f := &Fetcher{maxSize: 16 << 20}
	f.client = &http.Client{
		Transport: transport,
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
	return f
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
	// Carrier-grade NAT (RFC 6598), where Tailscale addresses live.
	if netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return false
	}
	return true
}

// Result is either a magnet URI (when the URL redirects to one) or the
// body of a torrent file.
type Result struct {
	Magnet string
	Body   []byte
}

// Get downloads rawURL.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (Result, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Result{}, fmt.Errorf("bad URL: %w", err)
	}
	if u.Scheme == "magnet" {
		return Result{Magnet: rawURL}, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Result{}, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "piecemeal")
	resp, err := f.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Indexers often redirect a download link to a magnet URI.
	if loc := resp.Header.Get("Location"); resp.StatusCode/100 == 3 && strings.HasPrefix(loc, "magnet:") {
		return Result{Magnet: loc}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("%s: %s", u.Redacted(), resp.Status)
	}
	if resp.ContentLength > f.maxSize {
		return Result{}, fmt.Errorf("%s: larger than %d bytes", u.Redacted(), f.maxSize)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, f.maxSize+1))
	if err != nil {
		return Result{}, err
	}
	if int64(len(b)) > f.maxSize {
		return Result{}, fmt.Errorf("%s: larger than %d bytes", u.Redacted(), f.maxSize)
	}
	return Result{Body: b}, nil
}
