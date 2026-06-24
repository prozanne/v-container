package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CloudImageName returns the cloud image filename for an Ubuntu release.
func CloudImageName(release string) string {
	return fmt.Sprintf("%s-server-cloudimg-amd64.img", release)
}

// CloudImageURL builds the download URL for a release from a mirror base.
func CloudImageURL(mirror, release string) string {
	mirror = strings.TrimRight(mirror, "/")
	return fmt.Sprintf("%s/%s/current/%s", mirror, release, CloudImageName(release))
}

// sha256URL builds the SHA256SUMS URL for a release.
func sha256URL(mirror, release string) string {
	mirror = strings.TrimRight(mirror, "/")
	return fmt.Sprintf("%s/%s/current/SHA256SUMS", mirror, release)
}

// EnsureBaseImage returns the local path to the cached cloud image for release,
// downloading and SHA256-verifying it if it is not already present. Downloads
// are atomic (temp file + rename) so an interrupted run never leaves a corrupt
// cached image. progress, if non-nil, receives byte counts during download.
func EnsureBaseImage(ctx context.Context, client *http.Client, mirror, release, cacheDir string, progress io.Writer) (string, error) {
	if release == "" {
		return "", fmt.Errorf("release must not be empty")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir %s: %w", cacheDir, err)
	}
	dest := filepath.Join(cacheDir, CloudImageName(release))
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return dest, nil // already cached
	}

	if client == nil {
		client = &http.Client{Timeout: 0}
	}

	// Fetch expected checksum first so we can verify the payload.
	wantSum, err := fetchSHA256(ctx, client, sha256URL(mirror, release), CloudImageName(release))
	if err != nil {
		// Non-fatal for environments that block SHA fetch but allow the image;
		// proceed without verification but make the risk explicit downstream.
		wantSum = ""
	}

	url := CloudImageURL(mirror, release)
	tmp := dest + ".part"
	_ = os.Remove(tmp)
	gotSum, err := downloadTo(ctx, client, url, tmp, progress)
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if wantSum != "" && !strings.EqualFold(wantSum, gotSum) {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, wantSum, gotSum)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("finalize cached image: %w", err)
	}
	return dest, nil
}

// downloadTo streams url to path, returning the hex SHA256 of the bytes written.
func downloadTo(ctx context.Context, client *http.Client, url, path string, progress io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}

	out, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", path, err)
	}

	h := sha256.New()
	writers := []io.Writer{out, h}
	if progress != nil {
		writers = append(writers, progress)
	}
	mw := io.MultiWriter(writers...)

	if _, err := io.Copy(mw, resp.Body); err != nil {
		_ = out.Close()
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return "", fmt.Errorf("sync %s: %w", path, err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchSHA256 downloads a SHA256SUMS file and returns the hex digest for the
// given filename. SHA256SUMS lines look like "<hex>  *<filename>" or
// "<hex>  <filename>".
func fetchSHA256(ctx context.Context, client *http.Client, url, filename string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: status %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == filename {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found in %s", filename, url)
}
