package cli

import (
	"fmt"
	"runtime"

	"github.com/prozanne/v-container/internal/config"
	"github.com/prozanne/v-container/internal/qemu"
	"github.com/prozanne/v-container/internal/ui"
	"github.com/spf13/cobra"
)

func setupCmd() *cobra.Command {
	var qemuDir string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Locate or configure the QEMU binaries",
		Long: "Checks whether QEMU can be found and, if not, explains how to provide it.\n" +
			"Use --qemu-dir to point vc at an existing QEMU installation (saved to config).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}

			if qemuDir != "" {
				tools, err := qemu.Locate(a.DataDir, qemuDir, "")
				if err != nil {
					return fmt.Errorf("no qemu-system-x86_64 under %q: %w", qemuDir, err)
				}
				a.Cfg.QemuPath = qemuDir
				if err := config.Save(a.DataDir, a.Cfg); err != nil {
					return err
				}
				ui.Success("Saved QEMU location: %s", tools.System)
				return nil
			}

			if err := a.RequireTools(); err == nil {
				ui.Success("QEMU is ready: %s", a.Tools.System)
				ui.Info("  qemu-img: %s", a.Tools.Img)
				return nil
			}

			ui.Warn("QEMU was not found.")
			ui.Info("")
			ui.Info("v-container needs the QEMU binaries (qemu-system-x86_64 and qemu-img).")
			ui.Info("Pick whichever is easiest in your environment:")
			ui.Info("")
			ui.Info("  1. Bundle: put a 'qemu' folder containing the binaries next to vc%s.", exeSuffix())
			ui.Info("  2. Point vc at an existing install:")
			ui.Hint("vc setup --qemu-dir \"C:\\\\Program Files\\\\qemu\"")
			ui.Info("  3. Add the QEMU folder to your PATH.")
			if runtime.GOOS == "windows" {
				ui.Info("")
				ui.Info("Windows builds: https://qemu.weilnetz.de/w64/  (qemu.org → Download → Windows)")
			}
			return fmt.Errorf("qemu not configured")
		},
	}
	cmd.Flags().StringVar(&qemuDir, "qemu-dir", "", "directory containing qemu-system-x86_64 (saved to config)")
	return cmd
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
