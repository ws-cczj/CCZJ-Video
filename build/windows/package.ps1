# Build a shippable Windows release of cczjVideo without needing NSIS.
#
# The chain is deliberately long because each stage fails silently:
# appicon.png -> icon.ico -> .syso (icon + manifest + version resource) ->
# `go build` links the .syso -> read the version back out of the produced .exe.
# A missing or malformed .syso still produces a working binary, just one that
# Explorer reports as having no version and no icon, so the last step verifies
# the resource instead of trusting that the earlier steps ran.
#
# ASCII only: PowerShell 5.1 mis-parses scripts that contain non-ASCII text
# without a BOM, which silently corrupts the following command line.
param(
    [string] $Version = "",
    [string] $Arch = "amd64",
    [string] $OutDir = "",
    [string] $BinOut = "",
    [switch] $AllowStaleFrontend
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $root

function Step($label, [scriptblock] $body) {
    Write-Host "==> $label"
    & $body
    if ($LASTEXITCODE -ne 0) { throw "$label failed (exit $LASTEXITCODE)" }
}

if (-not $Version) {
    $Version = & powershell -NoProfile -ExecutionPolicy Bypass -File build/get_version.ps1
}
if (-not $Version) { throw "could not read a version from version.json" }
Write-Host "release version: $Version (windows/$Arch)"

$locations = Get-Content -LiteralPath (Join-Path $root "build/artifact-paths.json") -Raw | ConvertFrom-Json
if (!$OutDir) { $OutDir = "$($locations.packages)/windows/v$Version/$Arch" }
$distPath = if ([IO.Path]::IsPathRooted($OutDir)) { $OutDir } else { Join-Path $root $OutDir }
New-Item -ItemType Directory -Force -Path $distPath | Out-Null

# The updater prefers the bare EXE and selects assets by platform and arch.
$exeAsset = "cczjVideo-windows-$Arch-$Version.exe"
$zipAsset = "cczjVideo-windows-$Arch-$Version-portable.zip"
$exeTarget = Join-Path $distPath $exeAsset
$zipTarget = Join-Path $distPath $zipAsset

Step "generate embedded JSON assets" { & node scripts/compress-embedded-json.mjs }

# go build embeds frontend/dist. An out-of-date dist yields a binary that
# silently ships the previous UI, so compare it against the sources.
$distIndex = Join-Path $root "frontend/dist/index.html"
if (-not (Test-Path $distIndex)) {
    throw "frontend/dist is missing. Run: wails3 task build:frontend"
}
if (-not $AllowStaleFrontend) {
    $newestSrc = (Get-ChildItem -Path (Join-Path $root "frontend/src") -Recurse -File |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1)
    if ($newestSrc -and $newestSrc.LastWriteTime -gt (Get-Item $distIndex).LastWriteTime) {
        throw "frontend/dist is older than $($newestSrc.Name). Run: wails3 task build:frontend (or pass -AllowStaleFrontend)"
    }
}

$appicon = Join-Path $root "build/appicon.png"
if (-not (Test-Path $appicon)) {
    Step "derive build/appicon.png from icon.png" {
        & powershell -NoProfile -ExecutionPolicy Bypass -File scripts/make_appicon.ps1
    }
}

Step "generate icon.ico" {
    & wails3 generate icons -input build/appicon.png -windowsfilename build/windows/icon.ico
}

Step "stamp version into info.generated.json" {
    & powershell -NoProfile -ExecutionPolicy Bypass -File build/windows/sync_info.ps1 -Version $Version
}

# The .syso has to sit next to the main package (the repo root here) for the Go
# linker to pick it up, and it must not linger afterwards: a stale copy would
# get linked into every later build, including debug ones.
$sysoName = "wails_windows_$Arch.syso"
$sysoPath = Join-Path $root $sysoName
$temporaryOutput = !$BinOut
if ($temporaryOutput) {
    $BinOut = Join-Path $distPath (".cczj-build-" + [Guid]::NewGuid().ToString("N") + ".exe")
}
try {
    Step "generate $sysoName" {
        & wails3 generate syso `
            -arch $Arch `
            -icon build/windows/icon.ico `
            -manifest build/windows/wails.exe.manifest `
            -info build/windows/info.generated.json `
            -out $sysoName
    }
    if (-not (Test-Path $sysoPath)) { throw "$sysoName was not generated" }

    Step "go build -> $BinOut" {
        & go build -tags production `
            -ldflags="-w -s -H windowsgui -X cczjVideo/app/updater.Version=$Version" `
            -o $BinOut
    }
    Step "verify the version resource" {
        & powershell -NoProfile -ExecutionPolicy Bypass -File build/windows/check_exe.ps1 `
            -Path $BinOut -ExpectVersion $Version
    }
    if ((Resolve-Path -LiteralPath $BinOut).Path -ne [IO.Path]::GetFullPath($exeTarget)) {
        Step "stage $exeAsset" { Copy-Item -LiteralPath $BinOut -Destination $exeTarget -Force }
    }
} finally {
    Remove-Item -LiteralPath $sysoPath -ErrorAction SilentlyContinue
    if ($temporaryOutput) { Remove-Item -LiteralPath $BinOut -ErrorAction SilentlyContinue }
}

Step "stage $zipAsset" {
    Compress-Archive -LiteralPath $exeTarget -DestinationPath $zipTarget -CompressionLevel Optimal -Force
}

$checksumLines = @()
foreach ($asset in @($exeAsset, $zipAsset)) {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $distPath $asset)).Hash.ToLower()
    # Two spaces: the GNU coreutils format, so `sha256sum -c checksums.txt` works.
    $checksumLines += "$hash  $asset"
}
$checksumsPath = Join-Path $distPath "checksums.txt"
[IO.File]::WriteAllText($checksumsPath, (($checksumLines -join "`r`n") + "`r`n"), (New-Object Text.UTF8Encoding $false))

Write-Host ""
Write-Host "artifacts in $OutDir/"
foreach ($f in @($exeAsset, $zipAsset, "checksums.txt")) {
    $item = Get-Item (Join-Path $distPath $f)
    Write-Host ("  {0,-44} {1,10:N0} bytes" -f $f, $item.Length)
}
Write-Host ""
Write-Host "Upload all three files to the GitHub release. The bare .exe is the asset the"
Write-Host "in-app updater picks (findBestAsset needs a platform token in the name) and"
Write-Host "hot-swaps; the zip is for first-time installs."
