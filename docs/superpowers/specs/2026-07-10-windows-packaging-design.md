# Windows Packaging & Release Automation — Design Spec

Date: 2026-07-10
Status: approved (autonomous goal session; decisions resolved from the product spec + measurements)

## 1. Goal

Running vc must be painless: install once, open any terminal, `vc launch` works. Concretely:

- A **per-user installer** that needs **no admin rights** (the whole point of v-container), bundles
  QEMU, and puts `vc` on PATH.
- A **portable zip** for machines where even user-level installers are blocked.
- Both built and attached to a GitHub Release **automatically when a `v*` tag is pushed**.

vc itself needs **zero code changes**: `qemu.Locate` already searches `<exe dir>/qemu/` second
(`internal/qemu/locate.go`), and `app.New` passes `platform.ExeDir()` (`internal/app/app.go:47`).
The installer only has to lay files out as `vc.exe` + `qemu\`.

## 2. Deliverables per release tag `vX.Y.Z`

| Asset | Contents |
|---|---|
| `vc-setup-X.Y.Z.exe` | Inno Setup per-user installer: `vc.exe` + pruned `qemu\`, PATH entry |
| `vc-X.Y.Z-windows-x86_64.zip` | The same tree, portable (unzip anywhere, optionally add to PATH) |
| `vc.exe` | Standalone binary, for users who already have QEMU or are upgrading vc only |
| `SHA256SUMS.txt` | Checksums of the above |

`cli.Version` is stamped from the tag via `-ldflags -X` (already wired; Makefile does the same).

## 3. Decisions (alternatives considered)

**Installer technology: Inno Setup 6.**
- Preinstalled on GitHub `windows-latest` runners (`ISCC.exe`), mature, tiny script.
- `PrivilegesRequired=lowest` → true per-user install to `%LOCALAPPDATA%\Programs\v-container`,
  no UAC prompt ever.
- Alternatives: NSIS (also preinstalled but cruder scripting, no built-in per-user story),
  MSIX (requires code signing and sideloading policy — exactly what corporate lockdown blocks),
  zip-only (not an installer; the ask was an installer).

**QEMU source: pinned qemu.weilnetz.de w64 installer.**
- The de-facto official Windows build (linked from qemu.org), one self-contained file.
- Pinned in `packaging/windows/get-qemu.ps1`: `qemu-w64-setup-20260501.exe` with its published
  SHA512. Upgrades are a deliberate two-line diff.
- Extracted with 7-Zip (preinstalled on runners); the NSIS exe is just an archive to 7z.
- Alternatives: MSYS2 `mingw-w64-qemu` (needs pacman + manual DLL closure assembly, version
  drifts with the repo), building QEMU from source (hours of CI for no benefit).

**Pruning policy: keep all root DLLs, drop other-architecture payloads.** Measured on the real
20260501 package (1.2 GB extracted):
- Keep: `qemu-system-x86_64.exe`, `qemu-img.exe`, all 114 root `*.dll` (141 MB), `COPYING`,
  `COPYING.LIB`, `share/*.bin`, `share/*.rom`, `share/keymaps/`.
- Drop: the other 60 `*.exe` (all-arch emulators, `*w.exe` GUI variants, qemu-ga/nbd/io…),
  `share/edk2-*` + `*.fd` (302 MB of non-x86/UEFI firmware), `share/{doc,man,icons,locale,
  applications,firmware,dtb}`, `lib/`, NSIS plugin dirs.
- Result: **173 MB installed / ~60 MB compressed**, 195 files — inside the 80–190 MB band the
  product spec budgeted.
- A PE import-closure prune could shave the DLL set further but risks missing `dlopen`'d
  libraries and re-breaking on every QEMU bump; rejected for fragility. The `*.bin` glob keeps a
  few KB-scale foreign boot ROMs; accepted for policy simplicity.
- QEMU finds firmware relative to its own exe (`qemu/share/`), so preserving the subtree shape
  keeps firmware discovery working with no `-L` needed.

**PATH handling:** installer appends the app dir to HKCU `Environment\Path` only if absent
(Pascal `NeedsAddPath` check), sets `ChangesEnvironment=yes` so Explorer broadcasts the change
(new terminals see it; open ones don't — README says so), and removes exactly that entry on
uninstall.

**Uninstall & user data:** uninstaller removes the program dir and PATH entry; the data dir
(`%LOCALAPPDATA%\v-container`: VM disks, keys, workspaces) is deliberately left — deleting VMs
silently is worse than leaving files. Documented in the README.

**No code signing.** No certificate exists; SmartScreen will warn on first run. Documented
("More info → Run anyway"). Signing is a future concern once there's a cert.

## 4. Release workflow (`.github/workflows/release.yml`)

Trigger: `push` on tags `v*`. Single `windows-latest` job (`permissions: contents: write`):

1. checkout, setup-go, `go test ./...` + `go vet ./...` — a failing suite **blocks the release**.
2. Build `vc.exe` with `VERSION=${tag#v}` ldflags.
3. `get-qemu.ps1`: download (actions/cache keyed on the pinned filename spares weilnetz),
   verify SHA512, extract, prune to `stage/qemu/`. Fails loudly if expected files are missing.
4. **Smoke on the staged tree (real Windows):** `stage/qemu/qemu-system-x86_64.exe --version`
   and `stage/vc.exe doctor`. Doctor runs `-accel help`, which loads the full DLL import set —
   this is a de-facto DLL-completeness check. (Runners have no WHPX; TCG fallback is expected
   and fine.)
5. `ISCC packaging/windows/vc.iss` → installer; `7z a` → portable zip; standalone `vc.exe`;
   `SHA256SUMS.txt`.
6. `softprops/action-gh-release@v2` attaches all four to the release for the tag.

## 5. CI workflow (`.github/workflows/ci.yml`)

Push/PR to main + feature branches: unit tests + vet on `ubuntu-latest` and `windows-latest`
(the product target runs the suite natively), plus the Windows cross-build on ubuntu. Keeps
release day boring.

## 6. Verification plan

- Locally (this box): YAML parses; unit suite still green; iss/ps1 reviewed against the
  extracted real package layout (done above — policy was derived from it).
- First `v0.1.0` tag push is the live end-to-end test: runner smoke (step 4) gates the release.
- Full guest-boot verification stays with the existing integration test + a manual run on a
  real Windows machine (WHPX path can only be proven there).

## 7. Out of scope

winget/scoop manifests, code signing, delta/auto-updates, DLL closure pruning, Windows-ARM64,
Linux/macOS packages.
