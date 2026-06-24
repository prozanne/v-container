package vm

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/prozanne/v-container/internal/config"
	"github.com/prozanne/v-container/internal/platform"
	"github.com/prozanne/v-container/internal/qemu"
	"github.com/prozanne/v-container/internal/state"
)

// Manager owns VM persistence and lifecycle. It is the single place that ties
// together qemu, images, cloud-init, networking and ssh.
type Manager struct {
	DataDir string
	Cfg     config.Config
	Tools   qemu.Tools
	HTTP    *http.Client // proxy-aware client for image downloads
}

// NewManager constructs a Manager.
func NewManager(dataDir string, cfg config.Config, tools qemu.Tools, httpClient *http.Client) *Manager {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Manager{DataDir: dataDir, Cfg: cfg, Tools: tools, HTTP: httpClient}
}

// Save writes a VM record to disk atomically.
func (m *Manager) Save(v *VM) error {
	if err := os.MkdirAll(v.Dir(m.DataDir), 0o755); err != nil {
		return fmt.Errorf("create vm dir: %w", err)
	}
	return state.WriteJSON(v.RecordPath(m.DataDir), v)
}

// Get loads a VM record by name.
func (m *Manager) Get(name string) (*VM, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	var v VM
	if err := state.ReadJSON(filepath.Join(platform.VMDir(m.DataDir, name), "vm.json"), &v); err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("no such vm %q", name)
		}
		return nil, err
	}
	return &v, nil
}

// Exists reports whether a VM with the given name is recorded.
func (m *Manager) Exists(name string) bool {
	return state.Exists(filepath.Join(platform.VMDir(m.DataDir, name), "vm.json"))
}

// List returns all VM records sorted by name.
func (m *Manager) List() ([]*VM, error) {
	dir := platform.VMsDir(m.DataDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var vms []*VM
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := m.Get(e.Name())
		if err != nil {
			continue // skip unreadable/partial entries
		}
		vms = append(vms, v)
	}
	sort.Slice(vms, func(i, j int) bool { return vms[i].Name < vms[j].Name })
	return vms, nil
}

// Status derives the current run state from the recorded pid.
func (m *Manager) Status(v *VM) Status {
	if v.PID > 0 && qemu.IsRunning(v.PID) {
		return StatusRunning
	}
	return StatusStopped
}

// DefaultVM returns the sole VM when exactly one exists, so commands can omit
// the name. It errors clearly when zero or many exist.
func (m *Manager) DefaultVM() (*VM, error) {
	vms, err := m.List()
	if err != nil {
		return nil, err
	}
	switch len(vms) {
	case 0:
		return nil, fmt.Errorf("no VMs exist yet — create one with 'vc launch'")
	case 1:
		return vms[0], nil
	default:
		return nil, fmt.Errorf("multiple VMs exist — specify a name")
	}
}

// lock acquires the per-VM lock for mutating operations.
func (m *Manager) lock(name string) (func(), error) {
	return state.Lock(filepath.Join(platform.VMDir(m.DataDir, name), "vm.lock"))
}

// generateMAC returns a locally-administered MAC with QEMU's 52:54:00 prefix.
func generateMAC() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate mac: %w", err)
	}
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2]), nil
}

// isNotFound reports whether err is a state "file not found".
func isNotFound(err error) bool {
	return errors.Is(err, state.ErrNotFound)
}
