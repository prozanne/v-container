// Package cloudinit renders cloud-init NoCloud documents (user-data and
// meta-data) for a headless Ubuntu guest.
//
// The rendered user-data is a #cloud-config document built from a
// map[string]any and marshalled with gopkg.in/yaml.v3. yaml.v3 marshals map
// keys in sorted (lexicographic) order, so for a given set of Options the
// output is deterministic and stable across runs and across processes.
//
// Instance identity caveat: meta-data instance-id is derived deterministically
// from the hostname (sha256). NoCloud uses instance-id to decide whether
// per-instance modules (user creation, ssh key injection, growpart) re-run, so
// two VMs created with the SAME hostname receive the SAME instance-id and the
// second VM's seed may be treated as already-applied. Callers that create
// multiple guests MUST use a unique hostname per VM to guarantee distinct
// seeds. (The public API is fixed and cannot carry an explicit InstanceID.)
package cloudinit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProxyConfig describes the HTTP(S) proxy to inject into the guest.
type ProxyConfig struct {
	// HTTP is the full proxy URL, e.g. "http://10.0.2.2:3128".
	HTTP string
	// HTTPS is the full HTTPS proxy URL.
	HTTPS string
	// NoProxy is a comma-separated list of hosts that bypass the proxy.
	NoProxy string
	// CACertPEM is an optional corporate root CA in PEM form (may be empty).
	CACertPEM string
}

// isEmpty reports whether the proxy carries no usable configuration.
func (p *ProxyConfig) isEmpty() bool {
	if p == nil {
		return true
	}
	return p.HTTP == "" && p.HTTPS == "" && p.NoProxy == "" && p.CACertPEM == ""
}

// quotedString marshals as a YAML double-quoted scalar regardless of content.
// This is used for values that begin with a newline (e.g. append-safe file
// content): the default literal block style would emit an explicit indent
// indicator ("|4") that does not reliably round-trip, whereas a double-quoted
// scalar always does and is accepted by cloud-init's PyYAML parser.
type quotedString string

func (q quotedString) MarshalYAML() (any, error) {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Style: yaml.DoubleQuotedStyle,
		Value: string(q),
	}, nil
}

// WriteFile describes a file written to the guest on first boot.
type WriteFile struct {
	// Path is the absolute destination path inside the guest.
	Path string
	// Content is the file body.
	Content string
	// Permissions is an octal mode string, e.g. "0644" (optional).
	Permissions string
	// Append, when true, appends to an existing file instead of overwriting.
	Append bool
}

// Options configures the rendered cloud-init documents.
type Options struct {
	// Hostname is the guest hostname. If empty it defaults to User, or
	// "ubuntu" when User is also empty (User is required, so this is a guard).
	// The effective hostname is validated against RFC-1123 label rules.
	Hostname string
	// User is the guest username to create, e.g. "vmuser". Required.
	User string
	// SSHAuthorizedKey is a single public key line for authorized_keys. It
	// must not contain embedded newlines.
	SSHAuthorizedKey string
	// Packages are apt packages to install on first boot.
	Packages []string
	// Proxy, when non-nil and non-empty, injects proxy settings.
	Proxy *ProxyConfig
	// EnableGrowpart grows the root partition and filesystem to fill the disk.
	EnableGrowpart bool
	// ExtraWriteFiles are additional files to write on first boot.
	ExtraWriteFiles []WriteFile
	// ExtraRunCmds are additional shell commands to run on first boot.
	ExtraRunCmds []string
}

// hostnameLabelRE matches a single RFC-1123 hostname label: lowercase
// alphanumerics and hyphens, not starting or ending with a hyphen.
var hostnameLabelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// octalPermsRE matches a 3- or 4-digit octal mode string, e.g. "644" or "0644".
var octalPermsRE = regexp.MustCompile(`^[0-7]{3,4}$`)

// hasControlChars reports whether s contains any ASCII control character
// (including newlines, carriage returns and tabs).
func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// validateHostname checks h against RFC-1123 rules: total length 1..253, each
// dot-separated label 1..63 chars matching [a-z0-9-] without leading/trailing
// hyphen. Returns an error describing the first violation.
func validateHostname(h string) error {
	if h == "" {
		return fmt.Errorf("hostname is empty")
	}
	if len(h) > 253 {
		return fmt.Errorf("hostname %q exceeds 253 characters", h)
	}
	if hasControlChars(h) {
		return fmt.Errorf("hostname contains control characters")
	}
	labels := strings.Split(h, ".")
	for _, label := range labels {
		if label == "" {
			return fmt.Errorf("hostname %q has an empty label", h)
		}
		if len(label) > 63 {
			return fmt.Errorf("hostname label %q exceeds 63 characters", label)
		}
		if !hostnameLabelRE.MatchString(label) {
			return fmt.Errorf("hostname label %q is not a valid RFC-1123 label "+
				"(use lowercase a-z, 0-9 and '-', not starting or ending with '-')", label)
		}
	}
	return nil
}

