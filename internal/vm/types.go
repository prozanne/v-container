// Package vm defines the persistent data model for a virtual machine and the
// Manager that drives its lifecycle. This file holds only the pure data types
// and path helpers; the orchestration logic lives in manager.go. Keeping the
// types import-light (only the dependency-free platform package) lets other
// packages reference the model without pulling in process/network machinery.
package vm

import (
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/prozanne/v-container/internal/platform"
)

// Status is the coarse runtime state of a VM.
type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusError    Status = "error"
	StatusUnknown  Status = "unknown"
)

// ShareMode selects how a shared folder is kept available in the guest.
type ShareMode string

const (
	// ShareSync mirrors a host folder to/from the guest over SFTP. Needs no
	// guest packages beyond sshd — the robust default.
	ShareSync ShareMode = "sync"
	// ShareLive mounts the host folder live in the guest via sshfs.
	ShareLive ShareMode = "live"
)

// Mount records a configured shared folder.
type Mount struct {
	HostPath  string    `json:"hostPath"`
	GuestPath string    `json:"guestPath"`
	Mode      ShareMode `json:"mode"`
}

// Forward records an extra host→guest TCP/UDP port forward (the SSH forward is
// stored separately as VM.SSHPort).
type Forward struct {
	HostPort  int    `json:"hostPort"`
	GuestPort int    `json:"guestPort"`
	Proto     string `json:"proto"`
}

// Source describes where a VM's disk came from.
type Source struct {
	Release string `json:"release,omitempty"` // Ubuntu codename, e.g. "noble"
	Image   string `json:"image,omitempty"`   // explicit base image path or URL
	ISO     string `json:"iso,omitempty"`     // ISO path for the install path
}

// VM is the persisted record for a single virtual machine (vm.json). Absolute
// paths are intentionally NOT stored — they are derived from the data dir and
// name so a moved/renamed data dir keeps working.
type VM struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	Source    Source    `json:"source"`

	CPUs   int `json:"cpus"`
	MemMB  int `json:"memMb"`
	DiskGB int `json:"diskGb"`

	User string `json:"user"`
	MAC  string `json:"mac"`

	SSHPort  int       `json:"sshPort"`
	QMPPort  int       `json:"qmpPort"`
	Forwards []Forward `json:"forwards,omitempty"`
	Mounts   []Mount   `json:"mounts,omitempty"`

	Accel string `json:"accel,omitempty"` // recorded active accelerator
	PID   int    `json:"pid,omitempty"`   // last known qemu process id
}

// Path helpers. Each takes the resolved data dir and returns an absolute path
// inside the VM's directory.

func (v *VM) Dir(dataDir string) string { return platform.VMDir(dataDir, v.Name) }
func (v *VM) RecordPath(dataDir string) string {
	return filepath.Join(v.Dir(dataDir), "vm.json")
}
func (v *VM) DiskPath(dataDir string) string   { return filepath.Join(v.Dir(dataDir), "disk.qcow2") }
func (v *VM) SeedPath(dataDir string) string   { return filepath.Join(v.Dir(dataDir), "seed.img") }
func (v *VM) KeyPath(dataDir string) string    { return filepath.Join(v.Dir(dataDir), "id_ed25519") }
func (v *VM) PubKeyPath(dataDir string) string { return v.KeyPath(dataDir) + ".pub" }
func (v *VM) ConsoleLog(dataDir string) string { return filepath.Join(v.Dir(dataDir), "console.log") }
func (v *VM) PIDPath(dataDir string) string    { return filepath.Join(v.Dir(dataDir), "qemu.pid") }
func (v *VM) LockPath(dataDir string) string   { return filepath.Join(v.Dir(dataDir), "vm.lock") }
func (v *VM) WorkspaceDir(dataDir string) string {
	return filepath.Join(v.Dir(dataDir), "workspace")
}

// nameRE constrains VM names: lowercase alphanumerics and hyphens, starting
// with an alphanumeric. The name is reused as a directory and a guest hostname,
// so it must be filesystem- and DNS-safe.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$|^[a-z0-9]$`)

// ValidateName checks that a VM name is safe to use as a directory and hostname.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len(name) > 32 {
		return fmt.Errorf("name %q is too long (max 32 characters)", name)
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid name %q: use lowercase letters, digits and hyphens (must start and end with a letter or digit)", name)
	}
	return nil
}
