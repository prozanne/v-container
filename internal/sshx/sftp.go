package sshx

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
)

// SFTP opens an SFTP subsystem over the existing SSH connection. The caller
// owns the returned client and must Close it.
func (c *Client) SFTP() (*sftp.Client, error) {
	sc, err := sftp.NewClient(c.Client)
	if err != nil {
		return nil, fmt.Errorf("start sftp subsystem: %w", err)
	}
	return sc, nil
}

// UploadFile copies a local file to a remote path, creating remote parent
// directories and preserving the file mode.
func UploadFile(sc *sftp.Client, localPath, remotePath string) error {
	in, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local %s: %w", localPath, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat local %s: %w", localPath, err)
	}
	if err := sc.MkdirAll(path.Dir(remotePath)); err != nil {
		return fmt.Errorf("mkdir remote %s: %w", path.Dir(remotePath), err)
	}
	out, err := sc.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote %s: %w", remotePath, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy to remote %s: %w", remotePath, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close remote %s: %w", remotePath, err)
	}
	if err := sc.Chmod(remotePath, info.Mode().Perm()); err != nil {
		// Non-fatal: some servers/filesystems reject chmod; keep the data.
		return nil
	}
	return nil
}

// DownloadFile copies a remote file to a local path, creating local parent
// directories and preserving the file mode.
func DownloadFile(sc *sftp.Client, remotePath, localPath string) error {
	in, err := sc.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote %s: %w", remotePath, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat remote %s: %w", remotePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("mkdir local %s: %w", filepath.Dir(localPath), err)
	}
	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local %s: %w", localPath, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy from remote %s: %w", remotePath, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close local %s: %w", localPath, err)
	}
	_ = os.Chmod(localPath, info.Mode().Perm())
	return nil
}

// UploadTree recursively uploads a local directory tree to a remote directory.
func UploadTree(sc *sftp.Client, localDir, remoteDir string) error {
	localDir = filepath.Clean(localDir)
	return filepath.Walk(localDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localDir, p)
		if err != nil {
			return err
		}
		remote := joinRemote(remoteDir, rel)
		switch {
		case info.IsDir():
			if err := sc.MkdirAll(remote); err != nil {
				return fmt.Errorf("mkdir remote %s: %w", remote, err)
			}
			return sc.Chmod(remote, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			return nil // skip symlinks for safety/portability
		default:
			return UploadFile(sc, p, remote)
		}
	})
}

// DownloadTree recursively downloads a remote directory tree to a local dir.
func DownloadTree(sc *sftp.Client, remoteDir, localDir string) error {
	walker := sc.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return fmt.Errorf("walk remote %s: %w", remoteDir, err)
		}
		info := walker.Stat()
		rel := remoteRel(remoteDir, walker.Path())
		local := filepath.Join(localDir, filepath.FromSlash(rel))
		switch {
		case info.IsDir():
			if err := os.MkdirAll(local, 0o755); err != nil {
				return fmt.Errorf("mkdir local %s: %w", local, err)
			}
		case info.Mode()&os.ModeSymlink != 0:
			continue
		default:
			if err := DownloadFile(sc, walker.Path(), local); err != nil {
				return err
			}
		}
	}
	return nil
}

// joinRemote joins a remote base with a (possibly OS-specific) relative path,
// always producing forward-slash POSIX paths for the guest.
func joinRemote(base, rel string) string {
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return base
	}
	return path.Join(base, rel)
}

// remoteRel returns the slash path of full relative to base.
func remoteRel(base, full string) string {
	base = strings.TrimSuffix(base, "/")
	if full == base {
		return "."
	}
	return strings.TrimPrefix(strings.TrimPrefix(full, base), "/")
}
