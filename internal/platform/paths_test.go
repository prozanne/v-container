package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirHonorsVCHome(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-home")
	t.Setenv(EnvHome, want)
	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
}

func TestEnsureDataDirCreatesSubdirs(t *testing.T) {
	home := filepath.Join(t.TempDir(), "h")
	t.Setenv(EnvHome, home)
	got, err := EnsureDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != home {
		t.Fatalf("EnsureDataDir() = %q, want %q", got, home)
	}
	for _, sub := range []string{"vms", "images", "qemu"} {
		if !dirExists(filepath.Join(home, sub)) {
			t.Errorf("expected subdir %q to be created", sub)
		}
	}
}

func TestPathHelpers(t *testing.T) {
	d := filepath.FromSlash("/data")
	cases := map[string]string{
		VMsDir(d):     filepath.Join(d, "vms"),
		VMDir(d, "x"): filepath.Join(d, "vms", "x"),
		ImagesDir(d):  filepath.Join(d, "images"),
		QemuDir(d):    filepath.Join(d, "qemu"),
		ConfigPath(d): filepath.Join(d, "config.json"),
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
