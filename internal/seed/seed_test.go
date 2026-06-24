package seed

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
)

// readISO opens the image at path read-only and returns its iso9660 filesystem.
func readISO(t *testing.T, path string) *iso9660.FileSystem {
	t.Helper()
	d, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadOnly))
	if err != nil {
		t.Fatalf("open image %q: %v", path, err)
	}
	t.Cleanup(func() { _ = d.Close() })

	fs, err := d.GetFilesystem(0)
	if err != nil {
		t.Fatalf("get filesystem: %v", err)
	}
	if got := fs.Type(); got != filesystem.TypeISO9660 {
		t.Fatalf("filesystem type = %v, want TypeISO9660", got)
	}
	isoFS, ok := fs.(*iso9660.FileSystem)
	if !ok {
		t.Fatalf("filesystem is %T, want *iso9660.FileSystem", fs)
	}
	return isoFS
}

func TestWriteISO_RoundTrip(t *testing.T) {
	userData := []byte("#cloud-config\npackages:\n  - htop\n")
	metaData := []byte("instance-id: iid-1\nlocal-hostname: t\n")

	files := []File{
		{Name: "user-data", Data: userData},
		{Name: "meta-data", Data: metaData},
	}

	out := filepath.Join(t.TempDir(), "nested", "seed.iso")

	if err := WriteISO(out, files); err != nil {
		t.Fatalf("WriteISO: %v", err)
	}

	// (size) The image must be a positive multiple of the 2048-byte sector.
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() <= 0 {
		t.Fatalf("image size = %d, want > 0", info.Size())
	}
	if info.Size()%sectorSize != 0 {
		t.Fatalf("image size = %d, want multiple of %d", info.Size(), sectorSize)
	}

	fs := readISO(t, out)

	// (a) Volume identifier must be exactly "CIDATA". The ISO9660 identifier is
	// a fixed 32-byte field, right-padded by the writer, so trim trailing
	// padding before comparing.
	if got := trimLabel(fs.Label()); got != volumeIdentifier {
		t.Errorf("volume identifier = %q, want %q", got, volumeIdentifier)
	}

	// (b) Both files exist at root with byte-identical contents. The io/fs read
	// API rejects leading slashes, so read by bare name.
	for _, f := range files {
		got, err := fs.ReadFile(f.Name)
		if err != nil {
			t.Fatalf("read back %q: %v", f.Name, err)
		}
		if !bytes.Equal(got, f.Data) {
			t.Errorf("contents of %q = %q, want %q", f.Name, got, f.Data)
		}
	}
}

func TestWriteISO_EmptyFileData(t *testing.T) {
	files := []File{
		{Name: "meta-data", Data: nil},
		{Name: "user-data", Data: []byte{}},
	}
	out := filepath.Join(t.TempDir(), "seed.iso")
	if err := WriteISO(out, files); err != nil {
		t.Fatalf("WriteISO with empty data: %v", err)
	}

	fs := readISO(t, out)
	for _, f := range files {
		got, err := fs.ReadFile(f.Name)
		if err != nil {
			t.Fatalf("read back %q: %v", f.Name, err)
		}
		if len(got) != 0 {
			t.Errorf("contents of %q = %q, want empty", f.Name, got)
		}
	}
}

// trimLabel removes the trailing padding (NUL or space) from a fixed-width
// ISO9660 volume identifier.
func trimLabel(s string) string {
	return strings.TrimRight(s, " \x00")
}

func TestWriteISO_Overwrite(t *testing.T) {
	out := filepath.Join(t.TempDir(), "seed.iso")
	first := []File{{Name: "meta-data", Data: []byte("instance-id: a\n")}}
	second := []File{{Name: "meta-data", Data: []byte("instance-id: bbbbb\n")}}

	if err := WriteISO(out, first); err != nil {
		t.Fatalf("WriteISO first: %v", err)
	}
	if err := WriteISO(out, second); err != nil {
		t.Fatalf("WriteISO overwrite: %v", err)
	}

	fs := readISO(t, out)
	got, err := fs.ReadFile("meta-data")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, second[0].Data) {
		t.Errorf("after overwrite contents = %q, want %q", got, second[0].Data)
	}
}

