# v-container — Design Spec

**Date:** 2026-06-23
**Status:** Approved direction (research-validated)

## 1. Problem & Goal

A developer's corporate Windows environment blocks **WSL, Hyper-V, and Docker**. They need a
simple, reliable way to run a **headless Ubuntu Linux** environment on Windows and move files
between Windows and Linux easily — without admin rights and without those blocked technologies.

The deliverable is a **single CLI `.exe`** (`vc`) that works from any terminal and behaves like
a lightweight "Docker/Multipass for Linux on Windows": create a VM, get a shell, run commands,
share folders. Priorities, in order: **usability**, **stability**, **easy Windows↔Linux folder
movement**.

### Success criteria
- From a fresh machine: `vc launch` → a working Ubuntu shell in one command, zero manual steps.
- No admin rights, no WSL/Hyper-V/Docker, no Windows feature toggles required.
- Files move between Windows and Linux trivially (`vc cp`, an auto-synced shared workspace).
- Corporate HTTP/HTTPS proxy "just works" inside the guest, inherited from Windows.
- Robust: clear errors, safe re-runs, no state corruption, graceful degradation.

## 2. Technology Decisions (research-validated)

All decisions below were validated against primary sources and an adversarial verification pass
(see `2026-06-23-tech-research-brief.md`). Key facts were also confirmed empirically on a Linux
box: Go cross-compiles to a **pure-static Windows .exe (no DLLs)**; QEMU 8.2 boots an Ubuntu
cloud image under **TCG** (software emulation, no KVM/Hyper-V) successfully to systemd.

| Area | Decision | Rationale |
|---|---|---|
| **VM engine** | **QEMU** (`qemu-system-x86_64`), user-mode networking | Only viable way to run Linux on Windows without WSL/Hyper-V/Docker; runs unprivileged. |
| **Acceleration** | `-machine q35,accel=whpx:tcg` + `-accel tcg,thread=multi`; auto-detect & warn | WHPX needs the (blocked) hypervisor + admin → assume **TCG**. One command works either way. |
| **Provisioning** | **Cloud-image fast path** (default) + **ISO autoinstall** (secondary) | Cloud image boots in seconds via cloud-init (like Multipass); ISO path for arbitrary ISOs. |
| **First-boot config** | cloud-init **NoCloud** seed (label `CIDATA`, `user-data`+`meta-data`) | Standard, no manual install; configures user, SSH key, sudo, proxy, packages, growpart. |
| **Seed image** | **Pure-Go FAT/ISO9660 writer** (no external tools) | `genisoimage`/`mkisofs` absent on Windows; cloud-init accepts FAT or ISO9660 labeled `CIDATA`. |
| **Folder sharing** | **SFTP over SSH (pure Go)** is the robust default | Verified: virtio-9p / virtio-fs / built-in SMB are **not compiled on Windows hosts**. |
| **Networking** | slirp user-mode, `hostfwd=tcp:127.0.0.1:<port>-:22` | Unprivileged; **always loopback-bound** so SSH is never exposed on the corporate LAN. |
| **Proxy** | Inherit Windows per-user proxy (HKCU) → inject into guest via cloud-init | No admin; `127.0.0.1` proxy → rewrite to slirp gateway `10.0.2.2`; remote proxy passthrough. |
| **Distribution** | Ship **release ZIP = `vc.exe` + pruned `qemu\`**; `vc setup` can download QEMU | QEMU is too large to embed (~80–190 MB); bundling = zero first-run network. |
| **Auth** | Per-VM generated **ed25519** keypair; no passwords | Secure by default; keys kept in the per-user data dir. |

### Core library stack (minimal, pure-Go, no cgo)
- CLI: **`spf13/cobra`** (rich nested subcommands; the surface here justifies it)
- SSH/SFTP: **`golang.org/x/crypto/ssh`** + **`github.com/pkg/sftp`**
- Terminal: **`golang.org/x/term`** (raw mode / size), **`github.com/briandowns/spinner`**,
  **`github.com/schollz/progressbar/v3`**
- Windows proxy: **`github.com/mattn/go-ieproxy`**
- Seed image: a small in-repo ISO9660/FAT writer (avoid heavy external deps; pin if a lib is used)

## 3. CLI Surface (Docker/Multipass-inspired, intuitive)

```
vc                       Show status + helpful next steps (great first-run UX)
vc doctor                Diagnose environment: qemu? accel? proxy? ports? data dir?
vc setup                 Provision/locate QEMU (bundle | download via system proxy)
vc launch [NAME]         Create + start an Ubuntu VM (cloud-image fast path)
                           --image, --cpus, --mem, --disk, --mount HOST[:GUEST], --release
