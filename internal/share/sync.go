package share

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/prozanne/v-container/internal/sshx"
)

// DefaultMtimeToleranceSec is the mtime slack used when comparing host and guest
// files. Cross-filesystem rounding (FAT/qcow2) and small clock skew make exact
// second equality unreliable.
const DefaultMtimeToleranceSec = 2

// Result summarizes a sync run.
type Result struct {
	Uploaded      int
	Downloaded    int
	DeletedLocal  int
	DeletedRemote int
	Conflicts     int
	ConflictPaths []string
}

// Sync performs one bidirectional reconciliation of localDir and remoteDir over
// SFTP, using manifestPath to remember the last-synced state (which is how it
// distinguishes "deleted" from "new"). It is safe to run repeatedly. Conflicts
// (both sides changed) are resolved last-writer-wins, and the losing version is
// preserved as a sibling ".vc-conflict-*" file so nothing is ever silently lost.
func Sync(ctx context.Context, client *sshx.Client, localDir, remoteDir, manifestPath string, tolSec int64) (Result, error) {
	var res Result
	if tolSec <= 0 {
		tolSec = DefaultMtimeToleranceSec
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return res, fmt.Errorf("create local dir %s: %w", localDir, err)
	}
	sc, err := client.SFTP()
	if err != nil {
		return res, err
	}
	defer sc.Close()
	if err := sc.MkdirAll(remoteDir); err != nil {
		return res, fmt.Errorf("create remote dir %s: %w", remoteDir, err)
	}

	local, err := scanLocal(localDir)
	if err != nil {
		return res, fmt.Errorf("scan local: %w", err)
	}
	remote, err := scanRemote(sc, remoteDir)
	if err != nil {
		return res, fmt.Errorf("scan remote: %w", err)
	}
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return res, fmt.Errorf("load manifest: %w", err)
	}

	actions := Plan(local, remote, manifest, tolSec)
	stamp := time.Now().Unix()

	for _, a := range actions {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		lpath := filepath.Join(localDir, filepath.FromSlash(a.Path))
		rpath := path.Join(remoteDir, a.Path)
		op := a.Op
		if op == OpConflict {
			res.Conflicts++
			res.ConflictPaths = append(res.ConflictPaths, a.Path)
			if err := backupConflictLoser(sc, a.Resolution, lpath, rpath, stamp); err != nil {
				return res, fmt.Errorf("preserve conflict loser for %s: %w", a.Path, err)
			}
			op = a.Resolution
		}
		switch op {
		case OpUpload:
			if err := sshx.UploadFile(sc, lpath, rpath); err != nil {
				return res, err
			}
			res.Uploaded++
		case OpDownload:
			if err := sshx.DownloadFile(sc, rpath, lpath); err != nil {
				return res, err
			}
			res.Downloaded++
		case OpDeleteLocal:
			if err := os.Remove(lpath); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("delete local %s: %w", lpath, err)
			}
			res.DeletedLocal++
		case OpDeleteRemote:
			if err := sc.Remove(rpath); err != nil && !isNotExist(err) {
				return res, fmt.Errorf("delete remote %s: %w", rpath, err)
			}
			res.DeletedRemote++
		}
	}

	// Rebuild the manifest from the post-sync local state (which now mirrors the
	// remote) so the next run has an accurate baseline.
	newManifest, err := scanLocal(localDir)
	if err != nil {
		return res, fmt.Errorf("rescan local: %w", err)
	}
	if err := saveManifest(manifestPath, newManifest); err != nil {
		return res, fmt.Errorf("save manifest: %w", err)
	}
	return res, nil
}

// backupConflictLoser copies the about-to-be-overwritten side to a sibling file
// on the host (always recoverable from Windows) before the winner overwrites it.
func backupConflictLoser(sc *sftp.Client, resolution Op, lpath, rpath string, stamp int64) error {
	switch resolution {
	case OpUpload: // host wins; guest's version is the loser
		dst := fmt.Sprintf("%s.vc-conflict-guest-%d", lpath, stamp)
		return sshx.DownloadFile(sc, rpath, dst)
	case OpDownload: // guest wins; host's version is the loser
		dst := fmt.Sprintf("%s.vc-conflict-host-%d", lpath, stamp)
		return copyLocalFile(lpath, dst)
	default:
		return nil
	}
}

func copyLocalFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// scanLocal walks a local directory into a Snapshot keyed by slash-relative
// path. Conflict-backup files (".vc-conflict-*") are excluded so they don't get
// synced into the guest.
func scanLocal(dir string) (Snapshot, error) {
	snap := Snapshot{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isIgnored(rel) {
			return nil
		}
		snap[rel] = Entry{Size: info.Size(), ModUnix: info.ModTime().Unix()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// scanRemote walks a remote directory into a Snapshot via SFTP.
func scanRemote(sc *sftp.Client, dir string) (Snapshot, error) {
	snap := Snapshot{}
	walker := sc.Walk(dir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return nil, err
		}
		info := walker.Stat()
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		rel := remoteRel(dir, walker.Path())
		if rel == "." || isIgnored(rel) {
			continue
		}
		snap[rel] = Entry{Size: info.Size(), ModUnix: info.ModTime().Unix()}
	}
	return snap, nil
}

func isIgnored(rel string) bool {
	base := path.Base(rel)
	return strings.Contains(base, ".vc-conflict-")
}

// remoteRel returns the slash path of full relative to base (POSIX remote paths).
func remoteRel(base, full string) string {
	base = strings.TrimSuffix(base, "/")
	if full == base {
		return "."
	}
	return strings.TrimPrefix(strings.TrimPrefix(full, base), "/")
}

func loadManifest(p string) (Snapshot, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, nil
		}
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		// A corrupt manifest is non-fatal: treat as empty (forces a full
		// reconcile rather than failing the sync).
		return Snapshot{}, nil
	}
	if snap == nil {
		snap = Snapshot{}
	}
	return snap, nil
}

func saveManifest(p string, snap Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// Marshal with sorted keys for stable diffs.
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]Entry, len(snap))
	for _, k := range keys {
		ordered[k] = snap[k]
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// isNotExist reports whether an SFTP error indicates a missing file.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "does not exist") ||
		strings.Contains(strings.ToLower(err.Error()), "no such file")
}
