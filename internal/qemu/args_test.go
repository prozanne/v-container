package qemu

import (
	"strings"
	"testing"
)

// valueAfter returns the argument immediately following the first occurrence
// of flag, and whether flag was found with a following value.
func valueAfter(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
	}
	return "", false
}

// contains reports whether s appears anywhere in args.
func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

// count returns how many times flag appears in args.
func count(args []string, flag string) int {
	n := 0
	for _, a := range args {
		if a == flag {
			n++
		}
	}
	return n
}

// allValuesAfter collects every argument that immediately follows flag.
func allValuesAfter(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// baseSpec is a minimal valid Spec used as a starting point in tests.
func baseSpec() Spec {
	return Spec{
		Drives: []Drive{{File: "/disks/disk.qcow2", Format: "qcow2"}},
	}
}

func TestBuildArgs_MachineAndAccel(t *testing.T) {
	tests := []struct {
		name      string
		accel     string
		machine   string
		wantMach  string
		wantAccel string
	}{
		{
			name:      "whpx fallback to tcg",
			accel:     "whpx:tcg",
			wantMach:  "q35,accel=whpx:tcg",
			wantAccel: "tcg,thread=multi",
		},
		{
			name:      "tcg only",
			accel:     "tcg",
			wantMach:  "q35,accel=tcg",
			wantAccel: "tcg,thread=multi",
		},
		{
			name:      "empty accel defaults to tcg",
			accel:     "",
			wantMach:  "q35,accel=tcg",
			wantAccel: "tcg,thread=multi",
		},
		{
			name:      "custom machine",
			accel:     "tcg",
			machine:   "pc",
			wantMach:  "pc,accel=tcg",
			wantAccel: "tcg,thread=multi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			s.Accel = tt.accel
			s.Machine = tt.machine
			args, err := BuildArgs(s)
			if err != nil {
				t.Fatalf("BuildArgs returned error: %v", err)
			}
			got, ok := valueAfter(args, "-machine")
			if !ok {
				t.Fatalf("-machine not present in args: %v", args)
			}
			if got != tt.wantMach {
				t.Errorf("value after -machine = %q, want %q", got, tt.wantMach)
			}
			gotAccel, ok := valueAfter(args, "-accel")
			if !ok {
				t.Fatalf("-accel not present in args: %v", args)
			}
			if gotAccel != tt.wantAccel {
				t.Errorf("value after -accel = %q, want %q", gotAccel, tt.wantAccel)
			}
		})
	}
}

func TestBuildArgs_AlwaysTCGObject(t *testing.T) {
	// Even when hardware acceleration is selected, -accel tcg,thread=multi
	// must always be present to configure the fallback object.
	s := baseSpec()
	s.Accel = "whpx:tcg"
	args, err := BuildArgs(s)
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	got, ok := valueAfter(args, "-accel")
	if !ok || got != "tcg,thread=multi" {
		t.Errorf("value after -accel = %q (found=%v), want %q", got, ok, "tcg,thread=multi")
	}
}

func TestBuildArgs_CPUAndMemDefaults(t *testing.T) {
	tests := []struct {
		name    string
		cpus    int
		mem     int
		wantSMP string
		wantMem string
	}{
		{name: "explicit values", cpus: 4, mem: 8192, wantSMP: "4", wantMem: "8192"},
		{name: "zero uses defaults", cpus: 0, mem: 0, wantSMP: "2", wantMem: "2048"},
		{name: "negative uses defaults", cpus: -1, mem: -100, wantSMP: "2", wantMem: "2048"},
		{name: "one cpu", cpus: 1, mem: 512, wantSMP: "1", wantMem: "512"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			s.CPUs = tt.cpus
			s.MemMB = tt.mem
			args, err := BuildArgs(s)
			if err != nil {
				t.Fatalf("BuildArgs returned error: %v", err)
			}
			if got, _ := valueAfter(args, "-smp"); got != tt.wantSMP {
				t.Errorf("value after -smp = %q, want %q", got, tt.wantSMP)
			}
			if got, _ := valueAfter(args, "-m"); got != tt.wantMem {
				t.Errorf("value after -m = %q, want %q", got, tt.wantMem)
			}
		})
	}
}

func TestBuildArgs_CPUMax(t *testing.T) {
	args, err := BuildArgs(baseSpec())
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	if got, _ := valueAfter(args, "-cpu"); got != "max" {
		t.Errorf("value after -cpu = %q, want %q", got, "max")
	}
}

func TestBuildArgs_Name(t *testing.T) {
	t.Run("name set", func(t *testing.T) {
		s := baseSpec()
		s.Name = "myvm"
		args, err := BuildArgs(s)
		if err != nil {
			t.Fatalf("BuildArgs returned error: %v", err)
		}
		if got, ok := valueAfter(args, "-name"); !ok || got != "myvm" {
			t.Errorf("value after -name = %q (found=%v), want %q", got, ok, "myvm")
		}
	})
	t.Run("name empty omits flag", func(t *testing.T) {
		args, err := BuildArgs(baseSpec())
		if err != nil {
			t.Fatalf("BuildArgs returned error: %v", err)
		}
		if contains(args, "-name") {
			t.Errorf("-name should be omitted when Name is empty: %v", args)
		}
	})
}

