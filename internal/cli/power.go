package cli

import (
	"fmt"
	"time"

	"github.com/prozanne/v-container/internal/ui"
	"github.com/spf13/cobra"
)

func startCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "start [name]",
		Short: "Start a stopped VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.RequireTools(); err != nil {
				return err
			}
			v, err := resolveVM(a, firstArg(args))
			if err != nil {
				return err
			}
			return ui.Spin(fmt.Sprintf("starting %s", v.Name), func() error {
				_, err := a.Mgr.Start(cmd.Context(), v.Name, timeout)
				return err
			})
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "how long to wait for boot (default 3m)")
	return cmd
}

func stopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "stop [name]",
		Short: "Stop a running VM (graceful ACPI shutdown)",
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
			return ui.Spin(fmt.Sprintf("stopping %s", v.Name), func() error {
				return a.Mgr.Stop(cmd.Context(), v.Name, force)
			})
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "kill the VM instead of a graceful shutdown")
	return cmd
}

func restartCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "restart [name]",
		Short: "Restart a VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.RequireTools(); err != nil {
				return err
			}
			v, err := resolveVM(a, firstArg(args))
			if err != nil {
				return err
			}
			if err := ui.Spin(fmt.Sprintf("stopping %s", v.Name), func() error {
				return a.Mgr.Stop(cmd.Context(), v.Name, false)
			}); err != nil {
				return err
			}
			return ui.Spin(fmt.Sprintf("starting %s", v.Name), func() error {
				_, err := a.Mgr.Start(cmd.Context(), v.Name, timeout)
				return err
			})
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "how long to wait for boot (default 3m)")
	return cmd
}

func rmCmd() *cobra.Command {
	var force bool
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm [name]",
		Aliases: []string{"delete", "remove"},
		Short:   "Delete a VM and all of its data",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			v, err := resolveVM(a, firstArg(args))
			if err != nil {
				return err
			}
			if !yes {
				ui.Warn("This permanently deletes VM %q and its disk/workspace.", v.Name)
				if !confirm(fmt.Sprintf("Delete %q?", v.Name)) {
					ui.Info("Aborted.")
					return nil
				}
			}
			return ui.Spin(fmt.Sprintf("removing %s", v.Name), func() error {
				return a.Mgr.Remove(cmd.Context(), v.Name, force)
			})
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "remove even if running (kills it first)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not prompt for confirmation")
	return cmd
}
