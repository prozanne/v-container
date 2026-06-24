package cloudinit

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testPubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY comment"

const testCAPEM = `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJALTEST...notarealcert...
-----END CERTIFICATE-----`

// parseUserData strips the leading "#cloud-config" line and unmarshals the
// remainder into a map for structural assertions.
func parseUserData(t *testing.T, ud []byte) map[string]any {
	t.Helper()
	idx := bytes.IndexByte(ud, '\n')
	if idx < 0 {
		t.Fatalf("user-data has no newline after header: %q", ud)
	}
	var m map[string]any
	if err := yaml.Unmarshal(ud[idx+1:], &m); err != nil {
		t.Fatalf("unmarshal user-data: %v\n---\n%s", err, ud)
	}
	return m
}

// asSlice converts an arbitrary YAML value into []any or fails.
func asSlice(t *testing.T, v any, what string) []any {
	t.Helper()
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("%s: expected []any, got %T", what, v)
	}
	return s
}

// asMap converts an arbitrary YAML value into map[string]any or fails.
func asMap(t *testing.T, v any, what string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: expected map[string]any, got %T", what, v)
	}
	return m
}

func sliceContainsString(s []any, want string) bool {
	for _, e := range s {
		if str, ok := e.(string); ok && str == want {
			return true
		}
	}
	return false
}

