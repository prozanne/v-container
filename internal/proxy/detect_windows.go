//go:build windows

package proxy

import (
	"fmt"

	ieproxy "github.com/mattn/go-ieproxy"
)

// Detect returns the host proxy config on Windows. It reads the per-user
// Internet Settings (HKCU) via go-ieproxy, mapping the static proxy
// configuration to a Config. Host-local endpoints (127.0.0.1 / localhost /
// ::1) are normalized to SlirpGateway so the guest can reach them through the
// slirp gateway.
//
// It does not error when no proxy is set: it returns Config{Enabled:false}, nil.
func Detect() (Config, error) {
	conf := ieproxy.ReloadConf()

	// Only the static configuration provides concrete endpoints we can hand to
	// the guest. PAC/auto-config (conf.Automatic) cannot be reduced to a fixed
	// endpoint here, so it is treated as "no static proxy".
	static := conf.Static
	if !static.Active {
		return Config{
			Enabled: false,
			NoProxy: DefaultNoProxy(static.NoProxy),
		}, nil
	}

	httpEndpoint := protocolEndpoint(static.Protocols, "http")
	httpsEndpoint := protocolEndpoint(static.Protocols, "https")

	cfg := buildConfig(httpEndpoint, httpsEndpoint, static.NoProxy)
	if !cfg.Enabled {
		// Active flag was set but no usable endpoint was found.
		return cfg, fmt.Errorf("proxy: active static proxy reported no usable endpoint")
	}
	return cfg, nil
}

// protocolEndpoint resolves the proxy endpoint for the given scheme from the
// ieproxy Protocols map, expressed as a full http URL. ieproxy stores values
// as bare "host:port" keyed by scheme ("http", "https") with "" as the
// fallback used for all schemes. Missing schemes fall back to the "" entry.
func protocolEndpoint(protocols map[string]string, scheme string) string {
	if protocols == nil {
		return ""
	}
	if v, ok := protocols[scheme]; ok && v != "" {
		return toHTTPURL(v)
	}
	if v, ok := protocols[""]; ok && v != "" {
		return toHTTPURL(v)
	}
	return ""
}