// Render returns the user-data (a #cloud-config document) and meta-data bytes.
//
// user-data is a YAML map prefixed with a literal "#cloud-config\n" line.
// meta-data is a small YAML document with instance-id and local-hostname keys.
func Render(o Options) (userData []byte, metaData []byte, err error) {
	if strings.TrimSpace(o.User) == "" {
		return nil, nil, fmt.Errorf("cloudinit: render: User must be non-empty")
	}

	if strings.ContainsAny(o.SSHAuthorizedKey, "\r\n") {
		return nil, nil, fmt.Errorf("cloudinit: render: SSHAuthorizedKey must be a single line (no embedded newlines)")
	}

	hostname := strings.TrimSpace(o.Hostname)
	if hostname == "" {
		if strings.TrimSpace(o.User) != "" {
			hostname = strings.TrimSpace(o.User)
		} else {
			hostname = "ubuntu"
		}
	}
	if err := validateHostname(hostname); err != nil {
		return nil, nil, fmt.Errorf("cloudinit: render: invalid hostname: %w", err)
	}

	if err := validateProxy(o.Proxy); err != nil {
		return nil, nil, fmt.Errorf("cloudinit: render: %w", err)
	}

	if err := validateWriteFiles(o.ExtraWriteFiles); err != nil {
		return nil, nil, fmt.Errorf("cloudinit: render: %w", err)
	}

	cfg := buildUserData(o, hostname)

	body, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("cloudinit: marshal user-data: %w", err)
	}
	userData = append([]byte("#cloud-config\n"), body...)

	metaData, err = buildMetaData(hostname)
	if err != nil {
		return nil, nil, fmt.Errorf("cloudinit: build meta-data: %w", err)
	}

	return userData, metaData, nil
}

// validateProxy rejects proxy fields that would corrupt the generated config.
// Proxy URLs and no_proxy lists must be single-line and free of control
// characters so they cannot break /etc/environment, the profile script or the
// systemd drop-in. A nil/empty proxy is valid.
func validateProxy(p *ProxyConfig) error {
	if p.isEmpty() {
		return nil
	}
	for _, f := range []struct {
		name, val string
	}{
		{"Proxy.HTTP", p.HTTP},
		{"Proxy.HTTPS", p.HTTPS},
		{"Proxy.NoProxy", p.NoProxy},
	} {
		if f.val != "" && hasControlChars(f.val) {
			return fmt.Errorf("%s contains control characters", f.name)
		}
	}
	return nil
}

// validateWriteFiles checks each caller-supplied write_files entry.
func validateWriteFiles(files []WriteFile) error {
	for i, wf := range files {
		if strings.TrimSpace(wf.Path) == "" {
			return fmt.Errorf("ExtraWriteFiles[%d]: Path must be non-empty", i)
		}
		if !path.IsAbs(wf.Path) {
			return fmt.Errorf("ExtraWriteFiles[%d]: Path %q must be absolute", i, wf.Path)
		}
		if hasControlChars(wf.Path) {
			return fmt.Errorf("ExtraWriteFiles[%d]: Path contains control characters", i)
		}
		if wf.Permissions != "" && !octalPermsRE.MatchString(wf.Permissions) {
			return fmt.Errorf("ExtraWriteFiles[%d]: Permissions %q must be a 3-4 digit octal string", i, wf.Permissions)
		}
	}
	return nil
}

