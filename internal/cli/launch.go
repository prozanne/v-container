package cli

import (
	"strings"
	"time"

	"github.com/prozanne/v-container/internal/ui"
	"github.com/prozanne/v-container/internal/vm"
	"github.com/spf13/cobra"
)

func launchCmd() *cobra.Command {
	var (
		image   string
		release string
		cpus    int
		mem     int
		disk    int
		mounts  []string
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "launch [name]",
		Short: "Create and start a headless Ubuntu VM",
		Args:  cobra.MaximumNArgs(1),
		Example: "  vc launch\n  vc launch dev --cpus 4 --mem 8192 --disk 40\n" +
			"  vc launch build --mount C:\\\\work --mount C:\\\\data:/data",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.RequireTools(); err != nil {
				ui.Errorf("QEMU is required to launch a VM.")
				ui.Hint("vc setup   # provision QEMU, or place a 'qemu' folder next to vc.exe")
				return err
			}

			name := ""
			if len(args) == 1 {
				name = args[0]
			} else {
				name = pickFreeName(a, "ubuntu")
			}

			ms, err := parseMounts(mounts)
			if err != nil {
				return err
			}

			opts := vm.CreateOptions{
				Name:         name,
				Release:      release,
				ImagePath:    image,
				CPUs:         cpus,
				MemMB:        mem,
				DiskGB:       disk,
				Mounts:       ms,
				ReadyTimeout: timeout,
			}

			ui.Step("Launching %s ...", ui.Bold(name))
			if image == "" {
				ui.Info("  base image: ubuntu %s (cached after first download)", orStr(release, a.Cfg.DefaultRelease))
			}
			opts.Progress = ui.ProgressWriter(0, "downloading image")

			ctx := cmd.Context()
			v, err := a.Mgr.Create(ctx, opts)
			if err != nil {
				return err
			}
			ui.Success("VM %s is ready.", ui.Bold(v.Name))
			ui.Info("")
			ui.Info("  ssh:        127.0.0.1:%d (user %s)", v.SSHPort, v.User)
			ui.Info("  workspace:  %s  ⇄  ~/workspace", v.WorkspaceDir(a.DataDir))
			if v.Accel == "tcg" {
				ui.Warn("Running under software emulation (TCG) — no hardware acceleration available; expect slower performance.")
			}
			ui.Info("")
			ui.Hint("vc shell %s        # open a shell", v.Name)
			ui.Hint("vc cp file %s:/tmp/  # copy a file in", v.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "path to an explicit base image (skips download)")
	cmd.Flags().StringVar(&release, "release", "", "Ubuntu release codename (default from config, e.g. noble)")
	cmd.Flags().IntVar(&cpus, "cpus", 0, "number of vCPUs")
	cmd.Flags().IntVar(&mem, "mem", 0, "memory in MiB")
	cmd.Flags().IntVar(&disk, "disk", 0, "disk size in GiB")
	cmd.Flags().StringArrayVar(&mounts, "mount", nil, "share a host folder: HOSTPATH or HOSTPATH:GUESTPATH (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "how long to wait for first boot (default 5m)")
	return cmd
}

func parseMounts(specs []string) ([]vm.MountSpec, error) {
	var out []vm.MountSpec
	for _, s := range specs {
		if s == "" {
			continue
		}
		// Split on the LAST ':' so Windows drive letters in the host path stay
		// intact (the guest path is always absolute POSIX, starting with '/').
		host, guest := s, ""
		if i := strings.LastIndex(s, ":/"); i > 0 {
			host = s[:i]
			guest = s[i+1:]
		}
		out = append(out, vm.MountSpec{HostPath: host, GuestPath: guest})
	}
	return out, nil
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	if b != "" {
		return b
	}
	return "noble"
}
