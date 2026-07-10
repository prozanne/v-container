# Downloads the pinned QEMU Windows build, verifies it, and prunes it to the
# x86_64-only subset vc ships (see docs/superpowers/specs/2026-07-10-windows-
# packaging-design.md for the policy and measurements).
#
#   pwsh packaging/windows/get-qemu.ps1 -OutDir stage/qemu
#
# To upgrade QEMU: bump $SetupName and $SetupSha512 from
# https://qemu.weilnetz.de/w64/ (the .sha512 file next to the installer).

[CmdletBinding()]
param(
    # Directory that receives the pruned tree (created; must not already contain one).
    [Parameter(Mandatory = $true)] [string]$OutDir,
    # Where the installer download and extraction live (kept for caching).
    [string]$WorkDir = 'build/qemu-work',
    # Pre-downloaded installer to use instead of downloading (e.g. from a CI cache).
    [string]$SetupExe = '',
    # 7-Zip executable; autodetected on Windows runners when empty.
    [string]$SevenZip = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$SetupName   = 'qemu-w64-setup-20260501.exe'
$SetupUrl    = "https://qemu.weilnetz.de/w64/$SetupName"
$SetupSha512 = '3d6b996bb904666f3b7ff62bed233b2d21dffe96f512af0a7151cfcc828bc5c8f9b62623cf2a9d363a3ae48111761f4630cce776eacb9b70d83e61e6ae50de47'

function Find-SevenZip {
    if ($SevenZip) { return $SevenZip }
    $candidates = @(
        (Join-Path $env:ProgramFiles '7-Zip/7z.exe'),
        '7z', '7zz'
    )
    foreach ($c in $candidates) {
        $cmd = Get-Command $c -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
    }
    throw '7-Zip not found; pass -SevenZip'
}

function Assert-Hash([string]$Path) {
    $actual = (Get-FileHash -Algorithm SHA512 -Path $Path).Hash
    if ($actual -ne $SetupSha512) {
        throw "SHA512 mismatch for ${Path}:`n  expected $SetupSha512`n  actual   $actual"
    }
}

$sevenZipExe = Find-SevenZip
New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null

# 1. Obtain + verify the installer.
if (-not $SetupExe) {
    $SetupExe = Join-Path $WorkDir $SetupName
    $haveGood = $false
    if (Test-Path $SetupExe) {
        try { Assert-Hash $SetupExe; $haveGood = $true; Write-Host "using cached $SetupExe" }
        catch { Remove-Item $SetupExe }
    }
    if (-not $haveGood) {
        Write-Host "downloading $SetupUrl"
        Invoke-WebRequest -Uri $SetupUrl -OutFile $SetupExe
    }
}
Assert-Hash $SetupExe
Write-Host "verified $SetupExe"

# 2. Extract (the NSIS installer is a plain archive to 7-Zip).
$extractDir = Join-Path $WorkDir 'extracted'
if (Test-Path $extractDir) { Remove-Item -Recurse -Force $extractDir }
& $sevenZipExe x -y "-o$extractDir" $SetupExe | Out-Null
if ($LASTEXITCODE -ne 0) { throw "7-Zip extraction failed (exit $LASTEXITCODE)" }

# 3. Prune into $OutDir. Keep policy (see design spec):
#    root:  qemu-system-x86_64.exe, qemu-img.exe, every DLL, the licenses
#    share: firmware blobs (*.bin, *.rom) and keymaps/
if (Test-Path $OutDir) { Remove-Item -Recurse -Force $OutDir }
$shareOut = Join-Path $OutDir 'share'
New-Item -ItemType Directory -Force -Path $shareOut | Out-Null

foreach ($name in 'qemu-system-x86_64.exe', 'qemu-img.exe', 'COPYING', 'COPYING.LIB') {
    Copy-Item (Join-Path $extractDir $name) $OutDir
}
Get-ChildItem -Path $extractDir -File -Filter '*.dll' | Copy-Item -Destination $OutDir

$shareIn = Join-Path $extractDir 'share'
Get-ChildItem -Path $shareIn -File |
    Where-Object { $_.Name -like '*.bin' -or $_.Name -like '*.rom' } |
    Copy-Item -Destination $shareOut
Copy-Item -Recurse (Join-Path $shareIn 'keymaps') $shareOut

# 4. Postconditions — fail the build rather than ship a broken tree.
$mustExist = @(
    'qemu-system-x86_64.exe', 'qemu-img.exe', 'COPYING',
    'share/bios-256k.bin', 'share/kvmvapic.bin', 'share/efi-virtio.rom'
)
foreach ($rel in $mustExist) {
    if (-not (Test-Path (Join-Path $OutDir $rel))) { throw "pruned tree is missing $rel" }
}
$dllCount = (Get-ChildItem -Path $OutDir -File -Filter '*.dll').Count
if ($dllCount -lt 50) { throw "suspiciously few DLLs ($dllCount) — did the package layout change?" }

$sizeMB = [math]::Round((Get-ChildItem -Recurse -File $OutDir | Measure-Object Length -Sum).Sum / 1MB)
Write-Host "pruned qemu tree ready: $OutDir ($dllCount DLLs, ${sizeMB} MB)"
