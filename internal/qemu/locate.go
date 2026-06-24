package qemu

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Tools holds the resolved paths to the QEMU binaries we use.
type Tools struct {
	System string // qemu-system-x86_64[.exe]
	Img    string // qemu-img[.exe]
}

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// Locate resolves the QEMU binaries. Search order:
//  1. configuredPath (if set) — treated as the qemu-system path or its dir
//  2. a "qemu" folder next to the running executable (bundled release)
//  3. dataDir/qemu (provisioned by `vc setup`)
//  4. PATH
//
// qemu-img is looked for alongside qemu-system first, then on PATH.
func Locate(dataDir, configuredPath string, exeDir string) (Tools, error) {
	sysName := exeName("qemu-system-x86_64")
	imgName := exeName("qemu-img")

	var candidates []string
	if configuredPath != "" {
		if isFile(configuredPath) {
			candidates = append(candidates, configuredPath)
		} else {
			candidates = append(candidates, filepath.Join(configuredPath, sysName))
		}
	}
	if exeDir != "" {
		candidates = append(candidates, filepath.Join(exeDir, "qemu", sysName))
		candidates = append(candidates, filepath.Join(exeDir, sysName))
	}
	if dataDir != "" {
		candidates = append(candidates, filepath.Join(dataDir, "qemu", sysName))
	}

	var system string
	for _, c := range candidates {
		if isFile(c) {
			system = c
			break
		}
	}
	if system == "" {
		if p, err := exec.LookPath(sysName); err == nil {
			system = p
		}
	}
	if system == "" {
		return Tools{}, fmt.Errorf("%s not found: bundle it next to vc in a 'qemu' folder, run 'vc setup', or add it to PATH", sysName)
	}

	// qemu-img: prefer the same directory as qemu-system, then PATH.
	img := filepath.Join(filepath.Dir(system), imgName)
	if !isFile(img) {
		if p, err := exec.LookPath(imgName); err == nil {
			img = p
		} else {
			return Tools{System: system}, fmt.Errorf("%s not found next to %s or on PATH", imgName, system)
		}
	}
	return Tools{System: system, Img: img}, nil
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// ProbeAccelerators runs `qemu-system -accel help` and returns the list of
// accelerators compiled into the binary (e.g. ["tcg","whpx"] or ["tcg","kvm"]).
func ProbeAccelerators(ctx context.Context, qemuSystem string) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, qemuSystem, "-accel", "help")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("probe accelerators: %w", err)
	}
	var accels []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "accelerators") {
			continue
		}
		accels = append(accels, line)
	}
	return accels, nil
}

// SelectAccel chooses the accelerator string for -machine accel=... given the
// available accelerators and the host. It prefers a hardware accelerator with
// automatic TCG fallback:
//   - Windows with whpx        -> "whpx:tcg"
//   - Linux with kvm + /dev/kvm -> "kvm:tcg"
//   - otherwise                 -> "tcg"
func SelectAccel(available []string) string {
	has := func(name string) bool {
		for _, a := range available {
			if a == name {
				return true
			}
		}
		return false
	}
	switch runtime.GOOS {
	case "windows":
		if has("whpx") {
			return "whpx:tcg"
		}
	case "linux":
		if has("kvm") && isFile("/dev/kvm") {
			return "kvm:tcg"
		}
	}
	return "tcg"
}

// IsSoftware reports whether the chosen accel string will run under pure software
// emulation (TCG only), so the CLI can warn about performance.
func IsSoftware(accel string) bool {
	return accel == "tcg"
}
