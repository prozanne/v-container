package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell [name]",
		Short: "Open an interactive shell in a VM",
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
			client, err := a.Mgr.Connect(cmd.Context(), v)
			if err != nil {
				return err
			}
			defer client.Close()
			return client.Shell(cmd.Context())
		},
	}
}

func execCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "exec <name> -- <command> [args...]",
		Short:   "Run a command in a VM and stream its output",
		Args:    cobra.MinimumNArgs(1),
		Example: "  vc exec dev -- uname -a\n  vc exec dev -- sh -c 'cd /tmp && ls -l'",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			// The first arg is the VM name; the rest is the command. If the
			// first arg looks like a command (no such VM) and a single VM
			// exists, treat all args as the command for the default VM.
			name := args[0]
			cmdArgs := args[1:]
			if !a.Mgr.Exists(name) {
				if dv, derr := a.Mgr.DefaultVM(); derr == nil {
					name = dv.Name
					cmdArgs = args
				}
			}
			if len(cmdArgs) == 0 {
				return fmt.Errorf("no command given; usage: vc exec <name> -- <command>")
			}
			v, err := a.Mgr.Get(name)
			if err != nil {
				return err
			}
			client, err := a.Mgr.Connect(cmd.Context(), v)
			if err != nil {
				return err
			}
			defer client.Close()
			exit, err := client.Stream(cmd.Context(), strings.Join(cmdArgs, " "), os.Stdin, os.Stdout, os.Stderr)
			if err != nil {
				return err
			}
			if exit != 0 {
				os.Exit(exit)
			}
			return nil
		},
	}
	return cmd
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
