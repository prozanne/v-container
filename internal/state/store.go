// Package state provides small, robust primitives for persisting data to
// disk: atomic JSON writes, JSON reads, and OS-level advisory file locks.
//
// It is deliberately domain-agnostic — it knows nothing about VMs or config —
// so it can be reused without creating import cycles. The atomic write
// (write-temp + fsync + rename) guarantees a reader never observes a
// partially written file, and the lock (released by the OS on process exit)
// serializes read-modify-write sequences across concurrent vc invocations.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// ErrNotFound is returned by ReadJSON when the target file does not exist.
var ErrNotFound = errors.New("state: file not found")

// WriteJSON marshals v as indented JSON and writes it to path atomically.
// The parent directory must already exist.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	return WriteFileAtomic(path, data, 0o644)
}

// ReadJSON reads and unmarshals JSON from path into v. It returns ErrNotFound
// (wrapped) if the file is missing, so callers can detect "no such record".
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// WriteFileAtomic writes data to path via a temp file in the same directory
// followed by an atomic rename. On Windows, rename-over-existing is handled by
// os.Rename (which maps to MoveFileEx with replace semantics on modern Go).
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	return nil
}

// Lock acquires an exclusive OS-level lock associated with lockPath, creating
// the lock file (and its parent directory) if needed. The returned unlock
// function must be called to release it; the OS also releases the lock if the
// process exits, so a crash never leaves a VM permanently locked.
func Lock(lockPath string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	fl := flock.New(lockPath)
	if err := fl.Lock(); err != nil {
		return nil, fmt.Errorf("lock %s: %w", lockPath, err)
	}
	return func() { _ = fl.Unlock() }, nil
}

// Exists reports whether a path exists on disk.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
