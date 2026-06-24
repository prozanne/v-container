package qemu

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Start launches qemu-system detached from the parent: it keeps running after
// vc exits (a background VM), writes no console window on Windows, and sends its
// own stdout/stderr to logPath. It returns the OS process id. The guest serial
// console is captured separately via the -serial argument in args.
func Start(qemuSystem string, args []string, logPath string) (int, error) {
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open qemu log %s: %w", logPath, err)
	}
	defer logf.Close()

	cmd := exec.Command(qemuSystem, args...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	cmd.SysProcAttr = detachSysProcAttr()

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start qemu: %w", err)
	}
	pid := cmd.Process.Pid
	// Release so the child becomes independent of this process.
	if err := cmd.Process.Release(); err != nil {
		return pid, fmt.Errorf("release qemu process: %w", err)
	}
	return pid, nil
}

// WritePIDFile records pid to path.
func WritePIDFile(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

// ReadPIDFile reads a pid from path. Returns 0 (no error) if the file is absent.
func ReadPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid file %s: %w", path, err)
	}
	return pid, nil
}

// IsRunning reports whether a process with pid is currently alive.
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAlive(pid)
}

// WaitExit polls until the process exits or timeout elapses; returns true if it
// exited within the deadline.
func WaitExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return !IsRunning(pid)
}

// Kill forcibly terminates the process (last resort).
func Kill(pid int) error {
	if pid <= 0 {
		return nil
	}
	return killProcess(pid)
}
