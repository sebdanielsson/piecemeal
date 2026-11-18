package main

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// hostCheck answers requests for host names other than the allowed ones
// with 421 Misdirected Request, so a site that rebinds its own name to
// piecemeal's address can't reach it from a visitor's browser. Loopback
// names and the health checks are always allowed: probes call those by
// address. With no allowed hosts every name is.
func hostCheck(allowed []string, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	set := map[string]bool{"localhost": true}
	for _, h := range allowed {
		set[h] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
		ip, err := netip.ParseAddr(host)
		if set[host] || (err == nil && ip.IsLoopback()) || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "piecemeal doesn't answer to this host name; see PIECEMEAL_ALLOWED_HOSTS\n", http.StatusMisdirectedRequest)
	})
}