vc create NAME --iso P   Create a VM from an ISO via automated install (advanced)
vc ls                    List VMs: name, status, cpus/mem, ssh, mounts
vc start NAME            Start an existing VM (headless)
vc stop  NAME            Graceful ACPI shutdown (fallback: terminate)
vc restart NAME
vc shell [NAME]          Interactive SSH shell (default VM if exactly one)
vc exec  NAME -- CMD...  Run a command, stream stdout/stderr, propagate exit code
vc cp SRC DST            Copy files host↔guest (NAME:/path syntax), recursive, progress
vc sync NAME [--watch]   Bidirectional mirror of the shared workspace (SFTP)
vc mount NAME HOST [G]   Live shared folder (opt-in: host SFTP server + guest sshfs)
vc info  NAME            Detailed info (IP/port, key, mounts, disk, accel, proxy)
vc rm    NAME            Delete a VM (confirm; --force)
vc config ...            Global config (proxy mode, default cpus/mem/disk, paths)
vc version
```

**Usability rules:** if exactly one VM exists, `NAME` is optional for `shell/exec/stop/...`.
No-arg `vc` prints status + next step. Every error states the cause and the fix. Long ops show
spinners/progress. First run is guided.

## 4. Architecture (Go packages)

```
cmd/vc/main.go              entrypoint → cli.Execute()
internal/cli/               cobra commands (one file per command) + shared flag/IO helpers
internal/app/               wiring: resolve config, data dir, qemu, manager (composition root)
internal/vm/                VM model + Manager: create/start/stop/delete, lifecycle, status
internal/qemu/              cmdline builder, accel detection, process spawn/supervise, QMP monitor
internal/qemu/locate.go     find/provision qemu (bundle dir | data dir | PATH | download)
internal/image/             disk images: qcow2 overlay/create/resize; cloud-image cache+download
internal/cloudinit/         render user-data/meta-data (user, key, sudo, proxy, packages, mounts)
internal/seed/              pure-Go writer for the CIDATA NoCloud seed (ISO9660, FAT fallback)
internal/sshx/              ssh client, keygen, wait-for-ssh, interactive PTY shell, exec, sftp
internal/share/            Sharer iface: CopyEngine (cp), SyncEngine (sync), LiveMount (sshfs)
internal/netx/              free-port allocation, hostfwd rule building, loopback-bind verify
internal/proxy/             Windows proxy detection (ieproxy/registry) + guest propagation model
internal/state/             on-disk per-VM state store: atomic writes, file locking, JSON
internal/config/            global config file load/save/merge with flags+env
internal/platform/          OS-specific paths/dirs (windows vs linux dev), data-dir resolution
internal/ui/                tables, colors, spinners, prompts, consistent message formatting
```

**Design principles:** each package has one purpose and a small interface; `share.Sharer` and
`qemu.Accelerator` are interfaces so behavior is swappable and testable; no package reaches into
another's internals. Pure logic (cmdline build, cloud-init render, seed bytes, state, port pick,
proxy parse, path parse) is isolated from process/network I/O so it is unit-testable on Linux.

## 5. On-disk Layout

```
Data dir:  Windows %LOCALAPPDATA%\v-container   |   Linux $XDG_DATA_HOME/v-container
           (override with VC_HOME)
  config.json                      global config
  qemu\                            provisioned qemu (if downloaded); else use bundled/next-to-exe
  images\                          cached base cloud images + user ISOs (+ .sha256)
  vms\<name>\
    vm.json                        spec + runtime state (pid, ssh port, mac, mounts, created, accel)
    disk.qcow2                     per-VM overlay (backing = cached base image)
    seed.img                       cloud-init NoCloud seed (CIDATA)
    id_ed25519 / .pub              per-VM ssh keypair
    console.log                    serial console (postmortem/debug)
    qemu.pid, monitor.sock         process + QMP control
    workspace\                     default shared folder (host side) ↔ guest ~/workspace
