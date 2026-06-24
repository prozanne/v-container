// Package proxy detects the host proxy configuration (the Windows per-user
// proxy on Windows, or HTTP(S)_PROXY environment variables elsewhere) and
// normalizes it so it can be consumed from inside a QEMU slirp guest.
//
// Inside a slirp network the host is reachable via the slirp gateway
// (SlirpGateway). A proxy that the host advertises as 127.0.0.1/localhost/::1
// is local to the host and therefore must be rewritten to the gateway address
// before it can be used by the guest. Remote (corporate) proxies are left
// untouched.
package proxy

import (
	"net"
	"net/url"
	"strings"
)

// SlirpGateway is the address the QEMU slirp network maps to the host.
const SlirpGateway = "10.0.2.2"

// Config is the normalized proxy configuration ready for use inside the guest.
type Config struct {
	// Enabled reports whether any proxy is active.
	Enabled bool
	// HTTP is the full URL endpoint for the http scheme (may be empty).
	HTTP string
	// HTTPS is the full URL endpoint for the https scheme (may be empty).
	HTTPS string
	// NoProxy is the comma-separated no_proxy list.
	NoProxy string
	// CACertPEM is reserved for a future feature: an optional CA certificate
	// (PEM) for a TLS-intercepting proxy. No current code path populates it; it
	// is intentionally left empty and exists so callers can rely on a stable
	// struct shape.
	CACertPEM string
}

// hostIsLocal reports whether host refers to the local machine and therefore
// must be rewritten to the slirp gateway to be reachable from the guest. It
// returns true for "localhost", any loopback IP (127.0.0.0/8, ::1) and the
// IPv4/IPv6 unspecified/wildcard bind addresses (0.0.0.0, ::). A proxy that
// advertises a wildcard bind address is host-local from the guest's point of
// view, so rewriting it to the gateway makes it reachable. The comparison is
// case-insensitive for the hostname form.
func hostIsLocal(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}

// NormalizeServer rewrites a host-local proxy host (127.0.0.1 / localhost /
// ::1) to gateway so the guest can reach it via the slirp gateway; any other
// host (a remote corporate proxy) is returned unchanged. Input/output are
// "host:port" or a full URL — the form is preserved. gateway is typically
// SlirpGateway.
func NormalizeServer(server, gateway string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	if gateway == "" {
		gateway = SlirpGateway
	}

	// Full URL form (contains a scheme like "http://").
	if i := strings.Index(server, "://"); i >= 0 {
		u, err := url.Parse(server)
		if err != nil || u.Host == "" {
			// Fall back to raw host rewriting on the part after "://".
			return server[:i+3] + normalizeHostPort(server[i+3:], gateway)
		}
		host := u.Hostname()
		if !hostIsLocal(host) {
			return server
		}
		if port := u.Port(); port != "" {
			u.Host = net.JoinHostPort(gateway, port)
		} else {
			u.Host = gateway
		}
		return u.String()
	}

	// Bare host:port (or bare host) form.
	return normalizeHostPort(server, gateway)
}

// normalizeHostPort rewrites a bare "host:port" (or "host") value, replacing a
// host-local host with gateway and preserving the port. IPv6 hosts may be
// bracketed ("[::1]:3128").
func normalizeHostPort(hostport, gateway string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		// No port present; treat the whole value as a host.
		host = strings.Trim(hostport, "[]")
		if !hostIsLocal(host) {
			return hostport
		}
		return gateway
	}
	if !hostIsLocal(host) {
		return hostport
	}
	return net.JoinHostPort(gateway, port)
}

// ParseProxyServer parses a Windows "ProxyServer" registry value, which is
// either "host:port" (used for all schemes) or
// "scheme=host:port;scheme=host:port". It returns full URL endpoints for http
// and https (e.g. "http://host:port"). Missing schemes return "".
func ParseProxyServer(raw string) (httpURL, httpsURL string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	// No scheme assignment: a single endpoint used for all schemes.
	if !strings.Contains(raw, "=") {
		ep := toHTTPURL(raw)
		return ep, ep
	}

	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		scheme := strings.ToLower(strings.TrimSpace(kv[0]))
		endpoint := strings.TrimSpace(kv[1])
		if endpoint == "" {
			continue
		}
		switch scheme {
		case "http":
			httpURL = toHTTPURL(endpoint)
		case "https":
			httpsURL = toHTTPURL(endpoint)
		}
	}
	return httpURL, httpsURL
}

// toHTTPURL ensures an endpoint is expressed as a full http URL. If the
// endpoint already carries a scheme it is returned unchanged (trimmed).
func toHTTPURL(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if strings.Contains(endpoint, "://") {
		return endpoint
	}
	return "http://" + endpoint
}

// defaultNoProxyEntries are always included in the no_proxy list so that guest
// loopback and the slirp network never go through a proxy.
var defaultNoProxyEntries = []string{
	"localhost",
	"127.0.0.1",
	"10.0.2.0/24",
	"10.0.2.2",
	"10.0.2.3",
	"::1",
}

// DefaultNoProxy returns the baseline no_proxy list
// (localhost,127.0.0.1,10.0.2.0/24,10.0.2.2,10.0.2.3,::1) merged with the
// comma-separated extra. Duplicates are removed while preserving order.
func DefaultNoProxy(extra string) string {
	seen := make(map[string]struct{})
	var out []string

	add := func(entry string) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return
		}
		key := strings.ToLower(entry)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}

	for _, e := range defaultNoProxyEntries {
		add(e)
	}
	for _, e := range strings.Split(extra, ",") {
		add(e)
	}
	return strings.Join(out, ",")
}

// buildConfig assembles a normalized Config from raw http/https/noproxy
// endpoint strings. Each endpoint's host is normalized against SlirpGateway so
// host-local proxies become reachable from the guest. Enabled is true when at
// least one endpoint is set.
func buildConfig(rawHTTP, rawHTTPS, rawNoProxy string) Config {
	httpEP := NormalizeServer(strings.TrimSpace(rawHTTP), SlirpGateway)
	httpsEP := NormalizeServer(strings.TrimSpace(rawHTTPS), SlirpGateway)
	return Config{
		Enabled: httpEP != "" || httpsEP != "",
		HTTP:    httpEP,
		HTTPS:   httpsEP,
		NoProxy: DefaultNoProxy(rawNoProxy),
	}
}
