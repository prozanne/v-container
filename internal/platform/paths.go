// Package platform resolves OS-specific locations and answers basic
// host-environment questions. It is intentionally dependency-free so every
// other package can rely on it without creating import cycles.
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// EnvHome is the environment variable that overrides the data directory.
const EnvHome = "VC_HOME"

// IsWindows reports whether we are running on Windows.
func IsWindows() bool { return runtime.GOOS == "windows" }

// DataDir returns the root directory where v-container keeps all of its
// state: config, cached images, provisioned QEMU and per-VM data.
//
// Resolution order:
//  1. $VC_HOME (explicit override)
//  2. Windows: %LOCALAPPDATA%\v-container
//  3. Linux:   $XDG_DATA_HOME/v-container or ~/.local/share/v-container
//  4. macOS:   ~/Library/Application Support/v-container (dev convenience)
//
// The directory is not created; callers that need it on disk should use
// EnsureDataDir.
func DataDir() (string, error) {
	if v := os.Getenv(EnvHome); v != "" {
		return v, nil
	}
	switch runtime.GOOS {
	case "windows":
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "v-container"), nil
		}
		// Fall back to the user config dir if LOCALAPPDATA is somehow unset.
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine LOCALAPPDATA: %w", err)
		}
		return filepath.Join(base, "v-container"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "v-container"), nil
	default: // linux and other unixes
		if base := os.Getenv("XDG_DATA_HOME"); base != "" {
			return filepath.Join(base, "v-container"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "v-container"), nil
	}
}

// EnsureDataDir resolves DataDir and creates it (and standard subdirectories)
// if missing, returning the resolved path.
func EnsureDataDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	for _, sub := range []string{"", "vms", "images", "qemu"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", fmt.Errorf("create data dir %q: %w", filepath.Join(dir, sub), err)
		}
	}
	return dir, nil
}

// VMsDir returns the directory that holds per-VM state.
func VMsDir(dataDir string) string { return filepath.Join(dataDir, "vms") }

// VMDir returns the directory for a single VM.
func VMDir(dataDir, name string) string { return filepath.Join(dataDir, "vms", name) }

// ImagesDir returns the cache directory for base images and ISOs.
func ImagesDir(dataDir string) string { return filepath.Join(dataDir, "images") }

// QemuDir returns the directory where a provisioned QEMU may live.
func QemuDir(dataDir string) string { return filepath.Join(dataDir, "qemu") }

// ConfigPath returns the path to the global config file.
func ConfigPath(dataDir string) string { return filepath.Join(dataDir, "config.json") }

// ExeDir returns the directory containing the running executable. Used to find
// a QEMU tree shipped alongside vc.exe. Best-effort; returns "" on failure.
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}
