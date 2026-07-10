package qemu

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// A VM's recorded pid can outlive its QEMU process (e.g. the host rebooted and
// the OS recycled the pid for an unrelated program). Aliveness alone must not
// count as "running", or Stop would kill an innocent process.

func TestIsRunningRejectsNonQemuProcess(t *testing.T) {
	pid := os.Getpid() // this test binary: alive, but not QEMU
	if IsRunning(pid) {
		t.Fatalf("IsRunning(%d) = true for a live non-qemu process (pid reuse would be treated as a running VM)", pid)
	}
}

func TestKillRefusesNonQemuProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep(1)")
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if err := Kill(pid); err == nil {
		t.Fatalf("Kill(%d) = nil, want refusal for a live non-qemu process", pid)
	}
	if !processAlive(pid) {
		t.Fatalf("Kill(%d) terminated a non-qemu process", pid)
	}
}

func TestWaitExitObservesReleasedChildExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix zombie semantics")
	}
	// Start() releases the qemu child without ever waiting on it, so when the
	// guest powers off while the launching process is still alive (vc restart,
	// tests), the child becomes a zombie: it still answers signal 0. WaitExit
	// must still report it as exited. The child masquerades as qemu-system so
	// the identity check cannot mask the zombie handling.
	fake := filepath.Join(t.TempDir(), "qemu-system-fake")
	src, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Skip("no /bin/sleep")
	}
	if err := os.WriteFile(fake, src, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(fake, "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil { // mimic qemu.Start
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}

	if !WaitExit(pid, 5*time.Second) {
		t.Fatalf("WaitExit(%d) did not observe the exit of a released (zombie) child", pid)
	}
}
