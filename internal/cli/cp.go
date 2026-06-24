package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/prozanne/v-container/internal/share"
	"github.com/prozanne/v-container/internal/ui"
	"github.com/spf13/cobra"
)

func cpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cp SRC DST",
		Short: "Copy files between host and guest (use name:/path for the guest)",
		Args:  cobra.ExactArgs(2),
		Example: "  vc cp ./report.txt dev:/home/vmuser/\n" +
			"  vc cp dev:/var/log/syslog ./syslog\n  vc cp ./dir dev:/tmp/dir",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			src, err := parseCopyArg(a, args[0])
			if err != nil {
				return err
			}
			dst, err := parseCopyArg(a, args[1])
			if err != nil {
				return err
			}
			if src.Remote == dst.Remote {
				return fmt.Errorf("exactly one of SRC/DST must be a guest path (name:/path)")
			}

			// Identify the VM (whichever side is remote) and connect.
			vmName := src.VMName
			if dst.Remote {
				vmName = dst.VMName
			}
			v, err := a.Mgr.Get(vmName)
			if err != nil {
				return err
			}
			client, err := a.Mgr.Connect(cmd.Context(), v)
			if err != nil {
				return err
			}
			defer client.Close()

			if dst.Remote {
				// upload host -> guest
				remote := dst.Path
				if remote == "" || endsWithSlash(remote) {
					remote = joinSlash(remote, filepath.Base(src.Path))
				}
				return ui.Spin(fmt.Sprintf("copying %s → %s:%s", src.Path, vmName, remote), func() error {
					return share.Upload(cmd.Context(), client, src.Path, remote)
				})
			}
			// download guest -> host
			local := dst.Path
			if local == "" {
				local = "."
			}
			if isDirLocal(local) {
				local = filepath.Join(local, baseSlash(src.Path))
			}
			return ui.Spin(fmt.Sprintf("copying %s:%s → %s", vmName, src.Path, local), func() error {
				return share.Download(cmd.Context(), client, src.Path, local)
			})
		},
	}
}

func syncCmd() *cobra.Command {
	var watch bool
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "sync [name]",
		Short: "Bidirectionally mirror the shared workspace (host ⇄ guest)",
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
			ws := workspaceMount(v)
			if ws == nil {
				return fmt.Errorf("vm %q has no shared workspace", v.Name)
			}
			client, err := a.Mgr.Connect(cmd.Context(), v)
			if err != nil {
				return err
			}
			defer client.Close()
			manifest := filepath.Join(v.Dir(a.DataDir), "workspace.sync.json")

			runOnce := func() error {
				res, err := share.Sync(cmd.Context(), client, ws.HostPath, ws.GuestPath, manifest, 0)
				if err != nil {
					return err
				}
				ui.Info("synced: ↑%d ↓%d  deleted ↑%d ↓%d  conflicts %d",
					res.Uploaded, res.Downloaded, res.DeletedRemote, res.DeletedLocal, res.Conflicts)
				for _, p := range res.ConflictPaths {
					ui.Warn("conflict (kept both): %s", p)
				}
				return nil
			}

			if !watch {
				return runOnce()
			}
			if interval <= 0 {
				interval = 3 * time.Second
			}
			ui.Step("watching %s (every %s); press Ctrl-C to stop", ws.HostPath, interval)
			if err := runOnce(); err != nil {
				return err
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-cmd.Context().Done():
					return nil
				case <-ticker.C:
					if err := runOnce(); err != nil {
						ui.Warn("sync: %v", err)
					}
				}
			}
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "keep syncing on an interval")
	cmd.Flags().DurationVar(&interval, "interval", 3*time.Second, "watch interval")
	return cmd
}

func endsWithSlash(p string) bool {
	return len(p) > 0 && (p[len(p)-1] == '/')
}

func joinSlash(dir, name string) string {
	if dir == "" {
		return name
	}
	if endsWithSlash(dir) {
		return dir + name
	}
	return dir + "/" + name
}

func baseSlash(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
