// Package qemu builds the command line used to launch a headless
// qemu-system-x86_64 virtual machine. This file holds the pure argument
// construction logic (no process execution); other files in the package add
// the launching/monitoring machinery.
package qemu

import (
	"fmt"
	"strconv"
	"strings"
)

// Default values applied when a Spec field is left zero/empty.
const (
	defaultMachine = "q35"
	defaultCPUs    = 2
	defaultMemMB   = 2048
	defaultDriveIf = "virtio"
	defaultAccel   = "tcg"
)

// Drive describes a single disk attached to the VM.
//
// Format must be either "qcow2" or "raw". If is the QEMU interface type
// (e.g. "virtio"); when empty it defaults to "virtio".
type Drive struct {
	File   string
	Format string
	If     string
}

// Spec is the full description of a headless VM from which the
// qemu-system-x86_64 argument list is derived.
type Spec struct {
	Name           string  // VM name, used for -name
	CPUs           int     // -smp
	MemMB          int     // -m (MiB)
	Accel          string  // "whpx:tcg" or "tcg" — emitted as a -accel fallback list
	Machine        string  // default "q35"
	Drives         []Drive // disk + seed (+ optional ISO)
	NetID          string  // e.g. "net0"
	NetdevValue    string  // full -netdev value
	ConsoleLogPath string  // -serial file:... (empty => -serial none)
	QMPPort        int     // >0 => -qmp tcp:127.0.0.1:PORT,server=on,wait=off
	MAC            string  // optional NIC MAC
	ExtraArgs      []string
}

// validFormats is the set of accepted Drive.Format values.
var validFormats = map[string]bool{
	"qcow2": true,
	"raw":   true,
}

// BuildArgs returns the argument list for qemu-system-x86_64 (excluding the
// binary path itself). It is a pure function: it performs no process
// execution and does not touch the filesystem. The returned slice is freshly
// allocated on every call.
//
// Defaults are applied for zero/empty fields (see the package constants).
// Inputs are validated and a descriptive error is returned for any invalid
// combination; the function never panics on bad input.
func BuildArgs(s Spec) ([]string, error) {
	// Validate networking: NetID and NetdevValue must both be set or both be
	// empty. A half-configured NIC would produce a broken command line.
	if (s.NetID == "") != (s.NetdevValue == "") {
		return nil, fmt.Errorf("qemu: networking requires NetID and NetdevValue to both be set or both be empty (NetID=%q, NetdevValue=%q)", s.NetID, s.NetdevValue)
	}

	// Validate drives: need at least one, each with a non-empty file and a
	// recognised format.
	if len(s.Drives) == 0 {
		return nil, fmt.Errorf("qemu: at least one drive is required")
	}
	for i, d := range s.Drives {
		if d.File == "" {
			return nil, fmt.Errorf("qemu: drive %d has an empty file path", i)
		}
		if !validFormats[d.Format] {
			return nil, fmt.Errorf("qemu: drive %d (%s) has invalid format %q (want %q or %q)", i, d.File, d.Format, "qcow2", "raw")
		}
	}

	// Resolve defaults without mutating the caller's Spec.
	machine := s.Machine
	if machine == "" {
		machine = defaultMachine
	}
	cpus := s.CPUs
	if cpus <= 0 {
		cpus = defaultCPUs
	}
	mem := s.MemMB
	if mem <= 0 {
		mem = defaultMemMB
	}
	accel := s.Accel
	if accel == "" {
		accel = defaultAccel
	}

	// Preallocate generously to avoid repeated growth.
	args := make([]string, 0, 24+2*len(s.Drives)+len(s.ExtraArgs))

	if s.Name != "" {
		args = append(args, "-name", s.Name)
	}

	// Accelerator selection. QEMU rejects mixing "-machine accel=" with the
	// standalone "-accel" option, so we use ONLY -accel and list each candidate
	// as its own flag: with several -accel options QEMU tries them in order and
	// uses the next when one fails to initialize (e.g. whpx then tcg). The tcg
	// object gets thread=multi for multi-threaded software emulation.
	args = append(args, "-machine", machine)
	for _, ac := range splitAccel(accel) {
		if ac == "tcg" {
			args = append(args, "-accel", "tcg,thread=multi")
		} else {
			args = append(args, "-accel", ac)
		}
	}
	args = append(args, "-cpu", "max")
	args = append(args, "-smp", strconv.Itoa(cpus))
	args = append(args, "-m", strconv.Itoa(mem))

	for _, d := range s.Drives {
		ifType := d.If
		if ifType == "" {
			ifType = defaultDriveIf
		}
		args = append(args, "-drive", "if="+ifType+",format="+d.Format+",file="+d.File)
	}

	if s.NetID != "" {
		args = append(args, "-netdev", s.NetdevValue)
		// romfile= (empty) disables the PXE option ROM. mac= is appended only
		// when a MAC was supplied.
		device := "virtio-net-pci,netdev=" + s.NetID + ",romfile="
		if s.MAC != "" {
			device += ",mac=" + s.MAC
		}
		args = append(args, "-device", device)
	}

	args = append(args, "-display", "none")

	if s.ConsoleLogPath != "" {
		args = append(args, "-serial", "file:"+s.ConsoleLogPath)
	} else {
		args = append(args, "-serial", "none")
	}

	if s.QMPPort > 0 {
		args = append(args, "-qmp", "tcp:127.0.0.1:"+strconv.Itoa(s.QMPPort)+",server=on,wait=off")
	}

	args = append(args, s.ExtraArgs...)

	return args, nil
}

// splitAccel parses an accelerator spec like "whpx:tcg" into ["whpx","tcg"],
// dropping empty entries. An empty spec yields ["tcg"].
func splitAccel(accel string) []string {
	parts := strings.Split(accel, ":")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"tcg"}
	}
	return out
}