func TestWriteISO_Errors(t *testing.T) {
	tmp := t.TempDir()
	tests := []struct {
		name    string
		outPath string
		files   []File
	}{
		{
			name:    "empty outPath",
			outPath: "",
			files:   []File{{Name: "meta-data", Data: []byte("x")}},
		},
		{
			name:    "whitespace outPath",
			outPath: "   ",
			files:   []File{{Name: "meta-data", Data: []byte("x")}},
		},
		{
			name:    "nil files",
			outPath: filepath.Join(tmp, "a.iso"),
			files:   nil,
		},
		{
			name:    "empty files slice",
			outPath: filepath.Join(tmp, "b.iso"),
			files:   []File{},
		},
		{
			name:    "empty file name",
			outPath: filepath.Join(tmp, "c.iso"),
			files:   []File{{Name: "", Data: []byte("x")}},
		},
		{
			name:    "name with forward slash",
			outPath: filepath.Join(tmp, "d.iso"),
			files:   []File{{Name: "sub/meta-data", Data: []byte("x")}},
		},
		{
			name:    "name with leading slash",
			outPath: filepath.Join(tmp, "e.iso"),
			files:   []File{{Name: "/meta-data", Data: []byte("x")}},
		},
		{
			name:    "name with backslash",
			outPath: filepath.Join(tmp, "f.iso"),
			files:   []File{{Name: "sub\\meta-data", Data: []byte("x")}},
		},
		{
			name:    "name is dot",
			outPath: filepath.Join(tmp, "g.iso"),
			files:   []File{{Name: ".", Data: []byte("x")}},
		},
		{
			name:    "name is dotdot",
			outPath: filepath.Join(tmp, "h.iso"),
			files:   []File{{Name: "..", Data: []byte("x")}},
		},
		{
			name:    "duplicate names",
			outPath: filepath.Join(tmp, "i.iso"),
			files: []File{
				{Name: "meta-data", Data: []byte("x")},
				{Name: "meta-data", Data: []byte("y")},
			},
		},
		{
			name:    "duplicate names differing only in case",
			outPath: filepath.Join(tmp, "j.iso"),
			files: []File{
				{Name: "meta-data", Data: []byte("x")},
				{Name: "META-DATA", Data: []byte("y")},
			},
		},
		{
			name:    "name with NUL byte",
			outPath: filepath.Join(tmp, "k.iso"),
			files:   []File{{Name: "meta\x00data", Data: []byte("x")}},
		},
		{
			name:    "name with newline control char",
			outPath: filepath.Join(tmp, "l.iso"),
			files:   []File{{Name: "meta\ndata", Data: []byte("x")}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := WriteISO(tc.outPath, tc.files)
			if err == nil {
				t.Fatalf("WriteISO(%q, ...) = nil, want error", tc.outPath)
			}
			// On validation failure no image must be left behind.
			if tc.outPath != "" && strings.TrimSpace(tc.outPath) != "" {
				if _, statErr := os.Stat(tc.outPath); statErr == nil {
					t.Errorf("image %q created despite error %v", tc.outPath, err)
				}
			}
		})
	}
}

