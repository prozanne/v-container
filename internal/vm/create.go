package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/prozanne/v-container/internal/cloudinit"
	"github.com/prozanne/v-container/internal/config"
	"github.com/prozanne/v-container/internal/image"
	"github.com/prozanne/v-container/internal/netx"
	"github.com/prozanne/v-container/internal/proxy"
	"github.com/prozanne/v-container/internal/seed"
	"github.com/prozanne/v-container/internal/sshx"
)

// MountSpec is a host↔guest shared folder request at create time.
type MountSpec struct {
	HostPath  string
	GuestPath string
}

// CreateOptions configures a new VM.
type CreateOptions struct {
	Name      string
	Release   string // Ubuntu codename; defaults to config
	ImagePath string // optional explicit base image (skips download)
	CPUs      int
	MemMB     int
	DiskGB    int
	Mounts    []MountSpec
	// Progress receives image-download progress, if non-nil.
	Progress io.Writer
	// ReadyTimeout bounds the wait for first-boot SSH; 0 => 5 minutes.
	ReadyTimeout time.Duration
}

// Create builds, starts and waits for a new VM, returning its record. On a
// first-boot timeout the VM is left in place (running) so the user can inspect
// the serial console; the error explains where to look.
func (m *Manager) Create(ctx context.Context, opts CreateOptions) (*VM, error) {
	if err := ValidateName(opts.Name); err != nil {
		return nil, err
	}
	if m.Exists(opts.Name) {
		return nil, fmt.Errorf("vm %q already exists", opts.Name)
	}

	v := &VM{
		Name:      opts.Name,
		CreatedAt: time.Now().UTC(),
		User:      orDefault(m.Cfg.GuestUser, "vmuser"),
		CPUs:      firstPositive(opts.CPUs, m.Cfg.DefaultCPUs, 4),
		MemMB:     firstPositive(opts.MemMB, m.Cfg.DefaultMemMB, 4096),
		DiskGB:    firstPositive(opts.DiskGB, m.Cfg.DefaultDiskGB, 20),
	}
	release := orDefault(opts.Release, m.Cfg.DefaultRelease, "noble")
	if opts.ImagePath != "" {
		v.Source.Image = opts.ImagePath
	} else {
		v.Source.Release = release
	}

	unlock, err := m.lock(opts.Name)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if err := os.MkdirAll(v.Dir(m.DataDir), 0o755); err != nil {
		return nil, fmt.Errorf("create vm dir: %w", err)
	}

	// Default shared workspace + any requested mounts.
	if err := os.MkdirAll(v.WorkspaceDir(m.DataDir), 0o755); err != nil {
		return nil, fmt.Errorf("create workspace dir: %w", err)
	}
	v.Mounts = append(v.Mounts, Mount{
		HostPath:  v.WorkspaceDir(m.DataDir),
		GuestPath: fmt.Sprintf("/home/%s/workspace", v.User),
		Mode:      ShareSync,
	})
	for _, ms := range opts.Mounts {
		gp := ms.GuestPath
		if gp == "" {
			gp = fmt.Sprintf("/home/%s/%s", v.User, filepath.Base(ms.HostPath))
		}
		v.Mounts = append(v.Mounts, Mount{HostPath: ms.HostPath, GuestPath: gp, Mode: ShareSync})
	}

	// SSH key.
	pubLine, err := sshx.WriteKeyPair(v.KeyPath(m.DataDir), "v-container:"+v.Name)
	if err != nil {
		return nil, err
	}

	// Base image → per-VM overlay.
	base, err := m.resolveBaseImage(ctx, v, release, opts.Progress)
	if err != nil {
		return nil, err
	}
	if err := image.CreateOverlay(ctx, m.Tools.Img, base, v.DiskPath(m.DataDir)); err != nil {
		return nil, err
	}
	if err := image.Resize(ctx, m.Tools.Img, v.DiskPath(m.DataDir), v.DiskGB); err != nil {
		return nil, err
	}

	// cloud-init seed.
	if err := m.writeSeed(v, string(pubLine)); err != nil {
		return nil, err
	}

	// Networking: SSH + QMP ports, MAC.
	sshPort, err := netx.FreeLoopbackPort()
	if err != nil {
		return nil, fmt.Errorf("allocate ssh port: %w", err)
	}
	qmpPort, err := netx.FreeLoopbackPort(sshPort)
	if err != nil {
		return nil, fmt.Errorf("allocate qmp port: %w", err)
	}
	v.SSHPort = sshPort
	v.QMPPort = qmpPort
	if v.MAC, err = generateMAC(); err != nil {
		return nil, err
	}

	if err := m.Save(v); err != nil {
		return nil, err
	}

	// Boot it.
	if err := m.startLocked(ctx, v); err != nil {
		return nil, err
	}

	// Wait for first-boot SSH.
	timeout := opts.ReadyTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if _, err := m.waitReady(ctx, v, timeout); err != nil {
		return v, fmt.Errorf("vm %q started but did not become reachable: %w\n  inspect the console: %s",
			v.Name, err, v.ConsoleLog(m.DataDir))
	}
	return v, nil
}

// resolveBaseImage returns the absolute path to the base image, downloading a
// cloud image when no explicit image was supplied.
func (m *Manager) resolveBaseImage(ctx context.Context, v *VM, release string, progress io.Writer) (string, error) {
	if v.Source.Image != "" {
		abs, err := filepath.Abs(v.Source.Image)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("base image %s: %w", abs, err)
		}
		return abs, nil
	}
	cacheDir := filepath.Join(m.DataDir, "images")
	return image.EnsureBaseImage(ctx, m.HTTP, m.Cfg.ImageMirror, release, cacheDir, progress)
}

// writeSeed renders cloud-init and writes the NoCloud seed image.
func (m *Manager) writeSeed(v *VM, pubKey string) error {
	ud, md, err := cloudinit.Render(cloudinit.Options{
		Hostname:         v.Name,
		User:             v.User,
		SSHAuthorizedKey: trimNewline(pubKey),
		Proxy:            m.buildProxy(),
		EnableGrowpart:   true,
	})
	if err != nil {
		return fmt.Errorf("render cloud-init: %w", err)
	}
	return seed.WriteISO(v.SeedPath(m.DataDir), []seed.File{
		{Name: "user-data", Data: ud},
		{Name: "meta-data", Data: md},
	})
}

// buildProxy resolves the proxy config to inject into the guest, honoring the
// global config's proxy mode.
func (m *Manager) buildProxy() *cloudinit.ProxyConfig {
	switch m.Cfg.ProxyMode {
	case config.ProxyNone:
		return nil
	default: // auto
		c, err := proxy.Detect()
		if err != nil || !c.Enabled {
			return nil
		}
		return &cloudinit.ProxyConfig{
			HTTP:      c.HTTP,
			HTTPS:     c.HTTPS,
			NoProxy:   proxy.DefaultNoProxy(c.NoProxy),
			CACertPEM: c.CACertPEM,
		}
	}
}

func orDefault(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
