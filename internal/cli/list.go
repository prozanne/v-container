package cli

import (
	"github.com/prozanne/v-container/internal/ui"
	"github.com/spf13/cobra"
)

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list", "ps"},
		Short:   "List VMs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			vms, err := a.Mgr.List()
			if err != nil {
				return err
			}
			if len(vms) == 0 {
				ui.Info("No VMs. Create one with 'vc launch'.")
				return nil
			}
			printVMTable(a, vms)
			return nil
		},
	}
}
