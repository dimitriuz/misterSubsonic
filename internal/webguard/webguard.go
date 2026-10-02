// Package webguard holds the checks both of the app's small web servers (the
// development viewer and the web remote) put in front of their handlers.
package webguard

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// CheckHost refuses requests whose Host header is a name other than
// localhost or one of extra. A DNS-rebinding page is same-origin with its own
// hostname, so without this it could press keys and read frames; it can't
// make the browser send an IP address as Host, so IP literals (the LAN
// address of a wildcard bind, say) are fine.
func CheckHost(next http.Handler, extra ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !HostAllowed(r.Host, extra...) {
			http.Error(w, "unexpected Host header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HostAllowed reports whether a Host header value (with or without a port)
// is an IP literal, localhost, or one of the extra names.
func HostAllowed(hostport string, extra ...string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return true
	}
	for _, e := range extra {
		if e != "" && strings.EqualFold(host, e) {
			return true
		}
	}
	return false
}

// Hostnames is this machine's name and the same with ".local" (mDNS), the
// names a phone on the home network may use to reach it. It is empty when
// the machine has no name.
func Hostnames() []string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return nil
	}
	return []string{name, name + ".local"}
}

// SameOrigin refuses requests from other web pages, which could otherwise
// press buttons (and start playback) through the user's browser.
func SameOrigin(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" && s != "none" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || u.Host != r.Host {
			return false
		}
	}
	return true
}