func TestRenderHeaderAndBaseStructure(t *testing.T) {
	ud, md, err := Render(Options{
		User:             "vmuser",
		SSHAuthorizedKey: testPubKey,
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	// First line must be exactly "#cloud-config".
	firstLine := string(ud[:bytes.IndexByte(ud, '\n')])
	if firstLine != "#cloud-config" {
		t.Fatalf("first line = %q, want %q", firstLine, "#cloud-config")
	}

	m := parseUserData(t, ud)

	// package_update true.
	if v, ok := m["package_update"].(bool); !ok || !v {
		t.Errorf("package_update = %v, want true", m["package_update"])
	}

	// ssh_pwauth false.
	if v, ok := m["ssh_pwauth"].(bool); !ok || v {
		t.Errorf("ssh_pwauth = %v, want false", m["ssh_pwauth"])
	}

	users := asSlice(t, m["users"], "users")
	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
	u := asMap(t, users[0], "users[0]")

	if name, _ := u["name"].(string); name != "vmuser" {
		t.Errorf("users[0].name = %q, want %q", name, "vmuser")
	}
	if sudo, _ := u["sudo"].(string); sudo != "ALL=(ALL) NOPASSWD:ALL" {
		t.Errorf("users[0].sudo = %q, want NOPASSWD entry", sudo)
	}
	if !strings.Contains(u["sudo"].(string), "NOPASSWD") {
		t.Errorf("sudo entry missing NOPASSWD: %q", u["sudo"])
	}
	if shell, _ := u["shell"].(string); shell != "/bin/bash" {
		t.Errorf("users[0].shell = %q, want /bin/bash", shell)
	}
	if lp, ok := u["lock_passwd"].(bool); !ok || !lp {
		t.Errorf("users[0].lock_passwd = %v, want true", u["lock_passwd"])
	}
	groups := asSlice(t, u["groups"], "users[0].groups")
	if !sliceContainsString(groups, "sudo") {
		t.Errorf("groups = %v, want to contain sudo", groups)
	}

	keys := asSlice(t, u["ssh_authorized_keys"], "ssh_authorized_keys")
	if !sliceContainsString(keys, testPubKey) {
		t.Errorf("ssh_authorized_keys = %v, want to contain test key", keys)
	}

	// meta-data parses with non-empty instance-id and matching hostname.
	var meta map[string]any
	if err := yaml.Unmarshal(md, &meta); err != nil {
		t.Fatalf("unmarshal meta-data: %v", err)
	}
	if iid, _ := meta["instance-id"].(string); iid == "" {
		t.Errorf("meta-data instance-id is empty")
	}
	if lh, _ := meta["local-hostname"].(string); lh != "vmuser" {
		t.Errorf("local-hostname = %q, want %q (defaulted from user)", lh, "vmuser")
	}
}

func TestRenderNoSSHKey(t *testing.T) {
	ud, _, err := Render(Options{User: "vmuser"})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)
	users := asSlice(t, m["users"], "users")
	u := asMap(t, users[0], "users[0]")
	if _, present := u["ssh_authorized_keys"]; present {
		t.Errorf("ssh_authorized_keys should be absent when no key given")
	}
}

func TestRenderHostnameDefault(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		user     string
		want     string
	}{
		{name: "explicit", hostname: "myhost", user: "vmuser", want: "myhost"},
		{name: "default to user", hostname: "", user: "vmuser", want: "vmuser"},
		{name: "whitespace hostname defaults to user", hostname: "   ", user: "alice", want: "alice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ud, md, err := Render(Options{Hostname: tc.hostname, User: tc.user})
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			m := parseUserData(t, ud)
			if got, _ := m["hostname"].(string); got != tc.want {
				t.Errorf("hostname = %q, want %q", got, tc.want)
			}
			var meta map[string]any
			if err := yaml.Unmarshal(md, &meta); err != nil {
				t.Fatalf("unmarshal meta-data: %v", err)
			}
			if got, _ := meta["local-hostname"].(string); got != tc.want {
				t.Errorf("local-hostname = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderPackages(t *testing.T) {
	ud, _, err := Render(Options{
		User:     "vmuser",
		Packages: []string{"htop", "curl"},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)
	pkgs := asSlice(t, m["packages"], "packages")
	if !sliceContainsString(pkgs, "htop") || !sliceContainsString(pkgs, "curl") {
		t.Errorf("packages = %v, want htop and curl", pkgs)
	}

	// Absent when empty.
	ud2, _, _ := Render(Options{User: "vmuser"})
	m2 := parseUserData(t, ud2)
	if _, present := m2["packages"]; present {
		t.Errorf("packages key should be absent when none provided")
	}
}

func TestRenderProxy(t *testing.T) {
	ud, _, err := Render(Options{
		User: "vmuser",
		Proxy: &ProxyConfig{
			HTTP:      "http://10.0.2.2:3128",
			HTTPS:     "http://10.0.2.2:3128",
			NoProxy:   "localhost,127.0.0.1",
			CACertPEM: testCAPEM,
		},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)

	apt := asMap(t, m["apt"], "apt")
	if hp, _ := apt["http_proxy"].(string); hp != "http://10.0.2.2:3128" {
		t.Errorf("apt.http_proxy = %q, want proxy URL", hp)
	}
	if hsp, _ := apt["https_proxy"].(string); hsp != "http://10.0.2.2:3128" {
		t.Errorf("apt.https_proxy = %q, want proxy URL", hsp)
	}

	// write_files must include /etc/environment, /etc/profile.d/proxy.sh and
	// the systemd drop-in.
	wf := asSlice(t, m["write_files"], "write_files")
	var sawEnv, sawProfile, sawDropin bool
	for _, e := range wf {
		entry := asMap(t, e, "write_files entry")
		switch entry["path"] {
		case "/etc/environment":
			sawEnv = true
			content, _ := entry["content"].(string)
			if !strings.Contains(content, "http_proxy=http://10.0.2.2:3128") {
				t.Errorf("/etc/environment missing http_proxy: %q", content)
			}
			if !strings.Contains(content, "no_proxy=localhost,127.0.0.1") {
				t.Errorf("/etc/environment missing no_proxy: %q", content)
			}
		case "/etc/profile.d/proxy.sh":
			sawProfile = true
			content, _ := entry["content"].(string)
			if !strings.Contains(content, "export http_proxy=") {
				t.Errorf("proxy.sh missing export: %q", content)
			}
		case "/etc/systemd/system.conf.d/proxy.conf":
			sawDropin = true
			content, _ := entry["content"].(string)
			if !strings.Contains(content, "DefaultEnvironment=") {
				t.Errorf("systemd drop-in missing DefaultEnvironment: %q", content)
			}
		}
	}
	if !sawEnv {
		t.Error("write_files missing /etc/environment entry")
	}
	if !sawProfile {
		t.Error("write_files missing /etc/profile.d/proxy.sh entry")
	}
	if !sawDropin {
		t.Error("write_files missing systemd drop-in entry")
	}

	ca := asMap(t, m["ca_certs"], "ca_certs")
	trusted := asSlice(t, ca["trusted"], "ca_certs.trusted")
	if !sliceContainsString(trusted, testCAPEM) {
		t.Errorf("ca_certs.trusted = %v, want to contain PEM", trusted)
	}
}

func TestRenderProxyPartial(t *testing.T) {
	// Only HTTP set: https_proxy must be absent from apt; no CA.
	ud, _, err := Render(Options{
		User:  "vmuser",
		Proxy: &ProxyConfig{HTTP: "http://proxy:8080"},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)
	apt := asMap(t, m["apt"], "apt")
	if _, present := apt["https_proxy"]; present {
		t.Errorf("apt.https_proxy should be absent when HTTPS empty")
	}
	if _, present := m["ca_certs"]; present {
		t.Errorf("ca_certs should be absent when CACertPEM empty")
	}
}

// findWriteFile returns the write_files entry whose path matches, or nil.
func findWriteFile(t *testing.T, m map[string]any, p string) map[string]any {
	t.Helper()
	raw, ok := m["write_files"]
	if !ok {
		return nil
	}
	for _, e := range asSlice(t, raw, "write_files") {
		entry := asMap(t, e, "write_files entry")
		if entry["path"] == p {
			return entry
		}
	}
	return nil
}

// TestRenderProxyNoProxyOnly pins the contractual branch where only NoProxy is
// set: no apt key (nothing to configure apt with) but the write_files proxy
// entries must still be emitted carrying the no_proxy lines.
func TestRenderProxyNoProxyOnly(t *testing.T) {
	ud, _, err := Render(Options{
		User:  "vmuser",
		Proxy: &ProxyConfig{NoProxy: "localhost,127.0.0.1,.internal"},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)

	if _, present := m["apt"]; present {
		t.Errorf("apt should be absent when only NoProxy is set")
	}
	if _, present := m["ca_certs"]; present {
		t.Errorf("ca_certs should be absent when CACertPEM empty")
	}

	env := findWriteFile(t, m, "/etc/environment")
	if env == nil {
		t.Fatalf("write_files missing /etc/environment for NoProxy-only proxy")
	}
	content, _ := env["content"].(string)
	if !strings.Contains(content, "no_proxy=localhost,127.0.0.1,.internal") {
		t.Errorf("/etc/environment missing no_proxy line: %q", content)
	}
	if !strings.Contains(content, "NO_PROXY=localhost,127.0.0.1,.internal") {
		t.Errorf("/etc/environment missing NO_PROXY line: %q", content)
	}
	// http/https proxy must not appear.
	if strings.Contains(content, "http_proxy=") || strings.Contains(content, "https_proxy=") {
		t.Errorf("/etc/environment unexpectedly contains http(s)_proxy: %q", content)
	}

	if findWriteFile(t, m, "/etc/profile.d/proxy.sh") == nil {
		t.Errorf("write_files missing proxy.sh for NoProxy-only proxy")
	}
	if findWriteFile(t, m, "/etc/systemd/system.conf.d/proxy.conf") == nil {
		t.Errorf("write_files missing systemd drop-in for NoProxy-only proxy")
	}
}

// TestRenderProxyCAOnly pins the branch where only CACertPEM is set: ca_certs
// is emitted but there is nothing to put into apt or write_files.
func TestRenderProxyCAOnly(t *testing.T) {
	ud, _, err := Render(Options{
		User:  "vmuser",
		Proxy: &ProxyConfig{CACertPEM: testCAPEM},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)

	ca := asMap(t, m["ca_certs"], "ca_certs")
	trusted := asSlice(t, ca["trusted"], "ca_certs.trusted")
	if !sliceContainsString(trusted, testCAPEM) {
		t.Errorf("ca_certs.trusted = %v, want to contain PEM", trusted)
	}
	if _, present := m["apt"]; present {
		t.Errorf("apt should be absent when only CACertPEM is set")
	}
	if _, present := m["write_files"]; present {
		t.Errorf("write_files should be absent when only CACertPEM is set")
	}
}

// TestRenderProxyEnvironmentLeadingNewline guards against the /etc/environment
// append-corruption bug: when appended to an existing file with no trailing
// newline the content must start with a newline so our first assignment lands
// on its own line.
func TestRenderProxyEnvironmentLeadingNewline(t *testing.T) {
	ud, _, err := Render(Options{
		User:  "vmuser",
		Proxy: &ProxyConfig{HTTP: "http://10.0.2.2:3128"},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)
	env := findWriteFile(t, m, "/etc/environment")
	if env == nil {
		t.Fatalf("write_files missing /etc/environment")
	}
	if ap, ok := env["append"].(bool); !ok || !ap {
		t.Fatalf("/etc/environment append = %v, want true", env["append"])
	}
	content, _ := env["content"].(string)
	if !strings.HasPrefix(content, "\n") {
		t.Errorf("/etc/environment content must begin with a newline to be append-safe, got %q", content)
	}
}

// TestRenderProxySystemdQuoting verifies the systemd drop-in quotes tokens with
// systemd-compatible escaping (verbatim spaces, escaped backslash/quote) rather
// than Go's %q semantics.
func TestRenderProxySystemdQuoting(t *testing.T) {
	ud, _, err := Render(Options{
		User: "vmuser",
		// A space in NoProxy is unusual but must not produce \xNN escapes.
		Proxy: &ProxyConfig{HTTP: "http://10.0.2.2:3128", NoProxy: "a b"},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)
	dropin := findWriteFile(t, m, "/etc/systemd/system.conf.d/proxy.conf")
	if dropin == nil {
		t.Fatalf("write_files missing systemd drop-in")
	}
	content, _ := dropin["content"].(string)
	if !strings.Contains(content, `"http_proxy=http://10.0.2.2:3128"`) {
		t.Errorf("drop-in missing quoted http_proxy token: %q", content)
	}
	// The space must be preserved verbatim inside double quotes, not escaped.
	if !strings.Contains(content, `"no_proxy=a b"`) {
		t.Errorf("drop-in did not preserve space verbatim: %q", content)
	}
	if strings.Contains(content, `\x`) || strings.Contains(content, `\u`) {
		t.Errorf("drop-in contains Go-style escapes: %q", content)
	}
}

// TestSystemdQuote unit-tests the escaping helper directly.
func TestSystemdQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"http_proxy=http://h:1", `"http_proxy=http://h:1"`},
		{"a b", `"a b"`},
		{`back\slash`, `"back\\slash"`},
		{`quote"here`, `"quote\"here"`},
		{"", `""`},
	}
	for _, tc := range tests {
		if got := systemdQuote(tc.in); got != tc.want {
			t.Errorf("systemdQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderProxyNilAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proxy *ProxyConfig
	}{
		{name: "nil", proxy: nil},
		{name: "empty struct", proxy: &ProxyConfig{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ud, _, err := Render(Options{User: "vmuser", Proxy: tc.proxy})
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			m := parseUserData(t, ud)
			if _, present := m["apt"]; present {
				t.Errorf("apt should be absent for %s proxy", tc.name)
			}
			if _, present := m["write_files"]; present {
				t.Errorf("write_files should be absent for %s proxy", tc.name)
			}
		})
	}
}

func TestRenderGrowpart(t *testing.T) {
	ud, _, err := Render(Options{User: "vmuser", EnableGrowpart: true})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)
	gp := asMap(t, m["growpart"], "growpart")
	if mode, _ := gp["mode"].(string); mode != "auto" {
		t.Errorf("growpart.mode = %q, want auto", mode)
	}
	devices := asSlice(t, gp["devices"], "growpart.devices")
	if !sliceContainsString(devices, "/") {
		t.Errorf("growpart.devices = %v, want to contain /", devices)
	}
	if v, ok := m["resize_rootfs"].(bool); !ok || !v {
		t.Errorf("resize_rootfs = %v, want true", m["resize_rootfs"])
	}

	// Absent when disabled.
	ud2, _, _ := Render(Options{User: "vmuser"})
	m2 := parseUserData(t, ud2)
	if _, present := m2["growpart"]; present {
		t.Errorf("growpart should be absent when EnableGrowpart false")
	}
}

func TestRenderExtraWriteFilesAndRunCmds(t *testing.T) {
	ud, _, err := Render(Options{
		User: "vmuser",
		ExtraWriteFiles: []WriteFile{
			{Path: "/etc/foo", Content: "bar", Permissions: "0600", Append: true},
			{Path: "/etc/baz", Content: "qux"},
		},
		ExtraRunCmds: []string{"echo hi", "systemctl restart foo"},
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	m := parseUserData(t, ud)

	wf := asSlice(t, m["write_files"], "write_files")
	var sawFoo, sawBaz bool
	for _, e := range wf {
		entry := asMap(t, e, "write_files entry")
		switch entry["path"] {
		case "/etc/foo":
			sawFoo = true
			if entry["permissions"] != "0600" {
				t.Errorf("/etc/foo permissions = %v, want 0600", entry["permissions"])
			}
			if ap, ok := entry["append"].(bool); !ok || !ap {
				t.Errorf("/etc/foo append = %v, want true", entry["append"])
			}
		case "/etc/baz":
			sawBaz = true
			if _, present := entry["append"]; present {
				t.Errorf("/etc/baz should not carry append key when false")
			}
			if _, present := entry["permissions"]; present {
				t.Errorf("/etc/baz should not carry permissions when empty")
			}
		}
	}
	if !sawFoo || !sawBaz {
		t.Errorf("missing extra write_files entries: foo=%v baz=%v", sawFoo, sawBaz)
	}

	cmds := asSlice(t, m["runcmd"], "runcmd")
	if !sliceContainsString(cmds, "echo hi") || !sliceContainsString(cmds, "systemctl restart foo") {
		t.Errorf("runcmd = %v, want both commands", cmds)
	}
}

func TestRenderErrorEmptyUser(t *testing.T) {
	tests := []struct {
		name string
		user string
	}{
		{name: "empty", user: ""},
		{name: "whitespace", user: "   "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ud, md, err := Render(Options{User: tc.user})
			if err == nil {
				t.Fatalf("expected error for user %q, got nil", tc.user)
			}
			if ud != nil || md != nil {
				t.Errorf("expected nil outputs on error, got ud=%v md=%v", ud, md)
			}
			if !strings.Contains(err.Error(), "User") {
				t.Errorf("error %q should mention User", err)
			}
		})
	}
}

func TestRenderDeterministic(t *testing.T) {
	opts := Options{
		User:             "vmuser",
		Hostname:         "host1",
		SSHAuthorizedKey: testPubKey,
		Packages:         []string{"htop", "curl"},
		Proxy: &ProxyConfig{
			HTTP:      "http://10.0.2.2:3128",
			HTTPS:     "http://10.0.2.2:3128",
			NoProxy:   "localhost",
			CACertPEM: testCAPEM,
		},
		EnableGrowpart:  true,
		ExtraWriteFiles: []WriteFile{{Path: "/etc/foo", Content: "bar"}},
		ExtraRunCmds:    []string{"echo hi"},
	}
	ud1, md1, err := Render(opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	for i := 0; i < 5; i++ {
		ud2, md2, err := Render(opts)
		if err != nil {
			t.Fatalf("Render error on iteration %d: %v", i, err)
		}
		if !bytes.Equal(ud1, ud2) {
			t.Fatalf("user-data not deterministic on iteration %d", i)
		}
		if !bytes.Equal(md1, md2) {
			t.Fatalf("meta-data not deterministic on iteration %d", i)
		}
	}
}

func TestInstanceIDStableAndNonEmpty(t *testing.T) {
	a := instanceID("host1")
	b := instanceID("host1")
	c := instanceID("host2")
	if a == "" {
		t.Fatal("instanceID empty")
	}
	if a != b {
		t.Errorf("instanceID not stable: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("instanceID collision for different hostnames: %q", a)
	}
}

func TestUserDataValidYAML(t *testing.T) {
	// Ensure full document (including header) round-trips after stripping
	// the comment line, with a representative configuration.
	ud, _, err := Render(Options{
		User:             "vmuser",
		SSHAuthorizedKey: testPubKey,
		Proxy:            &ProxyConfig{HTTP: "http://p:1", CACertPEM: testCAPEM},
		EnableGrowpart:   true,
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	// The whole thing (with the leading comment) must be valid YAML too,
	// because "#cloud-config" is a YAML comment.
	var whole map[string]any
	if err := yaml.Unmarshal(ud, &whole); err != nil {
		t.Fatalf("full user-data is not valid YAML: %v", err)
	}
}
