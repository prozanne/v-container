//go:build integration

// Integration test for the full VM pipeline: overlay → cloud-init seed → QEMU
// boot → SSH → exec → file copy → sync → stop. It is excluded from normal `go
// test` (build tag "integration") and requires a real QEMU plus an Ubuntu cloud
// image.
//
// Run it like:
//
//	export VC_TEST_IMAGE=/path/to/ubuntu-cloudimg.img
//	# ensure qemu-system-x86_64 and qemu-img are on PATH (and any LD_LIBRARY_PATH
//	# / QEMU_MODULE_DIR your build needs)
//	go test -tags integration -run TestIntegrationFullLifecycle -timeout 20m ./internal/vm/
package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prozanne/v-container/internal/config"
	"github.com/prozanne/v-container/internal/qemu"
	"github.com/prozanne/v-container/internal/share"
)

func TestIntegrationFullLifecycle(t *testing.T) {
	image := os.Getenv("VC_TEST_IMAGE")
	if image == "" {
		t.Skip("set VC_TEST_IMAGE to a cloud image to run the integration test")
	}

	dataDir := t.TempDir()
	tools, err := qemu.Locate(dataDir, "", "")
	if err != nil {
		t.Fatalf("locate qemu (put it on PATH): %v", err)
	}
	cfg := config.Defaults()
	cfg.ProxyMode = config.ProxyNone // don't inherit host proxy in the test
	m := NewManager(dataDir, cfg, tools, nil)

	ctx := context.Background()
	t.Log("creating + booting VM (TCG can take a few minutes)...")
	v, err := m.Create(ctx, CreateOptions{
		Name:         "itest",
		ImagePath:    image,
		CPUs:         2,
		MemMB:        2048,
		DiskGB:       6,
		ReadyTimeout: 12 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_ = m.Stop(context.Background(), v.Name, true)
	})

	if got := m.Status(v); got != StatusRunning {
		t.Fatalf("status = %s, want running", got)
	}

	client, err := m.Connect(ctx, v)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	// 1. exec
	out, _, exit, err := client.Run(ctx, "uname -a")
	if err != nil || exit != 0 {
		t.Fatalf("exec uname: exit=%d err=%v", exit, err)
	}
	if !strings.Contains(out, "Linux") {
		t.Fatalf("uname output unexpected: %q", out)
	}
	t.Logf("guest: %s", strings.TrimSpace(out))

	// 2. cp host -> guest -> verify
	localFile := filepath.Join(t.TempDir(), "hello.txt")
	const payload = "hello-from-host-12345"
	if err := os.WriteFile(localFile, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := share.Upload(ctx, client, localFile, "/tmp/hello.txt"); err != nil {
		t.Fatalf("upload: %v", err)
	}
	out, _, exit, err = client.Run(ctx, "cat /tmp/hello.txt")
	if err != nil || exit != 0 {
		t.Fatalf("cat uploaded file: exit=%d err=%v", exit, err)
	}
	if strings.TrimSpace(out) != payload {
		t.Fatalf("uploaded content mismatch: got %q want %q", out, payload)
	}

	// 3. cp guest -> host
	if _, _, _, err := client.Run(ctx, "echo guest-made > /tmp/back.txt"); err != nil {
		t.Fatal(err)
	}
	back := filepath.Join(t.TempDir(), "back.txt")
	if err := share.Download(ctx, client, "/tmp/back.txt", back); err != nil {
		t.Fatalf("download: %v", err)
	}
	data, err := os.ReadFile(back)
	if err != nil || !strings.Contains(string(data), "guest-made") {
		t.Fatalf("downloaded content mismatch: %q err=%v", string(data), err)
	}

	// 4. workspace sync round-trip (host -> guest)
	ws := v.WorkspaceDir(dataDir)
	if err := os.WriteFile(filepath.Join(ws, "note.txt"), []byte("sync-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	guestWS := "/home/" + v.User + "/workspace"
	manifest := filepath.Join(v.Dir(dataDir), "workspace.sync.json")
	if _, err := share.Sync(ctx, client, ws, guestWS, manifest, 0); err != nil {
		t.Fatalf("sync: %v", err)
	}
	out, _, _, err = client.Run(ctx, "cat "+guestWS+"/note.txt")
	if err != nil || strings.TrimSpace(out) != "sync-me" {
		t.Fatalf("synced file not in guest: got %q err=%v", out, err)
	}

	// 5. stop gracefully
	if err := m.Stop(ctx, v.Name, false); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := m.Status(v); got != StatusStopped {
		t.Fatalf("after stop status = %s, want stopped", got)
	}
}
