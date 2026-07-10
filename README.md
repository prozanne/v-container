# v-container (`vc`)

Run headless **Ubuntu Linux** virtual machines on **Windows** from a single `.exe` —
**no WSL, no Hyper-V, no Docker, no admin rights.** Built for locked-down corporate machines
where those are blocked.

It works like a tiny Multipass/Docker for Linux: launch a VM, get a shell, run commands, and move
files between Windows and Linux through a shared workspace. Under the hood it drives **QEMU** with
user-mode networking and cloud-init.

```text
vc launch dev            # create + boot an Ubuntu VM (cloud image, cloud-init)
vc shell dev             # interactive SSH shell
vc exec dev -- uname -a  # run a command
vc cp report.txt dev:/tmp/   # copy a file in
vc cp dev:/var/log/syslog .  # copy a file out
vc sync dev --watch      # keep the shared workspace mirrored both ways
vc stop dev              # graceful ACPI shutdown
vc ls                    # list VMs
```

## Why it exists

WSL, Hyper-V and Docker are commonly blocked or unavailable on corporate Windows. QEMU is a plain
user-space program: it runs **without admin rights and without any Windows virtualization feature**.
When the Windows Hypervisor Platform (WHPX) is available it is used automatically; otherwise QEMU
falls back to software emulation (TCG) — slower, but it always works.

## Features

- **One exe, any terminal.** Pure-Go binary, no runtime dependencies.
- **Zero-click Ubuntu.** Boots official Ubuntu **cloud images** and configures them with cloud-init
  (user, SSH key, passwordless sudo) — no installer, ready in seconds (with acceleration).
- **Easy file movement.** `vc cp` for one-off copies and a bidirectional **shared workspace**
  (`vc sync`) — all over SFTP, so nothing extra is needed on Windows.
- **Inherits your Windows proxy.** The per-user system proxy (and corporate CA) is detected and
  injected into the guest automatically — apt and tools "just work".
- **Safe by default.** Per-VM SSH keys (no passwords); SSH is bound to `127.0.0.1` only, never the
  LAN.
- **Stable.** Atomic state writes, per-VM locks, graceful shutdown with escalation, serial console
  logs for diagnosis.

## Install

Grab the latest release from GitHub Releases — three flavors, all admin-free:

- **`vc-setup-<version>.exe` (recommended)** — per-user installer: no admin rights, no UAC
  prompt. Installs `vc.exe` plus a bundled QEMU to `%LOCALAPPDATA%\Programs\v-container` and adds
  it to your user `PATH`. Open a **new** terminal afterwards and run `vc launch dev`.
- **`vc-<version>-windows-x86_64.zip`** — the same tree, portable: unzip anywhere and run
  `.\vc.exe` from that folder (or add it to `PATH` yourself). For machines where even user-level
  installers are blocked.
- **`vc.exe`** — standalone binary, if you already have QEMU or are only upgrading vc.

Windows SmartScreen may warn once about the unsigned binaries — choose "More info → Run anyway".
Uninstalling removes the program and its `PATH` entry but deliberately leaves your VMs
(`%LOCALAPPDATA%\v-container`); delete that folder too if you want them gone.

Releases are built and attached automatically when a `v*` tag is pushed
(`.github/workflows/release.yml`).

### Providing QEMU yourself

The installer and the zip already bundle QEMU (x86_64-only, ~170 MB unpacked). If you use the
standalone `vc.exe` instead, it looks for `qemu-system-x86_64` and `qemu-img` in this order:

1. a `qemu/` folder next to `vc.exe` (the **bundle** layout),
2. a directory you set with `vc setup --qemu-dir <path>` (saved to config),
3. your `PATH`.

Windows QEMU builds: <https://qemu.weilnetz.de/w64/> (linked from qemu.org). Run `vc setup` for
guidance.

## Usage

| Command | Description |
|---|---|
| `vc launch [name]` | Create and start a VM. Flags: `--cpus --mem --disk --release --image --mount`. |
| `vc ls` | List VMs and their status. |
| `vc shell [name]` | Interactive SSH shell (name optional if only one VM). |
| `vc exec <name> -- <cmd>` | Run a command and stream output. |
| `vc cp SRC DST` | Copy files; use `name:/path` for the guest side. |
| `vc sync [name] [--watch]` | Bidirectionally mirror the shared workspace. |
| `vc start/stop/restart [name]` | Power control (`stop --force` to kill). |
| `vc rm [name]` | Delete a VM and its data. |
| `vc info [name]` | Detailed VM info. |
| `vc doctor` | Diagnose QEMU, acceleration and proxy. |
| `vc config [set k v]` | Show/change defaults. |
| `vc setup` | Locate/configure QEMU. |

### Sharing folders

Every VM gets a **workspace** folder on the host that mirrors to `~/workspace` in the guest:

```text
vc sync dev            # mirror once (host ⇄ guest)
vc sync dev --watch    # keep mirroring as files change
```

Conflicts (a file changed on both sides) are resolved newest-wins, and the losing copy is kept as a
`*.vc-conflict-*` sibling so nothing is ever lost. For one-off transfers use `vc cp`.

### Proxy

By default `vc` inherits the Windows per-user proxy and injects it into the guest (apt proxy,
environment variables, systemd, and the corporate root CA for TLS-intercepting proxies). Control it
with `vc config set proxy auto|none`.

## Performance note

If the Windows Hypervisor Platform isn't available (common on locked-down machines), VMs run under
**TCG software emulation** — correct but noticeably slower. `vc doctor` tells you which mode is
active. For heavy builds, consider a remote KVM/cloud box.

## Building

```sh
make build        # build dist/vc.exe (windows/amd64) and dist/vc (host)
make test         # unit tests
make vet          # go vet
```

The integration test boots a real VM and is opt-in:

```sh
export VC_TEST_IMAGE=/path/to/ubuntu-cloudimg.img   # ensure qemu is on PATH
go test -tags integration -timeout 20m ./internal/vm/
```

## How it works

```text
vc ──> QEMU (qemu-system-x86_64, user-mode net, -nographic)
        ├─ disk.qcow2     per-VM CoW overlay on a cached Ubuntu cloud image
        ├─ seed.img       cloud-init NoCloud seed (user, SSH key, proxy, growpart)
        ├─ hostfwd        127.0.0.1:<port> → guest:22  (SSH/SFTP)
        └─ QMP            127.0.0.1:<port>             (graceful shutdown)
```

Files are shared over SFTP because QEMU's native 9p/virtio-fs/SMB sharing is not available on
Windows hosts. See `docs/superpowers/specs/` for the full design and research.

## License

TBD.