func TestImageSize(t *testing.T) {
	tests := []struct {
		name  string
		files []File
	}{
		{name: "single small file", files: []File{{Name: "a", Data: []byte("hi")}}},
		{name: "empty file", files: []File{{Name: "a", Data: nil}}},
		{name: "large file", files: []File{{Name: "a", Data: bytes.Repeat([]byte("x"), 3*1024*1024)}}},
		{
			name: "many files",
			files: []File{
				{Name: "a", Data: []byte("1")},
				{Name: "b", Data: []byte("2")},
				{Name: "c", Data: []byte("3")},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := imageSize(tc.files)
			if got <= 0 {
				t.Fatalf("imageSize = %d, want > 0", got)
			}
			if got%sectorSize != 0 {
				t.Errorf("imageSize = %d, want multiple of %d", got, sectorSize)
			}
			if got < minImageSize {
				t.Errorf("imageSize = %d, want >= %d", got, minImageSize)
			}

			// It must hold all of the content plus the leading system area.
			var data int64
			for _, f := range tc.files {
				data += int64(len(f.Data))
			}
			if got < data {
				t.Errorf("imageSize = %d, smaller than content %d", got, data)
			}
		})
	}
}

// countWorkspaces returns the number of leftover go-diskfs ISO working
// directories under the OS temp dir. go-diskfs creates one per CreateFilesystem
// call and only removes it on a successful Finalize, so a successful WriteISO
// (or its error-path cleanup) must leave the count unchanged.
func countWorkspaces(t *testing.T) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "diskfs_iso*"))
	if err != nil {
		t.Fatalf("glob temp workspaces: %v", err)
	}
	return len(matches)
}

func TestWriteISO_NoWorkspaceLeak(t *testing.T) {
	before := countWorkspaces(t)

	out := filepath.Join(t.TempDir(), "seed.iso")
	files := []File{{Name: "meta-data", Data: []byte("instance-id: leak\n")}}
	for i := 0; i < 5; i++ {
		if err := WriteISO(out, files); err != nil {
			t.Fatalf("WriteISO iteration %d: %v", i, err)
		}
	}

	if after := countWorkspaces(t); after != before {
		t.Errorf("temp ISO workspaces leaked: before=%d after=%d", before, after)
	}
}

// TestWriteISO_ErrorPathNoWorkspaceLeak drives WriteISO through a failure that
// occurs after go-diskfs has allocated its ISO working directory (during file
// writing) but before Finalize. go-diskfs only removes that workspace on a
// successful Finalize, so WriteISO must reclaim it itself on error. The failure
// is induced with a filename that passes WriteISO's own validation yet is too
// long for the underlying OS filesystem, making the per-file write fail.
//
// Without WriteISO's deferred cleanup this leaves a stray diskfs_iso* directory
// in the OS temp dir; the assertion below catches that regression.
func TestWriteISO_ErrorPathNoWorkspaceLeak(t *testing.T) {
	before := countWorkspaces(t)

	out := filepath.Join(t.TempDir(), "seed.iso")
	// A 300-character name is a valid bare filename to WriteISO but exceeds the
	// host filesystem's per-component limit, so the write fails after the ISO
	// workspace has already been created.
	longName := strings.Repeat("a", 300)

	err := WriteISO(out, []File{{Name: longName, Data: []byte("x")}})
	if err == nil {
		t.Fatalf("WriteISO with over-long name = nil, want error")
	}

	if after := countWorkspaces(t); after != before {
		t.Errorf("temp ISO workspaces leaked on error path: before=%d after=%d", before, after)
	}
	// A failed write must not leave a partial image behind.
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("partial image %q left behind after error: stat err = %v", out, statErr)
	}
}

func TestRoundUp(t *testing.T) {
	tests := []struct {
		n, mult, want int64
	}{
		{0, 2048, 2048},
		{1, 2048, 2048},
		{2048, 2048, 2048},
		{2049, 2048, 4096},
		{-5, 2048, 2048},
		{4096, 2048, 4096},
		{100, 0, 100}, // mult <= 0 returns n unchanged
		{100, -1, 100},
	}
	for _, tc := range tests {
		if got := roundUp(tc.n, tc.mult); got != tc.want {
			t.Errorf("roundUp(%d, %d) = %d, want %d", tc.n, tc.mult, got, tc.want)
		}
	}
}
