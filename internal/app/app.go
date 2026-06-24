// Package app is the composition root: it resolves configuration, the data
// directory, a proxy-aware HTTP client and the QEMU tools, and wires up the VM
// manager that the CLI commands operate on.
package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mattn/go-ieproxy"
	"github.com/prozanne/v-container/internal/config"
	"github.com/prozanne/v-container/internal/platform"
	"github.com/prozanne/v-container/internal/qemu"
	"github.com/prozanne/v-container/internal/vm"
)

// App holds the shared runtime dependencies for CLI commands.
type App struct {
	DataDir  string
	Cfg      config.Config
	HTTP     *http.Client
	Tools    qemu.Tools
	toolsErr error
	Mgr      *vm.Manager
}

// New builds the App. Failure to locate QEMU is NOT fatal here — commands that
// need it call RequireTools — so read-only commands (ls, info, version) and
// `vc doctor` keep working on a machine where QEMU isn't set up yet.
func New() (*App, error) {
	dataDir, err := platform.EnsureDataDir()
	if err != nil {
		return nil, fmt.Errorf("prepare data dir: %w", err)
	}
	cfg, err := config.Load(dataDir)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{Proxy: ieproxy.GetProxyFunc()},
		Timeout:   0, // image downloads can be large; no overall timeout
	}

	a := &App{DataDir: dataDir, Cfg: cfg, HTTP: httpClient}
	a.Tools, a.toolsErr = qemu.Locate(dataDir, cfg.QemuPath, platform.ExeDir())
	a.Mgr = vm.NewManager(dataDir, cfg, a.Tools, httpClient)
	return a, nil
}

// RequireTools returns an error if the QEMU binaries could not be located.
func (a *App) RequireTools() error {
	if a.toolsErr != nil {
		return a.toolsErr
	}
	if a.Tools.System == "" {
		return fmt.Errorf("qemu-system-x86_64 not found")
	}
	return nil
}

// ToolsError exposes the (possibly nil) tools-location error for diagnostics.
func (a *App) ToolsError() error { return a.toolsErr }

// HTTPWithTimeout returns a shallow copy of the HTTP client with a request
// timeout, for small fetches like checksums.
func (a *App) HTTPWithTimeout(d time.Duration) *http.Client {
	c := *a.HTTP
	c.Timeout = d
	return &c
}