func TestBuildArgs_Drives(t *testing.T) {
	tests := []struct {
		name   string
		drives []Drive
		want   []string
	}{
		{
			name:   "single qcow2 default interface",
			drives: []Drive{{File: "/d/disk.qcow2", Format: "qcow2"}},
			want:   []string{"if=virtio,format=qcow2,file=/d/disk.qcow2"},
		},
		{
			name: "multiple drives disk seed iso",
			drives: []Drive{
				{File: "/d/disk.qcow2", Format: "qcow2"},
				{File: "/d/seed.img", Format: "raw"},
				{File: "/d/installer.iso", Format: "raw", If: "ide"},
			},
			want: []string{
				"if=virtio,format=qcow2,file=/d/disk.qcow2",
				"if=virtio,format=raw,file=/d/seed.img",
				"if=ide,format=raw,file=/d/installer.iso",
			},
		},
		{
			name:   "windows-style path passed through unaltered",
			drives: []Drive{{File: `C:\vms\disk.qcow2`, Format: "qcow2"}},
			want:   []string{`if=virtio,format=qcow2,file=C:\vms\disk.qcow2`},
		},
		{
			name:   "explicit interface override",
			drives: []Drive{{File: "/d/disk.raw", Format: "raw", If: "scsi"}},
			want:   []string{"if=scsi,format=raw,file=/d/disk.raw"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			s.Drives = tt.drives
			args, err := BuildArgs(s)
			if err != nil {
				t.Fatalf("BuildArgs returned error: %v", err)
			}
			got := allValuesAfter(args, "-drive")
			if len(got) != len(tt.want) {
				t.Fatalf("got %d -drive args %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("drive %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildArgs_Networking(t *testing.T) {
	tests := []struct {
		name        string
		netID       string
		netdev      string
		mac         string
		wantNetdev  string
		wantDevice  string
		wantPresent bool
	}{
		{
			name:        "user net with hostfwd no mac",
			netID:       "net0",
			netdev:      "user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22",
			wantNetdev:  "user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22",
			wantDevice:  "virtio-net-pci,netdev=net0,romfile=",
			wantPresent: true,
		},
		{
			name:        "with mac",
			netID:       "net0",
			netdev:      "user,id=net0",
			mac:         "52:54:00:12:34:56",
			wantNetdev:  "user,id=net0",
			wantDevice:  "virtio-net-pci,netdev=net0,romfile=,mac=52:54:00:12:34:56",
			wantPresent: true,
		},
		{
			name:        "no networking",
			netID:       "",
			netdev:      "",
			wantPresent: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			s.NetID = tt.netID
			s.NetdevValue = tt.netdev
			s.MAC = tt.mac
			args, err := BuildArgs(s)
			if err != nil {
				t.Fatalf("BuildArgs returned error: %v", err)
			}
			if !tt.wantPresent {
				if contains(args, "-netdev") {
					t.Errorf("-netdev should be absent when networking unset: %v", args)
				}
				// No virtio-net-pci device should be emitted.
				for _, v := range allValuesAfter(args, "-device") {
					if strings.HasPrefix(v, "virtio-net-pci") {
						t.Errorf("unexpected NIC device %q", v)
					}
				}
				return
			}
			if got, ok := valueAfter(args, "-netdev"); !ok || got != tt.wantNetdev {
				t.Errorf("value after -netdev = %q (found=%v), want %q", got, ok, tt.wantNetdev)
			}
			gotDevice := ""
			for _, v := range allValuesAfter(args, "-device") {
				if strings.HasPrefix(v, "virtio-net-pci") {
					gotDevice = v
				}
			}
			if gotDevice != tt.wantDevice {
				t.Errorf("NIC -device = %q, want %q", gotDevice, tt.wantDevice)
			}
		})
	}
}

func TestBuildArgs_DisplayNone(t *testing.T) {
	args, err := BuildArgs(baseSpec())
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	if got, ok := valueAfter(args, "-display"); !ok || got != "none" {
		t.Errorf("value after -display = %q (found=%v), want %q", got, ok, "none")
	}
}

func TestBuildArgs_Serial(t *testing.T) {
	tests := []struct {
		name    string
		logPath string
		want    string
	}{
		{name: "log path set", logPath: "/var/log/vm/console.log", want: "file:/var/log/vm/console.log"},
		{name: "windows log path", logPath: `C:\logs\console.log`, want: `file:C:\logs\console.log`},
		{name: "empty path none", logPath: "", want: "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			s.ConsoleLogPath = tt.logPath
			args, err := BuildArgs(s)
			if err != nil {
				t.Fatalf("BuildArgs returned error: %v", err)
			}
			if got, ok := valueAfter(args, "-serial"); !ok || got != tt.want {
				t.Errorf("value after -serial = %q (found=%v), want %q", got, ok, tt.want)
			}
		})
	}
}

func TestBuildArgs_QMP(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		present bool
		want    string
	}{
		{name: "port set", port: 4444, present: true, want: "tcp:127.0.0.1:4444,server=on,wait=off"},
		{name: "zero port absent", port: 0, present: false},
		{name: "negative port absent", port: -1, present: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			s.QMPPort = tt.port
			args, err := BuildArgs(s)
			if err != nil {
				t.Fatalf("BuildArgs returned error: %v", err)
			}
			got, ok := valueAfter(args, "-qmp")
			if tt.present {
				if !ok || got != tt.want {
					t.Errorf("value after -qmp = %q (found=%v), want %q", got, ok, tt.want)
				}
			} else if contains(args, "-qmp") {
				t.Errorf("-qmp should be absent for port %d: %v", tt.port, args)
			}
		})
	}
}

func TestBuildArgs_ExtraArgs(t *testing.T) {
	s := baseSpec()
	s.ExtraArgs = []string{"-no-reboot", "-rtc", "base=utc"}
	args, err := BuildArgs(s)
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	// ExtraArgs come last.
	n := len(args)
	if n < 3 || args[n-3] != "-no-reboot" || args[n-2] != "-rtc" || args[n-1] != "base=utc" {
		t.Errorf("ExtraArgs not appended at the end: %v", args)
	}
}

func TestBuildArgs_ReturnsFreshSlice(t *testing.T) {
	// Two calls with the same spec must not share backing memory.
	s := baseSpec()
	a, err := BuildArgs(s)
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	b, err := BuildArgs(s)
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	if len(a) == 0 {
		t.Fatal("expected non-empty args")
	}
	a[0] = "MUTATED"
	if b[0] == "MUTATED" {
		t.Error("BuildArgs returned a shared slice across calls")
	}
}

func TestBuildArgs_DoesNotMutateSpec(t *testing.T) {
	s := Spec{
		Drives: []Drive{{File: "/d/disk.qcow2", Format: "qcow2"}}, // If empty
	}
	if _, err := BuildArgs(s); err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	if s.Machine != "" || s.CPUs != 0 || s.MemMB != 0 || s.Accel != "" {
		t.Errorf("BuildArgs mutated the input Spec: %+v", s)
	}
	if s.Drives[0].If != "" {
		t.Errorf("BuildArgs mutated Drive.If: %q", s.Drives[0].If)
	}
}

func TestBuildArgs_Errors(t *testing.T) {
	tests := []struct {
		name    string
		spec    Spec
		wantSub string
	}{
		{
			name:    "no drives",
			spec:    Spec{},
			wantSub: "at least one drive",
		},
		{
			name:    "nil drives slice",
			spec:    Spec{Drives: nil},
			wantSub: "at least one drive",
		},
		{
			name: "empty drive file",
			spec: Spec{
				Drives: []Drive{{File: "", Format: "qcow2"}},
			},
			wantSub: "empty file path",
		},
		{
			name: "bad drive format",
			spec: Spec{
				Drives: []Drive{{File: "/d/disk.img", Format: "vmdk"}},
			},
			wantSub: "invalid format",
		},
		{
			name: "missing format",
			spec: Spec{
				Drives: []Drive{{File: "/d/disk.img", Format: ""}},
			},
			wantSub: "invalid format",
		},
		{
			name: "netid set but netdev empty",
			spec: Spec{
				Drives: []Drive{{File: "/d/disk.qcow2", Format: "qcow2"}},
				NetID:  "net0",
			},
			wantSub: "both be set or both be empty",
		},
		{
			name: "netdev set but netid empty",
			spec: Spec{
				Drives:      []Drive{{File: "/d/disk.qcow2", Format: "qcow2"}},
				NetdevValue: "user,id=net0",
			},
			wantSub: "both be set or both be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := BuildArgs(tt.spec)
			if err == nil {
				t.Fatalf("expected error, got args: %v", args)
			}
			if args != nil {
				t.Errorf("expected nil args on error, got: %v", args)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestBuildArgs_FullSpecOrdering(t *testing.T) {
	// A representative full spec: verify singleton flags appear exactly once
	// and the overall command is well-formed.
	s := Spec{
		Name:           "demo",
		CPUs:           4,
		MemMB:          4096,
		Accel:          "whpx:tcg",
		Drives:         []Drive{{File: "/d/disk.qcow2", Format: "qcow2"}, {File: "/d/seed.img", Format: "raw"}},
		NetID:          "net0",
		NetdevValue:    "user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22",
		MAC:            "52:54:00:aa:bb:cc",
		ConsoleLogPath: "/d/console.log",
		QMPPort:        4444,
		ExtraArgs:      []string{"-no-reboot"},
	}
	args, err := BuildArgs(s)
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	for _, flag := range []string{"-name", "-machine", "-cpu", "-smp", "-m", "-netdev", "-display", "-serial", "-qmp"} {
		if c := count(args, flag); c != 1 {
			t.Errorf("flag %q appears %d times, want 1; args=%v", flag, c, args)
		}
	}
	if c := count(args, "-accel"); c != 1 {
		t.Errorf("flag -accel appears %d times, want 1; args=%v", c, args)
	}
	if c := count(args, "-drive"); c != 2 {
		t.Errorf("flag -drive appears %d times, want 2; args=%v", c, args)
	}
}
