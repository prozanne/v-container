//go:build !windows

package proxy

import "os"

// Detect returns the host proxy config. On non-Windows platforms it reads the
// HTTP_PROXY/http_proxy, HTTPS_PROXY/https_proxy and NO_PROXY/no_proxy
// environment variables. Endpoints are normalized so host-local proxies are
// reachable from the slirp guest via SlirpGateway. It never errors when no
// proxy is configured; it returns Config{Enabled:false} instead.
func Detect() (Config, error) {
	httpProxy := firstEnv("HTTP_PROXY", "http_proxy")
	httpsProxy := firstEnv("HTTPS_PROXY", "https_proxy")
	noProxy := firstEnv("NO_PROXY", "no_proxy")

	return buildConfig(httpProxy, httpsProxy, noProxy), nil
}

// firstEnv returns the value of the first set (non-empty) environment variable
// from keys, or "" if none are set.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
	}
	return ""
}
