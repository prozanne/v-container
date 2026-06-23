# Tech Research Brief — v-container (2026-06-23)

Research validated against primary sources (QEMU master source/docs, cloud-init/subiquity docs,
Ubuntu cloud-images) with an adversarial verification pass on the folder-sharing claim. Empirically
confirmed on a Linux box: Go → pure-static Windows `.exe` (no DLLs); QEMU 8.2 boots an Ubuntu cloud
image under **TCG** (no KVM/Hyper-V) to systemd.

## 1. Acceleration
- WHPX is the only HW accelerator on Windows; it needs the **same Type-1 hypervisor Hyper-V uses**
  running at boot, and enabling its feature needs **admin**. On a locked-down machine where the
  hypervisor is blocked, **WHPX cannot init** → assume **TCG**.
- HAXM is dead (removed in QEMU 8.2). TCG is ~8× slower but **usable headless**; MTTCG (one thread
  per vCPU) helps parallel work.
- One command works either way: `-machine q35,accel=whpx:tcg -accel tcg,thread=multi`. Detect the
  active accelerator and warn under TCG.

## 2. Provisioning
- **Default = cloud-image fast path** (like Multipass): download `noble-server-cloudimg-amd64.img`
  (24.04 LTS), `qemu-img resize` before first boot (growpart+resizefs auto-expand root), attach a
  **NoCloud seed** (label `CIDATA`, files `user-data`+`meta-data`), boot, poll SSH. Seconds, no install.
- **Secondary = ISO autoinstall (subiquity).** Two non-negotiables for zero-click: seed on a
  `CIDATA` volume **and** inject the literal `autoinstall` token + `ds=nocloud;s=/cdrom/<dir>/`
  (trailing slash) into the kernel cmdline — otherwise it stops at a confirmation prompt.
- Seed can be **FAT or ISO9660** labeled `CIDATA`. Generate it **in Go** (no `genisoimage`/`mcopy`
  on Windows). `meta-data` minimal: `instance-id` + `local-hostname`.
- Per-VM **ed25519** key; inject pubkey into `ssh_authorized_keys`; NOPASSWD sudo; no passwords.

## 3. Folder sharing — VERIFIED
- **Native QEMU sharing is unavailable on Windows hosts** (confirmed against QEMU master):
  - virtio-9p / `-fsdev local`: compiled only for linux/darwin/freebsd; Windows gets a dummy stub.
    The Windows 9p patch series (gitlab #974) was **never merged**; no `9p-util-win32.c` exists.
  - virtiofs/virtiofsd: Linux-only daemon. Cannot run on a Windows host.
  - QEMU "built-in" SMB (`-net user,smb=`): shells out to host `smbd` (Samba) — Unix-only.
- **Robust default = SFTP over QEMU `hostfwd`** with a **pure-Go** client (`pkg/sftp` over
  `x/crypto/ssh`). Needs **nothing installed on Windows**.
- **Optional richer modes:** (a) live FUSE share = host pure-Go SFTP **server** on 127.0.0.1 +
  guest **sshfs** over slirp gateway `10.0.2.2`; (b) `sshfs-win` + WinFsp drive letter
  (community-maintained, last release ~2020, needs WinFsp install — document as optional).

## 4. Networking & proxy
- slirp user-mode, **fully unprivileged**. SSH via `-netdev user,hostfwd=tcp:127.0.0.1:<port>-:22`
  — **always loopback-bound** (old slirp ignored bind addr, #1560/#1593; pin QEMU ≥ 8.x; verify
  listener is on 127.0.0.1, not 0.0.0.0). Topology: gw `10.0.2.2` (= host loopback), DNS `10.0.2.3`.
- Inherit Windows proxy from **HKCU Internet Settings** (no admin; via `go-ieproxy`). Normalize:
  `127.0.0.1`/`localhost` → `10.0.2.2`; remote proxy → verbatim. Inject via cloud-init: `apt`
  proxy, `/etc/environment` + `profile.d`, systemd drop-in, `no_proxy` (incl. `10.0.2.0/24`), and
  corporate root CA via `ca_certs.trusted` for TLS-intercepting proxies.

## 5. Distribution & libraries
- **Do not embed QEMU** (~80–190 MB). Ship a **release ZIP = `vc.exe` + pruned `qemu\`** (zero
  first-run network). Build the `qemu\` tree from the weilnetz NSIS installer (don't rely on
  7-Zip). Use OVMF/edk2 **4 MB** firmware; each VM gets its own writable VARS copy. Fallback:
  download-on-first-run into `%LOCALAPPDATA%`, pin + SHA-512 verify, route via system proxy.
  weilnetz binaries carry an **expired cert** (SmartScreen) — code-sign our exe; publish ZIP SHA.
- Libs: `spf13/cobra` (CLI), `x/crypto/ssh` + `pkg/sftp` (SSH/SFTP), `x/term`,
  `briandowns/spinner`, `schollz/progressbar/v3`, `mattn/go-ieproxy` (Windows proxy).
- Interactive shell on Windows: `term.MakeRaw` + VT in/out + `WindowChange` on resize; ConPTY not
  required to drive a remote SSH PTY.

## Top risks → mitigations
1. WHPX unavailable → TCG slow: one-command fallback; warn; document remote KVM offload.
2. Download/TLS-intercept restrictions: bundle everything; route via system proxy + inject CA; SHA-verify.
3. "Native share" expectations: locked to SFTP (verified); offer live-mount as upgrade; document.
4. SmartScreen/AV: code-sign exe; publish hashes; document bundled-QEMU cert.
5. hostfwd LAN exposure: always bind 127.0.0.1; verify at startup; per-VM keys, no passwords.

## Recommended default QEMU command
```
qemu-system-x86_64 -machine q35,accel=whpx:tcg -accel tcg,thread=multi -cpu max -smp 4 -m 8G \
  -nographic \
  -drive if=virtio,format=qcow2,file=disk.qcow2 \
  -drive if=virtio,format=raw,file=seed.img \
  -netdev user,id=net0,hostfwd=tcp:127.0.0.1:<port>-:22 \
  -device virtio-net-pci,netdev=net0
```
