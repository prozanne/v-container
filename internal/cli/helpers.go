package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/prozanne/v-container/internal/app"
	"github.com/prozanne/v-container/internal/ui"
	"github.com/prozanne/v-container/internal/vm"
	"github.com/spf13/cobra"
)

// confirm prompts the user for a yes/no answer, defaulting to no.
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// resolveVM returns the named VM, or the sole VM when name is empty.
func resolveVM(a *app.App, name string) (*vm.VM, error) {
	if name == "" {
		return a.Mgr.DefaultVM()
	}
	return a.Mgr.Get(name)
}

// runStatus is the no-argument overview: a table of VMs plus next-step hints.
func runStatus(cmd *cobra.Command) error {
	a, err := newApp()
	if err != nil {
		return err
	}
	vms, err := a.Mgr.List()
	if err != nil {
		return err
	}
	if len(vms) == 0 {
		ui.Info("%s", ui.Bold("v-container"))
		ui.Info("No VMs yet. Get started:")
		ui.Hint("vc launch            # create and start an Ubuntu VM")
		ui.Hint("vc doctor            # check your environment")
		if err := a.RequireTools(); err != nil {
			ui.Warn("QEMU not found yet: %v", err)
			ui.Hint("vc setup             # download QEMU, or bundle it next to vc.exe")
		}
		return nil
	}
	printVMTable(a, vms)
	ui.Info("")
	ui.Hint("vc shell [name]      # open a shell    vc cp SRC DST   # copy files")
	return nil
}

// printVMTable renders the standard VM listing.
func printVMTable(a *app.App, vms []*vm.VM) {
	rows := make([][]string, 0, len(vms))
	for _, v := range vms {
		status := string(a.Mgr.Status(v))
		ssh := "-"
		if v.SSHPort > 0 {
			ssh = fmt.Sprintf("127.0.0.1:%d", v.SSHPort)
		}
		rows = append(rows, []string{
			v.Name,
			status,
			fmt.Sprintf("%d", v.CPUs),
			fmt.Sprintf("%dMB", v.MemMB),
			fmt.Sprintf("%dGB", v.DiskGB),
			ssh,
			source(v),
		})
	}
	ui.Table([]string{"NAME", "STATUS", "CPUS", "MEM", "DISK", "SSH", "SOURCE"}, rows)
}

func source(v *vm.VM) string {
	switch {
	case v.Source.Release != "":
		return "ubuntu:" + v.Source.Release
	case v.Source.Image != "":
		return "image"
	case v.Source.ISO != "":
		return "iso"
	default:
		return "-"
	}
}

// isDirLocal reports whether a local path is an existing directory.
func isDirLocal(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// workspaceMount returns the VM's default shared workspace mount (the first
// recorded mount), or nil if none is configured.
func workspaceMount(v *vm.VM) *vm.Mount {
	for i := range v.Mounts {
		if v.Mounts[i].Mode == vm.ShareSync {
			return &v.Mounts[i]
		}
	}
	if len(v.Mounts) > 0 {
		return &v.Mounts[0]
	}
	return nil
}

// pickFreeName returns base, or base-2, base-3, ... — the first name not in use.
func pickFreeName(a *app.App, base string) string {
	if !a.Mgr.Exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !a.Mgr.Exists(candidate) {
			return candidate
		}
	}
}

// copyEndpoint describes one side of a `vc cp` argument.
type copyEndpoint struct {
	Remote bool
	VMName string // when Remote
	Path   string
}

// parseCopyArg splits "name:/path" / ":/path" (default VM) / a local path. A
// side is treated as remote only when the part before the first ':' names an
// existing VM (or is empty for the default VM), so Windows paths like C:\dir
// are correctly treated as local.
func parseCopyArg(a *app.App, arg string) (copyEndpoint, error) {
	idx := strings.Index(arg, ":")
	if idx >= 0 {
		name := arg[:idx]
		rest := arg[idx+1:]
		if name == "" {
			v, err := a.Mgr.DefaultVM()
			if err != nil {
				return copyEndpoint{}, err
			}
			return copyEndpoint{Remote: true, VMName: v.Name, Path: rest}, nil
		}
		if a.Mgr.Exists(name) {
			return copyEndpoint{Remote: true, VMName: name, Path: rest}, nil
		}
	}
	return copyEndpoint{Remote: false, Path: arg}, nil
}
