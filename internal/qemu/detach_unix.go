//go:build !windows

package qemu

import (
	"os"
	"syscall"
)

// detachSysProcAttr puts qemu in its own session so it is not killed when the
// vc process (or its terminal) goes away.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, signal 0 probes existence without affecting the process.
	return proc.Signal(syscall.Signal(0)) == nil
}

func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
