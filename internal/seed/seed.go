// Package seed builds a cloud-init NoCloud "seed" disk image. The image is an
// ISO9660 filesystem whose Volume Identifier is set to "CIDATA", which is the
// filesystem label cloud-init's NoCloud datasource looks for. Each provided
// file is placed at the root of the image (e.g. "user-data", "meta-data") so
// that cloud-init reads it on first boot.
package seed

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
)

// volumeIdentifier is the ISO9660 Volume Identifier (filesystem label) that
// cloud-init's NoCloud datasource discovers. cloud-init accepts "cidata" or
// "CIDATA"; we write it uppercase.
const volumeIdentifier = "CIDATA"

// sectorSize is the ISO9660 logical sector size in bytes. Image sizes are
// always a multiple of this value.
const sectorSize = 2048

// minImageSize is the floor for the pre-sized disk image (1 MiB). It is well
// above the ISO9660 system area + volume descriptors + a handful of small
// files, which keeps Finalize from running out of space.
const minImageSize = 1 << 20

// File is one file placed at the root of the seed image.
type File struct {
	// Name is the bare filename at the root of the image (no path separators).
	Name string
	// Data is the file's raw contents.
	Data []byte
}

// WriteISO writes an ISO9660 image to outPath containing the given files at the
// root directory, with the ISO9660 Volume Identifier set to "CIDATA" so that
// cloud-init's NoCloud datasource discovers it by filesystem label.
//
// outPath must be non-empty, files must contain at least one entry, and every
// File.Name must be a non-empty bare filename (no path separators). The parent
// directory of outPath is created if it does not already exist. The resulting
// image size is always a positive multiple of the 2048-byte ISO9660 sector.
func WriteISO(outPath string, files []File) error {
	if err := validate(outPath, files); err != nil {
		return err
	}

	// Ensure the parent directory exists.
	parent := filepath.Dir(outPath)
	if parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("seed: create parent directory %q: %w", parent, err)
		}
	}

	// diskfs.Create requires the target path to not exist; remove any stale
	// file first so callers can overwrite a previous image.
	if err := os.Remove(outPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("seed: remove existing image %q: %w", outPath, err)
	}

	size := imageSize(files)

	d, err := diskfs.Create(outPath, size, diskfs.SectorSizeDefault)
	if err != nil {
		return fmt.Errorf("seed: create disk image %q: %w", outPath, err)
	}
	// On any failure path below, make sure we don't leave a partial image and a
	// dangling backend behind.
	committed := false
	defer func() {
		_ = d.Close()
		if !committed {
			_ = os.Remove(outPath)
		}
	}()

	// ISO9660 uses 2048-byte logical blocks regardless of the disk's default
	// blocksize; pin it so Finalize lays out a standard image.
	d.LogicalBlocksize = sectorSize

	fs, err := d.CreateFilesystem(disk.FilesystemSpec{
		Partition:   0, // whole image, no partition table
		FSType:      filesystem.TypeISO9660,
		VolumeLabel: volumeIdentifier,
	})
	if err != nil {
		return fmt.Errorf("seed: create iso9660 filesystem: %w", err)
	}

	isoFS, ok := fs.(*iso9660.FileSystem)
	if !ok {
		return fmt.Errorf("seed: unexpected filesystem type %T, want *iso9660.FileSystem", fs)
	}
	// CreateFilesystem allocates a temporary working directory for the ISO that
	// is only cleaned up by Finalize on success. On any error path before
	// Finalize completes, that workspace would leak; isoFS.Close removes it.
	// After a successful Finalize the workspace is already gone and Close is a
	// no-op, so it is always safe to defer.
	defer func() { _ = isoFS.Close() }()

	for _, f := range files {
		if err := writeOne(isoFS, f); err != nil {
			return err
		}
	}

	if err := isoFS.Finalize(iso9660.FinalizeOptions{
		RockRidge:        true, // harmless; preserves exact long filenames
		VolumeIdentifier: volumeIdentifier,
	}); err != nil {
		return fmt.Errorf("seed: finalize iso9660 image: %w", err)
	}

	committed = true
	return nil
}

// writeOne writes a single file's contents into the ISO filesystem at its root.
func writeOne(fs *iso9660.FileSystem, f File) error {
	// Root the name so it lands at the top of the image.
	p := "/" + f.Name

	w, err := fs.OpenFile(p, os.O_CREATE|os.O_WRONLY)
	if err != nil {
		return fmt.Errorf("seed: open %q for write: %w", f.Name, err)
	}

	if len(f.Data) > 0 {
		if _, err := w.Write(f.Data); err != nil {
			_ = w.Close()
			return fmt.Errorf("seed: write %q: %w", f.Name, err)
		}
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("seed: close %q: %w", f.Name, err)
	}
	return nil
}

// validate checks the public inputs and returns a descriptive error for any
// violation. It does not touch the filesystem.
func validate(outPath string, files []File) error {
	if strings.TrimSpace(outPath) == "" {
		return errors.New("seed: outPath must not be empty")
	}
	if len(files) == 0 {
		return errors.New("seed: at least one file is required")
	}
	// ISO9660 directory entries are matched case-insensitively (the base
	// standard upper-cases names), so collapse case when detecting duplicates to
	// avoid writing two entries that the image cannot distinguish.
	seen := make(map[string]struct{}, len(files))
	for i, f := range files {
		if f.Name == "" {
			return fmt.Errorf("seed: files[%d].Name must not be empty", i)
		}
		if strings.ContainsRune(f.Name, '/') || strings.ContainsRune(f.Name, '\\') {
			return fmt.Errorf("seed: files[%d].Name %q must be a bare filename, not a path", i, f.Name)
		}
		// Reject "." and ".." which are not real filenames.
		if f.Name == "." || f.Name == ".." {
			return fmt.Errorf("seed: files[%d].Name %q is not a valid filename", i, f.Name)
		}
		// Reject NUL and other control characters: they are never valid in a
		// filename and a NUL would silently truncate the name in the image.
		if strings.IndexFunc(f.Name, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return fmt.Errorf("seed: files[%d].Name %q contains an invalid control character", i, f.Name)
		}
		key := strings.ToLower(f.Name)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("seed: duplicate file name %q (names are compared case-insensitively)", f.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// imageSize returns a pre-sized disk size large enough for the given content:
// the sum of file sizes plus generous ISO9660 overhead, floored at minImageSize
// and rounded up to a whole number of 2048-byte sectors.
func imageSize(files []File) int64 {
	var data int64
	for _, f := range files {
		// Each file occupies at least one sector; round up per file so many
		// small files still fit.
		data += roundUp(int64(len(f.Data)), sectorSize)
	}
	// Generous fixed overhead for the ISO9660 system area (16 sectors),
	// primary/terminator volume descriptors, the path table, the root
	// directory record, and Rock Ridge metadata.
	const overhead = 64 * sectorSize // 128 KiB
	total := data + overhead
	if total < minImageSize {
		total = minImageSize
	}
	return roundUp(total, sectorSize)
}

// roundUp rounds n up to the nearest positive multiple of mult. mult must be
// greater than zero.
func roundUp(n, mult int64) int64 {
	if mult <= 0 {
		return n
	}
	if n <= 0 {
		return mult
	}
	return ((n + mult - 1) / mult) * mult
}
