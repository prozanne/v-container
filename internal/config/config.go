// Package config defines the global v-container configuration: user-tunable
// defaults that apply when a command doesn't specify them explicitly.
package config

import (
	"errors"

	"github.com/prozanne/v-container/internal/platform"
	"github.com/prozanne/v-container/internal/state"
)

// ProxyMode controls how the Windows host proxy is propagated into guests.
type ProxyMode string

const (
	// ProxyAuto inherits the Windows per-user proxy automatically.
	ProxyAuto ProxyMode = "auto"
	// ProxyNone disables proxy injection entirely.
	ProxyNone ProxyMode = "none"
)

// Config holds global defaults. Zero values are replaced by Defaults() on load.
type Config struct {
	// DefaultCPUs is the vCPU count for new VMs.
	DefaultCPUs int `json:"defaultCpus"`
	// DefaultMemMB is the RAM in MiB for new VMs.
	DefaultMemMB int `json:"defaultMemMb"`
	// DefaultDiskGB is the virtual disk size in GiB for new VMs.
	DefaultDiskGB int `json:"defaultDiskGb"`
	// DefaultRelease is the Ubuntu release codename used by `vc launch`.
	DefaultRelease string `json:"defaultRelease"`
	// ImageMirror is the base URL for Ubuntu cloud images.
	ImageMirror string `json:"imageMirror"`
	// ProxyMode selects automatic or disabled proxy inheritance.
	ProxyMode ProxyMode `json:"proxyMode"`
	// ProxyURL, when set, overrides the detected proxy (explicit mode).
	ProxyURL string `json:"proxyUrl,omitempty"`
	// QemuPath optionally pins the qemu-system-x86_64 binary location.
	QemuPath string `json:"qemuPath,omitempty"`
	// GuestUser is the username created inside guests.
	GuestUser string `json:"guestUser"`
}

// Defaults returns a Config populated with sensible defaults.
func Defaults() Config {
	return Config{
		DefaultCPUs:    4,
		DefaultMemMB:   4096,
		DefaultDiskGB:  20,
		DefaultRelease: "noble",
		ImageMirror:    "https://cloud-images.ubuntu.com",
		ProxyMode:      ProxyAuto,
		GuestUser:      "vmuser",
	}
}

// Load reads the config from the data dir, filling any unset fields with
// defaults. A missing file yields the full default config (not an error).
func Load(dataDir string) (Config, error) {
	cfg := Defaults()
	path := platform.ConfigPath(dataDir)
	var onDisk Config
	if err := state.ReadJSON(path, &onDisk); err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return cfg, nil
		}
		return cfg, err
	}
	return merge(cfg, onDisk), nil
}

// Save writes the config to the data dir atomically.
func Save(dataDir string, cfg Config) error {
	return state.WriteJSON(platform.ConfigPath(dataDir), cfg)
}

// merge overlays non-zero fields of override onto base.
func merge(base, override Config) Config {
	if override.DefaultCPUs > 0 {
		base.DefaultCPUs = override.DefaultCPUs
	}
	if override.DefaultMemMB > 0 {
		base.DefaultMemMB = override.DefaultMemMB
	}
	if override.DefaultDiskGB > 0 {
		base.DefaultDiskGB = override.DefaultDiskGB
	}
	if override.DefaultRelease != "" {
		base.DefaultRelease = override.DefaultRelease
	}
	if override.ImageMirror != "" {
		base.ImageMirror = override.ImageMirror
	}
	if override.ProxyMode != "" {
		base.ProxyMode = override.ProxyMode
	}
	if override.ProxyURL != "" {
		base.ProxyURL = override.ProxyURL
	}
	if override.QemuPath != "" {
		base.QemuPath = override.QemuPath
	}
	if override.GuestUser != "" {
		base.GuestUser = override.GuestUser
	}
	return base
}
