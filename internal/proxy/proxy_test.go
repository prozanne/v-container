package proxy

import (
	"strings"
	"testing"
)

func TestNormalizeServer(t *testing.T) {
	tests := []struct {
		name    string
		server  string
		gateway string
		want    string
	}{
		{"empty", "", SlirpGateway, ""},
		{"whitespace", "   ", SlirpGateway, ""},
		{"loopback ip hostport", "127.0.0.1:3128", SlirpGateway, "10.0.2.2:3128"},
		{"localhost url", "http://localhost:8080", SlirpGateway, "http://10.0.2.2:8080"},
		{"remote hostport unchanged", "proxy.corp:8080", SlirpGateway, "proxy.corp:8080"},
		{"remote url unchanged", "http://proxy.corp:8080", SlirpGateway, "http://proxy.corp:8080"},
		{"ipv6 loopback hostport", "[::1]:3128", SlirpGateway, "10.0.2.2:3128"},
		{"ipv6 loopback url", "http://[::1]:3128", SlirpGateway, "http://10.0.2.2:3128"},
		{"localhost hostport", "localhost:3128", SlirpGateway, "10.0.2.2:3128"},
		{"https scheme preserved", "https://127.0.0.1:8443", SlirpGateway, "https://10.0.2.2:8443"},
		{"url with path preserved", "http://localhost:8080/pac", SlirpGateway, "http://10.0.2.2:8080/pac"},
		{"bare loopback host no port", "127.0.0.1", SlirpGateway, "10.0.2.2"},
		{"bare remote host no port", "proxy.corp", SlirpGateway, "proxy.corp"},
		{"loopback url no port", "http://localhost", SlirpGateway, "http://10.0.2.2"},
		{"alt loopback 127.0.0.2", "127.0.0.2:3128", SlirpGateway, "10.0.2.2:3128"},
		{"uppercase LOCALHOST", "http://LOCALHOST:8080", SlirpGateway, "http://10.0.2.2:8080"},
		{"empty gateway defaults", "127.0.0.1:3128", "", "10.0.2.2:3128"},
		{"custom gateway", "127.0.0.1:3128", "192.168.0.1", "192.168.0.1:3128"},
		{"0.0.0.0 treated local", "0.0.0.0:3128", SlirpGateway, "10.0.2.2:3128"},
		{"remote ip unchanged", "203.0.113.5:8080", SlirpGateway, "203.0.113.5:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeServer(tt.server, tt.gateway)
			if got != tt.want {
				t.Errorf("NormalizeServer(%q, %q) = %q, want %q", tt.server, tt.gateway, got, tt.want)
			}
		})
	}
}

func TestParseProxyServer(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantHTTP  string
		wantHTTPS string
	}{
		{"empty", "", "", ""},
		{"whitespace", "   ", "", ""},
		{"single endpoint all schemes", "proxy:8080", "http://proxy:8080", "http://proxy:8080"},
		{"per scheme", "http=p1:80;https=p2:443", "http://p1:80", "http://p2:443"},
		{"only http scheme", "http=p1:80", "http://p1:80", ""},
		{"only https scheme", "https=p2:443", "", "http://p2:443"},
		{"extra schemes ignored", "ftp=f:21;http=p1:80", "http://p1:80", ""},
		{"trailing semicolon", "http=p1:80;", "http://p1:80", ""},
		{"spaces around parts", " http = p1:80 ; https = p2:443 ", "http://p1:80", "http://p2:443"},
		{"uppercase scheme", "HTTP=p1:80", "http://p1:80", ""},
		{"endpoint with scheme kept", "http=http://p1:80", "http://p1:80", ""},
		{"single endpoint with scheme", "http://proxy:8080", "http://proxy:8080", "http://proxy:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHTTP, gotHTTPS := ParseProxyServer(tt.raw)
			if gotHTTP != tt.wantHTTP || gotHTTPS != tt.wantHTTPS {
				t.Errorf("ParseProxyServer(%q) = (%q, %q), want (%q, %q)",
					tt.raw, gotHTTP, gotHTTPS, tt.wantHTTP, tt.wantHTTPS)
			}
		})
	}
}

func TestDefaultNoProxy(t *testing.T) {
	tests := []struct {
		name       string
		extra      string
		wantHas    []string
		wantNotDup string // entry expected to appear exactly once
	}{
		{"empty extra", "", []string{"localhost", "127.0.0.1", "10.0.2.0/24", "10.0.2.2", "10.0.2.3", "::1"}, ""},
		{"with extra", "example.com,.internal", []string{"example.com", ".internal", "10.0.2.0/24"}, ""},
		{"dedup default", "127.0.0.1,localhost", []string{"127.0.0.1"}, "127.0.0.1"},
		{"dedup case insensitive", "LOCALHOST", []string{"localhost"}, "localhost"},
		{"blanks ignored", " , ,example.com, ", []string{"example.com"}, ""},
		{"dedup extra repeats", "a.com,a.com", []string{"a.com"}, "a.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultNoProxy(tt.extra)
			if !strings.Contains(got, "10.0.2.0/24") {
				t.Errorf("DefaultNoProxy(%q) = %q, missing 10.0.2.0/24", tt.extra, got)
			}
			parts := strings.Split(got, ",")
			for _, want := range tt.wantHas {
				if !containsEntry(parts, want) {
					t.Errorf("DefaultNoProxy(%q) = %q, missing %q", tt.extra, got, want)
				}
			}
			for _, p := range parts {
				if p == "" {
					t.Errorf("DefaultNoProxy(%q) = %q, contains empty entry", tt.extra, got)
				}
			}
			if tt.wantNotDup != "" {
				n := countEntry(parts, tt.wantNotDup)
				if n != 1 {
					t.Errorf("DefaultNoProxy(%q) = %q, entry %q appears %d times, want 1",
						tt.extra, got, tt.wantNotDup, n)
				}
			}
		})
	}
}