```

## 6. Key Flows

### `vc launch` (default, cloud-image fast path)
1. Resolve/provision QEMU; `doctor`-style preflight (accel probe, data dir writable).
2. Ensure base cloud image cached (download via system proxy + SHA verify if missing; progress bar).
3. Create per-VM overlay qcow2 (`qemu-img create -f qcow2 -F qcow2 -b <base> disk.qcow2`); resize to `--disk`.
4. Generate ed25519 keypair.
5. Render cloud-init `user-data` (user `vmuser`, pubkey, NOPASSWD sudo, hostname, proxy env+apt+CA,
   packages, growpart, default workspace mount hint) + `meta-data` (instance-id, hostname).
6. Write CIDATA seed image (pure-Go).
7. Allocate a free loopback port; build hostfwd rule; verify loopback bind.
8. Spawn QEMU headless (`-nographic`, serial→console.log, QMP monitor, accel fallback).
9. Persist `vm.json` atomically.
10. Wait-for-SSH (poll with timeout + clear diagnostics on failure: show last console lines).
11. Print connection summary + next steps. Warn once if running under TCG.

### `vc shell` / `vc exec`
- SSH using the per-VM key. `shell`: request PTY, raw-mode local console, VT in/out, forward
  resize via `WindowChange`, restore terminal on exit. `exec`: stream output, propagate exit code.

### Folder sharing (headline)
- **`vc cp`** — pure-Go SFTP. `NAME:/path` ↔ Windows path, both directions, recursive, progress.
  Zero dependencies; always works (guest sshd only). The universal primitive.
- **Shared workspace + `vc sync`** — each VM has host `workspace\` ↔ guest `~/workspace`.
  `vc sync` mirrors bidirectionally over SFTP (mtime, last-writer-wins, manifest tracks deletes);
  `--watch` re-syncs on change (debounced). Auto-sync on shell enter/exit. **No guest packages
  beyond sshd** → maximally stable. This is the default "shared folder" experience.
- **`vc mount`** (opt-in, Phase 2) — true live FUSE share: host runs a pure-Go SFTP **server**
  bound to 127.0.0.1 serving the chosen Windows folder; guest mounts it via `sshfs` over the
  slirp gateway `10.0.2.2` (auto-installed via inherited proxy). Reconnect-resilient. Falls back
  to `sync` with a clear message if `sshfs` is unavailable.

### Proxy inheritance
- Detect Windows per-user proxy (HKCU Internet Settings via ieproxy): enabled?, server, override, PAC.
- Normalize: `127.0.0.1`/`localhost` → `10.0.2.2`; remote proxy → verbatim.
- Inject via cloud-init: `apt: {proxy,https_proxy}`, `/etc/environment` + `/etc/profile.d/proxy.sh`
  (`no_proxy=localhost,127.0.0.1,10.0.2.0/24,10.0.2.2,::1` + overrides), systemd drop-in, and
  optional corporate root CA via `ca_certs.trusted`. `--proxy auto|none|<URL>`.

## 7. Stability & Safety

- **Atomic state**: write temp + fsync + rename; per-VM file lock to prevent concurrent corruption.
- **Process supervision**: track pid; health = pid alive + SSH reachable; detect stale state.
- **Graceful degradation**: accel fallback (WHPX→TCG); share fallback (mount→sync→cp).
- **Serial console logging** for every VM (postmortem on boot failures).
- **Idempotent, re-runnable** operations; safe interrupts; clear, actionable errors.
- **Security**: per-VM keys, no passwords, SSH bound to loopback (verified at startup; refuse if 0.0.0.0).

## 8. Testing Strategy

- **Unit (run on Linux, the default CI):** cloud-init render (golden files), seed image bytes
  (parse back / structure asserts), QEMU cmdline builder (table tests across accel/share/proxy/net),
  free-port allocation, state store (atomic + locking + concurrent), config merge, proxy parse +
  normalization, `cp` path parsing, name validation.
- **Integration (run on Linux with the local extracted QEMU, build-tagged `integration`):**
  full `launch` of the cached Ubuntu cloud image under TCG → wait-for-SSH → `exec uname -a` →
  `cp` round-trip → `sync` round-trip (write host→see guest, write guest→see host) → `stop`.
  This validates the real boot→cloud-init→SSH→share pipeline; identical code runs on Windows.
- **Build gates:** `go vet`, `go build` for **windows/amd64** and **linux/amd64**, `go test ./...`.

## 9. Distribution

- `make` targets produce `dist/vc.exe` (windows/amd64, CGO off) and `dist/vc` (linux dev).
- Release ZIP: `vc.exe` + `qemu\` (pruned: `qemu-system-x86_64.exe` + required DLLs + firmware).
- `vc setup` provisions QEMU for users who only grabbed the exe (download via system proxy, SHA-verify).
- README: quick start, the TCG performance note, the bundled-QEMU/SmartScreen note, proxy behavior.

## 10. Phasing

- **Phase 1 (core, must be rock-solid):** config/state/platform, qemu locate + cmdline + spawn +
  QMP stop, image cache/overlay/resize, cloud-init + seed, sshx (keygen/wait/shell/exec/sftp),
  netx, `launch/ls/shell/exec/stop/start/rm/info/doctor/cp`, proxy inheritance, default workspace
  + `sync`. Unit + integration tests green; cross-compiles to Windows.
- **Phase 2:** `vc mount` live sshfs share; `vc create --iso` autoinstall; richer `config`;
  sshfs-win/WinFsp drive-mount detection; packaging polish + signing docs.

## Out of scope (YAGNI for v1)
GUI; non-Ubuntu distros; nested virt; GPU passthrough; multi-arch guests; clustering; a daemon.
Networking beyond user-mode slirp + port forwards. Windows-as-guest.
