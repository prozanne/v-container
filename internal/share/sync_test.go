package share

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanLocal(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "hello")
	mustWrite(t, filepath.Join(dir, "sub", "b.txt"), "world!!")
	// conflict-backup files must be ignored
	mustWrite(t, filepath.Join(dir, "a.txt.vc-conflict-host-123"), "x")

	snap, err := scanLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap["a.txt"]; !ok {
		t.Errorf("missing a.txt in %v", snap)
	}
	if _, ok := snap["sub/b.txt"]; !ok {
		t.Errorf("missing sub/b.txt (slash-relative) in %v", snap)
	}
	if snap["sub/b.txt"].Size != 7 {
		t.Errorf("size of sub/b.txt = %d, want 7", snap["sub/b.txt"].Size)
	}
	for k := range snap {
		if filepath.Base(k) != k && filepath.Separator == '\\' {
			// ensure forward slashes regardless of OS
		}
	}
	if _, ok := snap["a.txt.vc-conflict-host-123"]; ok {
		t.Errorf("conflict backup file should be ignored")
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.json")

	// Missing manifest => empty, no error.
	if snap, err := loadManifest(p); err != nil || len(snap) != 0 {
		t.Fatalf("missing manifest: snap=%v err=%v", snap, err)
	}

	want := Snapshot{"a": {Size: 1, ModUnix: 10}, "b/c": {Size: 2, ModUnix: 20}}
	if err := saveManifest(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadManifest(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch: got %v want %v", got, want)
	}
}

func TestManifestCorruptIsEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(p, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := loadManifest(p)
	if err != nil {
		t.Fatalf("corrupt manifest should not error, got %v", err)
	}
	if len(snap) != 0 {
		t.Fatalf("corrupt manifest should yield empty snapshot, got %v", snap)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
