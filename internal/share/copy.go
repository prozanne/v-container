// Package share moves files between the Windows host and the Linux guest over
// SFTP — the robust, dependency-free sharing mechanism (verified that QEMU's
// native 9p/virtio-fs/SMB are unavailable on Windows hosts). It provides
// one-shot copy (cp) and a bidirectional workspace mirror (sync).
package share

import (
	"context"
	"fmt"
	"os"

	"github.com/pkg/sftp"
	"github.com/prozanne/v-container/internal/sshx"
)

// Upload copies a local file or directory tree to the guest. Directories are
// detected automatically and copied recursively.
func Upload(ctx context.Context, client *sshx.Client, localPath, remotePath string) error {
	sc, err := client.SFTP()
	if err != nil {
		return err
	}
	defer sc.Close()
	return uploadAuto(sc, localPath, remotePath)
}

// Download copies a remote file or directory tree from the guest to the host.
func Download(ctx context.Context, client *sshx.Client, remotePath, localPath string) error {
	sc, err := client.SFTP()
	if err != nil {
		return err
	}
	defer sc.Close()
	info, err := sc.Stat(remotePath)
	if err != nil {
		return fmt.Errorf("stat remote %s: %w", remotePath, err)
	}
	if info.IsDir() {
		return sshx.DownloadTree(sc, remotePath, localPath)
	}
	return sshx.DownloadFile(sc, remotePath, localPath)
}

func uploadAuto(sc *sftp.Client, localPath, remotePath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat local %s: %w", localPath, err)
	}
	if info.IsDir() {
		return sshx.UploadTree(sc, localPath, remotePath)
	}
	return sshx.UploadFile(sc, localPath, remotePath)
}
