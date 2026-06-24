package cli

import (
	"context"
	"runtime"

	"github.com/prozanne/v-container/internal/proxy"
	"github.com/prozanne/v-container/internal/qemu"
	"github.com/prozanne/v-container/internal/ui"
	"github.com/spf13/cobra"
)

// reportProxy prints the detected host proxy that would be injected into guests.
func reportProxy() {
	c, err := proxy.Detect()
	if err != nil {
		ui.Warn("proxy:       detection failed (%v)", err)
		return
	}
	if !c.Enabled {
		ui.Info("  proxy:       none detected (guests go direct)")
		return
	}
	ui.Success("proxy:       http=%s https=%s (inherited into guests)", orDash(c.HTTP), orDash(c.HTTPS))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info [name]",
		Short: "Show detailed information about a VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			v, err := resolveVM(a, firstArg(args))
			if err != nil {
				return err
			}
			ui.Info("%s", ui.Bold(v.Name))
			ui.Info("  status:     %s", a.Mgr.Status(v))
			ui.Info("  source:     %s", source(v))
			ui.Info("  resources:  %d vCPU, %d MiB RAM, %d GiB disk", v.CPUs, v.MemMB, v.DiskGB)
			ui.Info("  user:       %s", v.User)
			ui.Info("  ssh:        ssh -p %d %s@127.0.0.1", v.SSHPort, v.User)
			if v.Accel != "" {
				ui.Info("  accel:      %s", v.Accel)
			}
			if len(v.Mounts) > 0 {
				ui.Info("  mounts:")
				for _, mnt := range v.Mounts {
					ui.Info("    %s  ⇄  %s  (%s)", mnt.HostPath, mnt.GuestPath, mnt.Mode)
				}
			}
			ui.Info("  data dir:   %s", v.Dir(a.DataDir))
			ui.Info("  console:    %s", v.ConsoleLog(a.DataDir))
			return nil
		},
	}
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the environment and report what works",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			ui.Info("%s", ui.Bold("v-container doctor"))
			ui.Info("  host:        %s/%s", runtime.GOOS, runtime.GOARCH)
			ui.Info("  data dir:    %s", a.DataDir)

			if err := a.RequireTools(); err != nil {
				ui.Errorf("qemu:        not found (%v)", err)
				ui.Hint("Place a 'qemu' folder next to vc.exe, run 'vc setup', or add qemu to PATH.")
			} else {
				ui.Success("qemu:        %s", a.Tools.System)
				ui.Success("qemu-img:    %s", a.Tools.Img)
				accels, perr := qemu.ProbeAccelerators(context.Background(), a.Tools.System)
				if perr != nil {
					ui.Warn("accel:       could not probe (%v)", perr)
				} else {
					chosen := qemu.SelectAccel(accels)
					if qemu.IsSoftware(chosen) {
						ui.Warn("accel:       %v  → using TCG (software emulation; slower, but works)", accels)
					} else {
						ui.Success("accel:       %v  → using %s (hardware accelerated)", accels, chosen)
					}
				}
			}

			reportProxy()

			vms, _ := a.Mgr.List()
			ui.Info("  vms:         %d", len(vms))
			return nil
		},
	}
}