func containsEntry(parts []string, want string) bool {
	for _, p := range parts {
		if p == want {
			return true
		}
	}
	return false
}

func countEntry(parts []string, want string) int {
	n := 0
	for _, p := range parts {
		if strings.EqualFold(p, want) {
			n++
		}
	}
	return n
}

func TestBuildConfig(t *testing.T) {
	tests := []struct {
		name        string
		rawHTTP     string
		rawHTTPS    string
		rawNoProxy  string
		wantEnabled bool
		wantHTTP    string
		wantHTTPS   string
	}{
		{"none", "", "", "", false, "", ""},
		{"http only normalized", "http://127.0.0.1:3128", "", "", true, "http://10.0.2.2:3128", ""},
		{"both remote", "http://proxy.corp:80", "http://proxy.corp:443", "", true, "http://proxy.corp:80", "http://proxy.corp:443"},
		{"trims whitespace", "  http://localhost:8080  ", "", "", true, "http://10.0.2.2:8080", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := buildConfig(tt.rawHTTP, tt.rawHTTPS, tt.rawNoProxy)
			if cfg.Enabled != tt.wantEnabled {
				t.Errorf("buildConfig Enabled = %v, want %v", cfg.Enabled, tt.wantEnabled)
			}
			if cfg.HTTP != tt.wantHTTP {
				t.Errorf("buildConfig HTTP = %q, want %q", cfg.HTTP, tt.wantHTTP)
			}
			if cfg.HTTPS != tt.wantHTTPS {
				t.Errorf("buildConfig HTTPS = %q, want %q", cfg.HTTPS, tt.wantHTTPS)
			}
			if !strings.Contains(cfg.NoProxy, "10.0.2.0/24") {
				t.Errorf("buildConfig NoProxy = %q, missing 10.0.2.0/24", cfg.NoProxy)
			}
		})
	}
}

func TestDetectEnv(t *testing.T) {
	// Clear any inherited env so the test is deterministic.
	for _, k := range []string{
		"HTTP_PROXY", "http_proxy",
		"HTTPS_PROXY", "https_proxy",
		"NO_PROXY", "no_proxy",
	} {
		t.Setenv(k, "")
	}

	t.Run("no proxy set", func(t *testing.T) {
		cfg, err := Detect()
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if cfg.Enabled {
			t.Errorf("Detect() Enabled = true, want false when no env set")
		}
		if !strings.Contains(cfg.NoProxy, "10.0.2.0/24") {
			t.Errorf("Detect() NoProxy = %q, missing default entries", cfg.NoProxy)
		}
	})

	t.Run("http proxy normalized", func(t *testing.T) {
		t.Setenv("HTTP_PROXY", "http://127.0.0.1:3128")
		cfg, err := Detect()
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if !cfg.Enabled {
			t.Fatalf("Detect() Enabled = false, want true")
		}
		if !strings.Contains(cfg.HTTP, "10.0.2.2") {
			t.Errorf("Detect() HTTP = %q, want host normalized to 10.0.2.2", cfg.HTTP)
		}
		if cfg.HTTP != "http://10.0.2.2:3128" {
			t.Errorf("Detect() HTTP = %q, want %q", cfg.HTTP, "http://10.0.2.2:3128")
		}
	})

	t.Run("lowercase env and remote https", func(t *testing.T) {
		t.Setenv("HTTP_PROXY", "")
		t.Setenv("http_proxy", "http://localhost:8080")
		t.Setenv("https_proxy", "http://proxy.corp:8443")
		t.Setenv("no_proxy", "example.com")
		cfg, err := Detect()
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if !cfg.Enabled {
			t.Fatalf("Detect() Enabled = false, want true")
		}
		if cfg.HTTP != "http://10.0.2.2:8080" {
			t.Errorf("Detect() HTTP = %q, want %q", cfg.HTTP, "http://10.0.2.2:8080")
		}
		if cfg.HTTPS != "http://proxy.corp:8443" {
			t.Errorf("Detect() HTTPS = %q, want %q (remote unchanged)", cfg.HTTPS, "http://proxy.corp:8443")
		}
		if !strings.Contains(cfg.NoProxy, "example.com") {
			t.Errorf("Detect() NoProxy = %q, missing example.com", cfg.NoProxy)
		}
	})
}
