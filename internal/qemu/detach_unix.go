//go:build !windows

package qemu

import (
	"fmt"
	"os"
	"strings"
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
	if proc.Signal(syscall.Signal(0)) != nil {
		return false
	}
	// A qemu child we spawned is never waited on (Start releases it), so when
	// the guest powers off it lingers as a zombie that still answers signal 0.
	// A zombie is dead: reap it if it is ours and report it gone.
	if _, state, ok := procStat(pid); ok && state == 'Z' {
		var ws syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
		return false
	}
	return true
}

func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// processIsQemu reports whether pid looks like a QEMU binary, so that a stale
// pid recycled by the OS for an unrelated program is never mistaken for a VM.
// Platforms without procfs cannot verify and fall back to "yes".
func processIsQemu(pid int) bool {
	comm, _, ok := procStat(pid)
	if !ok {
		if _, err := os.Stat("/proc/self"); err != nil {
			return true // no procfs (e.g. macOS): cannot verify identity
		}
		return false // procfs exists but pid is gone or unreadable
	}
	return strings.HasPrefix(comm, "qemu-system")
}

// procStat reads /proc/<pid>/stat and returns the command name (truncated by
// the kernel to 15 bytes) and the process state. comm is parenthesized and may
// itself contain parentheses, so it ends at the last ')'.
func procStat(pid int) (comm string, state byte, ok bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", 0, false
	}
	s := string(data)
	lparen := strings.IndexByte(s, '(')
	rparen := strings.LastIndexByte(s, ')')
	if lparen < 0 || rparen < lparen || rparen+2 >= len(s) {
		return "", 0, false
	}
	return s[lparen+1 : rparen], s[rparen+2], true
}
