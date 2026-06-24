package cli

import (
	"fmt"
	"strconv"

	"github.com/prozanne/v-container/internal/config"
	"github.com/prozanne/v-container/internal/ui"
	"github.com/spf13/cobra"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change global defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			printConfig(a.Cfg)
			ui.Info("")
			ui.Hint("vc config set <key> <value>   keys: cpus mem disk release mirror proxy guestuser qemupath")
			return nil
		},
	}
	cmd.AddCommand(configSetCmd())
	return cmd
}

func printConfig(c config.Config) {
	ui.Info("%s", ui.Bold("v-container config"))
	ui.Info("  cpus:       %d", c.DefaultCPUs)
	ui.Info("  mem:        %d MiB", c.DefaultMemMB)
	ui.Info("  disk:       %d GiB", c.DefaultDiskGB)
	ui.Info("  release:    %s", c.DefaultRelease)
	ui.Info("  mirror:     %s", c.ImageMirror)
	ui.Info("  proxy:      %s", c.ProxyMode)
	ui.Info("  guestuser:  %s", c.GuestUser)
	if c.QemuPath != "" {
		ui.Info("  qemupath:   %s", c.QemuPath)
	}
}

func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			key, val := args[0], args[1]
			if err := applyConfig(&a.Cfg, key, val); err != nil {
				return err
			}
			if err := config.Save(a.DataDir, a.Cfg); err != nil {
				return err
			}
			ui.Success("Set %s = %s", key, val)
			return nil
		},
	}
}

func applyConfig(c *config.Config, key, val string) error {
	atoiPos := func() (int, error) {
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("%s must be a positive integer", key)
		}
		return n, nil
	}
	switch key {
	case "cpus":
		n, err := atoiPos()
		if err != nil {
			return err
		}
		c.DefaultCPUs = n
	case "mem":
		n, err := atoiPos()
		if err != nil {
			return err
		}
		c.DefaultMemMB = n
	case "disk":
		n, err := atoiPos()
		if err != nil {
			return err
		}
		c.DefaultDiskGB = n
	case "release":
		c.DefaultRelease = val
	case "mirror":
		c.ImageMirror = val
	case "proxy":
		switch config.ProxyMode(val) {
		case config.ProxyAuto, config.ProxyNone:
			c.ProxyMode = config.ProxyMode(val)
		default:
			return fmt.Errorf("proxy must be 'auto' or 'none'")
		}
	case "guestuser":
		c.GuestUser = val
	case "qemupath":
		c.QemuPath = val
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}
