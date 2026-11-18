package fetch

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Cookie is a cookie sent with downloads, for trackers that need a login,
// in the shape of qBittorrent's cookie jar.
type Cookie struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
	Value  string `json:"value"`
	// ExpirationDate is in seconds since the epoch; 0 means never.
	ExpirationDate int64 `json:"expirationDate"`
}

// jar sends the configured cookies. Cookies set by responses are not kept.
type jar struct {
	cookies atomic.Pointer[[]Cookie]
	now     func() time.Time
}

func (j *jar) SetCookies(*url.URL, []*http.Cookie) {}

func (j *jar) Cookies(u *url.URL) []*http.Cookie {
	p := j.cookies.Load()
	if p == nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	now := j.now().Unix()
	var out []*http.Cookie
	for _, c := range *p {
		domain := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		if domain == "" || (host != domain && !strings.HasSuffix(host, "."+domain)) {
			continue
		}
		if c.Path != "" && !pathMatch(path, c.Path) {
			continue
		}
		if c.ExpirationDate > 0 && c.ExpirationDate <= now {
			continue
		}
		// A cookie to send: the attributes gosec checks only apply to ones
		// a server sets.
		out = append(out, &http.Cookie{Name: c.Name, Value: c.Value}) //nolint:gosec // see above
	}
	return out
}

// pathMatch is RFC 6265's path match.
func pathMatch(path, cookiePath string) bool {
	if !strings.HasPrefix(path, cookiePath) {
		return false
	}
	return len(path) == len(cookiePath) || strings.HasSuffix(cookiePath, "/") || path[len(cookiePath)] == '/'
}

// SetCookies replaces the cookies sent with downloads.
func (f *Fetcher) SetCookies(cs []Cookie) {
	cs = append([]Cookie{}, cs...)
	f.jar.cookies.Store(&cs)
}

// Cookies returns the cookies sent with downloads.
func (f *Fetcher) Cookies() []Cookie {
	if p := f.jar.cookies.Load(); p != nil {
		return append([]Cookie{}, *p...)
	}
	return []Cookie{}
}
