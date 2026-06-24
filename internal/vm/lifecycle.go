package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prozanne/v-container/internal/netx"
	"github.com/prozanne/v-container/internal/qemu"
	"github.com/prozanne/v-container/internal/sshx"
)

// EnvQemuExtraArgs lets advanced users pass additional raw arguments to QEMU
// (space-separated). It is also how local development points QEMU at a custom
// firmware directory, e.g. VC_QEMU_EXTRA_ARGS="-L /path/to/firmware".
const EnvQemuExtraArgs = "VC_QEMU_EXTRA_ARGS"

// Start boots an existing, stopped VM and waits for it to become reachable.
func (m *Manager) Start(ctx context.Context, name string, readyTimeout time.Duration) (*VM, error) {
	v, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	unlock, err := m.lock(name)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if m.Status(v) == StatusRunning {
		return v, fmt.Errorf("vm %q is already running", name)
	}
	if err := m.startLocked(ctx, v); err != nil {
		return nil, err
	}
	if readyTimeout <= 0 {
		readyTimeout = 3 * time.Minute
	}
	if _, err := m.waitReady(ctx, v, readyTimeout); err != nil {
		return v, fmt.Errorf("vm %q started but did not become reachable: %w\n  inspect the console: %s",
			name, err, v.ConsoleLog(m.DataDir))
	}
	return v, nil
}

// startLocked builds the QEMU command line and launches the VM. The caller must
// hold the per-VM lock.
func (m *Manager) startLocked(ctx context.Context, v *VM) error {
	if m.Status(v) == StatusRunning {
		return fmt.Errorf("vm %q is already running", v.Name)
	}

	accels, err := qemu.ProbeAccelerators(ctx, m.Tools.System)
	if err != nil {
		// Non-fatal: fall back to a safe default.
		accels = []string{"tcg"}
	}
	v.Accel = qemu.SelectAccel(accels)

	forwards := []netx.Forward{{HostPort: v.SSHPort, GuestPort: 22, Proto: "tcp"}}
	for _, f := range v.Forwards {
		forwards = append(forwards, netx.Forward{HostPort: f.HostPort, GuestPort: f.GuestPort, Proto: f.Proto})
	}
	netdev, err := netx.BuildNetdev("net0", forwards)
	if err != nil {
		return fmt.Errorf("build netdev: %w", err)
	}

	spec := qemu.Spec{
		Name:  v.Name,
		CPUs:  v.CPUs,
		MemMB: v.MemMB,
		Accel: v.Accel,
		Drives: []qemu.Drive{
			{File: v.DiskPath(m.DataDir), Format: "qcow2", If: "virtio"},
			{File: v.SeedPath(m.DataDir), Format: "raw", If: "virtio"},
		},
		NetID:          "net0",
		NetdevValue:    netdev,
		ConsoleLogPath: v.ConsoleLog(m.DataDir),
		QMPPort:        v.QMPPort,
		MAC:            v.MAC,
		ExtraArgs:      extraQemuArgs(),
	}
	args, err := qemu.BuildArgs(spec)
	if err != nil {
		return fmt.Errorf("build qemu args: %w", err)
	}

	qemuLog := filepath.Join(v.Dir(m.DataDir), "qemu.log")
	pid, err := qemu.Start(m.Tools.System, args, qemuLog)
	if err != nil {
		return err
	}
	v.PID = pid
	if err := qemu.WritePIDFile(v.PIDPath(m.DataDir), pid); err != nil {
		return err
	}
	return m.Save(v)
}

// Stop shuts a VM down. Without force it asks the guest to power off via ACPI
// (QMP system_powerdown), escalating to QMP quit and finally a hard kill if the
// guest does not exit in time.
func (m *Manager) Stop(ctx context.Context, name string, force bool) error {
	v, err := m.Get(name)
	if err != nil {
		return err
	}
	unlock, err := m.lock(name)
	if err != nil {
		return err
	}
	defer unlock()

	if m.Status(v) != StatusRunning {
		// Already stopped; make sure recorded pid is cleared.
		return m.clearRunState(v)
	}

	qmpAddr := fmt.Sprintf("127.0.0.1:%d", v.QMPPort)
	if force {
		_ = qemu.Kill(v.PID)
	} else {
		if err := qemu.Powerdown(qmpAddr, 5*time.Second); err == nil && qemu.WaitExit(v.PID, 30*time.Second) {
			return m.clearRunState(v)
		}
		// Escalate.
		if err := qemu.Quit(qmpAddr, 5*time.Second); err == nil && qemu.WaitExit(v.PID, 10*time.Second) {
			return m.clearRunState(v)
		}
		_ = qemu.Kill(v.PID)
	}
	if !qemu.WaitExit(v.PID, 10*time.Second) {
		return fmt.Errorf("vm %q did not stop", name)
	}
	return m.clearRunState(v)
}

// extraQemuArgs parses VC_QEMU_EXTRA_ARGS into a slice of arguments.
func extraQemuArgs() []string {
	raw := strings.TrimSpace(os.Getenv(EnvQemuExtraArgs))
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

// clearRunState resets the recorded pid and removes the pid file.
func (m *Manager) clearRunState(v *VM) error {
	v.PID = 0
	_ = os.Remove(v.PIDPath(m.DataDir))
	return m.Save(v)
}

// Remove deletes a VM and all of its data. A running VM must be stopped first
// unless force is set (in which case it is killed).
func (m *Manager) Remove(ctx context.Context, name string, force bool) error {
	v, err := m.Get(name)
	if err != nil {
		return err
	}
	if m.Status(v) == StatusRunning {
		if !force {
			return fmt.Errorf("vm %q is running — stop it first or use --force", name)
		}
		if err := m.Stop(ctx, name, true); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(v.Dir(m.DataDir)); err != nil {
		return fmt.Errorf("remove vm dir: %w", err)
	}
	return nil
}

// dialOptions builds SSH dial options for a VM.
func (m *Manager) dialOptions(v *VM) (sshx.DialOptions, error) {
	id, err := sshx.LoadIdentity(v.KeyPath(m.DataDir))
	if err != nil {
		return sshx.DialOptions{}, err
	}
	return sshx.DialOptions{
		Addr:   fmt.Sprintf("127.0.0.1:%d", v.SSHPort),
		User:   v.User,
		Signer: id.Signer,
	}, nil
}

// waitReady waits until the VM's SSH is reachable.
func (m *Manager) waitReady(ctx context.Context, v *VM, timeout time.Duration) (*sshx.Client, error) {
	opts, err := m.dialOptions(v)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return sshx.WaitForSSH(cctx, opts)
}

// Connect opens a single SSH connection to a (running) VM.
func (m *Manager) Connect(ctx context.Context, v *VM) (*sshx.Client, error) {
	if m.Status(v) != StatusRunning {
		return nil, fmt.Errorf("vm %q is not running — start it with 'vc start %s'", v.Name, v.Name)
	}
	opts, err := m.dialOptions(v)
	if err != nil {
		return nil, err
	}
	return sshx.Dial(opts)
}
