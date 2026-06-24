// Package cli implements the vc command-line interface.
package cli

import (
	"context"
	"os"
	"os/signal"

	"github.com/prozanne/v-container/internal/app"
	"github.com/prozanne/v-container/internal/ui"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Execute runs the root command and returns a process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		ui.Errorf("%v", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "vc",
		Short: "v-container — run headless Ubuntu Linux VMs on Windows, no WSL/Hyper-V/Docker",
		Long: "v-container (vc) runs headless Ubuntu Linux virtual machines on Windows using QEMU.\n" +
			"It needs no WSL, Hyper-V or Docker and no admin rights. Launch a VM, get a shell,\n" +
			"run commands, and move files between Windows and Linux with a shared workspace.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd)
		},
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.AddCommand(
		launchCmd(),
		listCmd(),
		shellCmd(),
		execCmd(),
		cpCmd(),
		syncCmd(),
		startCmd(),
		stopCmd(),
		restartCmd(),
		rmCmd(),
		infoCmd(),
		doctorCmd(),
		setupCmd(),
		configCmd(),
		versionCmd(),
	)
	return root
}

// newApp builds the application context for a command.
func newApp() (*app.App, error) {
	return app.New()
}
