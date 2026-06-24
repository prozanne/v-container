// Package image manages VM disk images: creating per-VM qcow2 overlays backed
// by a shared base image, resizing them, and downloading + caching Ubuntu cloud
// images. Disk operations shell out to qemu-img (path supplied by the caller);
// the package never locates binaries itself.
package image

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Info is the subset of `qemu-img info` we care about.
type Info struct {
	Format      string `json:"format"`
	VirtualSize int64  `json:"virtual-size"`
	ActualSize  int64  `json:"actual-size"`
	BackingFile string `json:"backing-filename"`
}

// runQemuImg executes qemu-img with args and returns stdout, wrapping failures
// with the captured stderr for a useful message.
func runQemuImg(ctx context.Context, qemuImg string, args ...string) ([]byte, error) {
	if qemuImg == "" {
		return nil, fmt.Errorf("qemu-img path is empty")
	}
	cmd := exec.CommandContext(ctx, qemuImg, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("qemu-img %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// CreateOverlay creates a copy-on-write qcow2 overlay at overlayPath backed by
// basePath. The base is referenced by absolute path and is never modified, so a
// single cached cloud image safely backs many VMs.
func CreateOverlay(ctx context.Context, qemuImg, basePath, overlayPath string) error {
	if basePath == "" || overlayPath == "" {
		return fmt.Errorf("CreateOverlay: base and overlay paths are required")
	}
	_, err := runQemuImg(ctx, qemuImg,
		"create", "-q",
		"-f", "qcow2",
		"-F", "qcow2",
		"-b", basePath,
		overlayPath,
	)
	if err != nil {
		return fmt.Errorf("create overlay %s: %w", overlayPath, err)
	}
	return nil
}

// Resize grows a qcow2 image's virtual size to sizeGB GiB. Shrinking is refused
// by qemu-img unless forced; we only ever grow (cloud-init growpart expands the
// root filesystem on first boot to match).
func Resize(ctx context.Context, qemuImg, diskPath string, sizeGB int) error {
	if sizeGB <= 0 {
		return fmt.Errorf("resize: size must be positive, got %d", sizeGB)
	}
	_, err := runQemuImg(ctx, qemuImg, "resize", diskPath, fmt.Sprintf("%dG", sizeGB))
	if err != nil {
		return fmt.Errorf("resize %s to %dG: %w", diskPath, sizeGB, err)
	}
	return nil
}

// Stat returns `qemu-img info` for an image.
func Stat(ctx context.Context, qemuImg, path string) (Info, error) {
	out, err := runQemuImg(ctx, qemuImg, "info", "--output=json", path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(out, &info); err != nil {
		return Info{}, fmt.Errorf("parse qemu-img info for %s: %w", path, err)
	}
	return info, nil
}