// buildUserData assembles the #cloud-config map for the given options.
func buildUserData(o Options, hostname string) map[string]any {
	cfg := map[string]any{
		"hostname":       hostname,
		"package_update": true,
		"ssh_pwauth":     false,
	}

	// Primary user.
	user := map[string]any{
		"name":        o.User,
		"groups":      []string{"sudo"},
		"shell":       "/bin/bash",
		"sudo":        "ALL=(ALL) NOPASSWD:ALL",
		"lock_passwd": true,
	}
	if strings.TrimSpace(o.SSHAuthorizedKey) != "" {
		user["ssh_authorized_keys"] = []string{o.SSHAuthorizedKey}
	}
	cfg["users"] = []any{user}

	// Packages.
	if len(o.Packages) > 0 {
		pkgs := make([]string, len(o.Packages))
		copy(pkgs, o.Packages)
		cfg["packages"] = pkgs
	}

	// write_files accumulates proxy files plus any extras.
	var writeFiles []any

	// Proxy.
	if !o.Proxy.isEmpty() {
		apt := map[string]any{}
		if o.Proxy.HTTP != "" {
			apt["http_proxy"] = o.Proxy.HTTP
		}
		if o.Proxy.HTTPS != "" {
			apt["https_proxy"] = o.Proxy.HTTPS
		}
		if len(apt) > 0 {
			cfg["apt"] = apt
		}

		if env := proxyEnvironment(o.Proxy); env != "" {
			// Lead with a newline so appending onto an existing /etc/environment
			// that lacks a trailing newline can't fuse our first assignment onto
			// the file's last line. A leading newline would normally make yaml.v3
			// emit a "|4" block-indent indicator that doesn't round-trip, so we
			// force a double-quoted scalar (quotedString), which always does.
			writeFiles = append(writeFiles, map[string]any{
				"path":        "/etc/environment",
				"content":     quotedString("\n" + env),
				"append":      true,
				"permissions": "0644",
			})
		}

		if profile := proxyProfileScript(o.Proxy); profile != "" {
			writeFiles = append(writeFiles, map[string]any{
				"path":        "/etc/profile.d/proxy.sh",
				"content":     profile,
				"permissions": "0644",
			})
		}

		if dropin := proxySystemdDropIn(o.Proxy); dropin != "" {
			writeFiles = append(writeFiles, map[string]any{
				"path":        "/etc/systemd/system.conf.d/proxy.conf",
				"content":     dropin,
				"permissions": "0644",
			})
		}

		if strings.TrimSpace(o.Proxy.CACertPEM) != "" {
			cfg["ca_certs"] = map[string]any{
				"trusted": []string{o.Proxy.CACertPEM},
			}
		}
	}

	// Extra write_files appended after proxy files (stable order).
	for _, wf := range o.ExtraWriteFiles {
		entry := map[string]any{
			"path":    wf.Path,
			"content": wf.Content,
		}
		if wf.Permissions != "" {
			entry["permissions"] = wf.Permissions
		}
		if wf.Append {
			entry["append"] = true
		}
		writeFiles = append(writeFiles, entry)
	}

	if len(writeFiles) > 0 {
		cfg["write_files"] = writeFiles
	}

	// Growpart.
	if o.EnableGrowpart {
		cfg["growpart"] = map[string]any{
			"mode":    "auto",
			"devices": []string{"/"},
		}
		cfg["resize_rootfs"] = true
	}

	// runcmd.
	if len(o.ExtraRunCmds) > 0 {
		cmds := make([]string, len(o.ExtraRunCmds))
		copy(cmds, o.ExtraRunCmds)
		cfg["runcmd"] = cmds
	}

	return cfg
}

// proxyEnvironment renders the lines appended to /etc/environment.
func proxyEnvironment(p *ProxyConfig) string {
	var b strings.Builder
	writeProxyVars(&b, p, false)
	return b.String()
}

// proxyProfileScript renders /etc/profile.d/proxy.sh with export statements.
func proxyProfileScript(p *ProxyConfig) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	writeProxyVars(&b, p, true)
	if b.Len() == len("#!/bin/sh\n") {
		return ""
	}
	return b.String()
}

// writeProxyVars writes http_proxy/https_proxy/no_proxy in both lower and
// upper case. When export is true each line is prefixed with "export ".
func writeProxyVars(b *strings.Builder, p *ProxyConfig, export bool) {
	prefix := ""
	if export {
		prefix = "export "
	}
	emit := func(name, val string) {
		if val == "" {
			return
		}
		fmt.Fprintf(b, "%s%s=%s\n", prefix, name, val)
		fmt.Fprintf(b, "%s%s=%s\n", prefix, strings.ToUpper(name), val)
	}
	emit("http_proxy", p.HTTP)
	emit("https_proxy", p.HTTPS)
	emit("no_proxy", p.NoProxy)
}

// systemdQuote renders a single DefaultEnvironment token using systemd's
// double-quote escaping rules. Within a double-quoted word systemd treats a
// backslash as an escape character, so the only bytes that need escaping are
// the backslash and the double quote itself; everything else (including
// spaces) is taken verbatim. This is more faithful than Go's %q, which would
// emit \xNN / \uNNNN sequences that systemd parses differently.
func systemdQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// proxySystemdDropIn renders the systemd DefaultEnvironment drop-in.
func proxySystemdDropIn(p *ProxyConfig) string {
	var pairs []string
	add := func(name, val string) {
		if val == "" {
			return
		}
		pairs = append(pairs, systemdQuote(name+"="+val))
		pairs = append(pairs, systemdQuote(strings.ToUpper(name)+"="+val))
	}
	add("http_proxy", p.HTTP)
	add("https_proxy", p.HTTPS)
	add("no_proxy", p.NoProxy)
	if len(pairs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Manager]\n")
	fmt.Fprintf(&b, "DefaultEnvironment=%s\n", strings.Join(pairs, " "))
	return b.String()
}

// buildMetaData renders the NoCloud meta-data document.
func buildMetaData(hostname string) ([]byte, error) {
	meta := map[string]any{
		"instance-id":    instanceID(hostname),
		"local-hostname": hostname,
	}
	out, err := yaml.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal meta-data: %w", err)
	}
	return out, nil
}

// instanceID derives a stable, non-empty instance id from the hostname so a
// given hostname always yields the same id (deterministic output). See the
// package doc for the implication of reusing a hostname across VMs.
func instanceID(hostname string) string {
	sum := sha256.Sum256([]byte(hostname))
	return "iid-" + hex.EncodeToString(sum[:8])
}
