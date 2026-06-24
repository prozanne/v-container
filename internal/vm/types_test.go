package vm

import (
	"path/filepath"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"a", "ab", "dev", "ubuntu-24", "x1-y2", "a0123456789012345678901234567890"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) unexpected error: %v", n, err)
		}
	}
	invalid := []string{"", "-x", "x-", "X", "a_b", "a b", "a/b", "café",
		"toolongtoolongtoolongtoolongtoolong"} // 34 chars
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) expected error, got nil", n)
		}
	}
}

func TestPathHelpers(t *testing.T) {
	v := &VM{Name: "dev"}
	d := filepath.FromSlash("/data")
	vmdir := filepath.Join(d, "vms", "dev")
	cases := map[string]string{
		v.Dir(d):          vmdir,
		v.RecordPath(d):   filepath.Join(vmdir, "vm.json"),
		v.DiskPath(d):     filepath.Join(vmdir, "disk.qcow2"),
		v.SeedPath(d):     filepath.Join(vmdir, "seed.img"),
		v.KeyPath(d):      filepath.Join(vmdir, "id_ed25519"),
		v.PubKeyPath(d):   filepath.Join(vmdir, "id_ed25519.pub"),
		v.ConsoleLog(d):   filepath.Join(vmdir, "console.log"),
		v.PIDPath(d):      filepath.Join(vmdir, "qemu.pid"),
		v.LockPath(d):     filepath.Join(vmdir, "vm.lock"),
		v.WorkspaceDir(d): filepath.Join(vmdir, "workspace"),
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
