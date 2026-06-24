package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the vc version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("vc %s (%s %s/%s)\n", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
