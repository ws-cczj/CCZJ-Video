# Prepare a GitHub release for cczjVideo without typing the same text twice.
#
# CHANGELOG.md is the single source for the release notes: whatever section matches the
# version being shipped becomes the release body, gets copied to the clipboard, and is
# also written to dist/ so it survives a lost clipboard. The version is NOT a free
# parameter here -- it has to equal version.json, because that file both drives the
# version baked into the binary (via package.ps1 -> -X cczjVideo/app/updater.Version)
# and the update text every installed copy reads from raw.githubusercontent. Shipping a
# tag that disagrees with version.json makes the app announce a build you did not upload.
#
# This script never touches GitHub: it stages files, fills the browser form's tag and
# title, and leaves the upload and the publish button to you.
#
# ASCII only: PowerShell 5.1 mis-parses non-ASCII scripts that carry no BOM, which
# silently corrupts the following command line (same rule as package.ps1).
param(
    # Defaults to version.json. Passing a different value is an error on purpose.
    [string] $Version = "",
    # Release title shown on GitHub. Default is just the tag; write your own if you want.
    [string] $Title = "",
    [string] $OutDir = "dist",
    # Only regenerate the notes and the checklist. Skips the build, so bin/ is untouched.
    [switch] $SkipBuild,
    # Ship even if version.json's "desc" still looks like the previous release's text.
    [switch] $AllowStaleDesc,
    # Do not launch Explorer or the browser (used by automated runs).
    [switch] $NoOpen
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $root

function Step($label, [scriptblock] $body) {
    Write-Host "==> $label"
    & $body
    if ($LASTEXITCODE -ne 0) { throw "$label failed (exit $LASTEXITCODE)" }
}

# Returns the markdown body under "## [key]" up to the next "## " heading, or $null.
function Get-ChangeLogSection([string] $path, [string] $key) {
    if (-not (Test-Path -LiteralPath $path)) { return $null }
    $lines = [IO.File]::ReadAllLines($path, [Text.Encoding]::UTF8)
    $pattern = '^##\s+\[' + [regex]::Escape($key) + '\]'
    $body = @()
    $inside = $false
    foreach ($line in $lines) {
        if (-not $inside) {
            if ($line -match $pattern) { $inside = $true }
            continue
        }
        if ($line -match '^##\s') { break }
        $body += $line
    }
    if (-not $inside) { return $null }
    $text = ($body -join "`r`n").Trim()
    if ($text.Length -eq 0) { return $null }
    return $text
}

# ---------------------------------------------------------------- version
$jsonVersion = (& powershell -NoProfile -ExecutionPolicy Bypass -File build/get_version.ps1).Trim()
if (-not $jsonVersion) { throw "could not read a version from version.json" }
if (-not $Version) { $Version = $jsonVersion }
if ($Version -ne $jsonVersion) {
    throw "version.json says $jsonVersion but you asked to release $Version. Bump version.json (and its desc) first, then run again."
}
if (-not $Title) { $Title = "v$Version" }
$tag = "v$Version"
Write-Host "release: $tag  (version.json agrees)"

# ------------------------------------------------------- in-app update text
# version.json's "desc" is the text installed copies show in their update dialog, read
# from raw.githubusercontent. It is the one release input this script cannot write for
# you, and forgetting it is the quiet failure: the binary ships fine and every user is
# told the previous release's news. So stop instead of publishing.
$vj = [IO.File]::ReadAllText((Join-Path $root "version.json"), [Text.Encoding]::UTF8) | ConvertFrom-Json
$desc = "$($vj.desc)"
$prevDesc = ""
if ($vj.history -and $vj.history.Count -gt 0) { $prevDesc = "$($vj.history[0].desc)" }
# "dai tian" (to be filled) as a codepoint escape, so this file stays ASCII-only.
$placeholder = [regex]::Unescape('\u5f85\u586b')
if ($desc.Trim().Length -eq 0) { throw "version.json desc is empty - write the update text users will see." }
if ($desc -like "*$placeholder*" -or $desc -match '(?i)\bTODO\b') { throw "version.json desc is still a placeholder: $desc" }
if ($prevDesc -and $desc -eq $prevDesc) { throw "version.json desc is identical to history[0] ($($vj.history[0].version)) - it still describes the previous release." }
if ($desc -notlike "*$Version*" -and -not $AllowStaleDesc) {
    throw "version.json desc never mentions $Version. Pass -AllowStaleDesc if that is deliberate."
}

# ---------------------------------------------------------------- notes
$notes = Get-ChangeLogSection (Join-Path $root "CHANGELOG.md") $Version
$notesFrom = "## [$Version]"
if (-not $notes) {
    $notes = Get-ChangeLogSection (Join-Path $root "CHANGELOG.md") "Unreleased"
    $notesFrom = "## [Unreleased]"
}
if (-not $notes) {
    throw "CHANGELOG.md has neither a '## [$Version]' nor a '## [Unreleased]' section with content. Write the changes there first."
}

# ---------------------------------------------------------------- build
if (-not $SkipBuild) {
    # go build writes bin/cczjVideo.exe; a running app holds that file open and the
    # linker fails with "Access is denied" halfway through. Say so before building.
    $live = Get-Process -Name "cczjVideo" -ErrorAction SilentlyContinue
    if ($live) {
        throw "cczjVideo.exe is running (PID $(($live.Id) -join ', ')). Close it first -- this script will not kill it for you."
    }
    Step "wails3 task windows:package" { & wails3 task windows:package }
}

# ---------------------------------------------------------------- stage notes next to the artifacts
$distPath = if ([IO.Path]::IsPathRooted($OutDir)) { $OutDir } else { Join-Path $root $OutDir }
New-Item -ItemType Directory -Force -Path $distPath | Out-Null
$notesPath = Join-Path $distPath "release-notes-$tag.md"
[IO.File]::WriteAllText($notesPath, $notes + "`r`n", (New-Object Text.UTF8Encoding $false))

# Set-Clipboard can fail when another process owns the clipboard. Losing the paste is
# worth a warning, not an aborted release -- the file above is the fallback.
try {
    Set-Clipboard -Value $notes
    $clipboardState = "copied to the clipboard"
} catch {
    $clipboardState = "NOT on the clipboard ($($_.Exception.Message)); open $notesPath"
}

# ---------------------------------------------------------------- verify what to upload
$expected = @(
    "cczjVideo-windows-amd64-$Version.exe",
    "cczjVideo-windows-amd64-$Version-portable.zip",
    "checksums.txt"
)
Write-Host ""
Write-Host "upload these from $OutDir/ :"
foreach ($name in $expected) {
    $item = Get-Item -LiteralPath (Join-Path $distPath $name) -ErrorAction SilentlyContinue
    if ($item) {
        Write-Host ("  [ok]      {0,-46} {1,12:N0} bytes" -f $name, $item.Length)
    } else {
        $hint = if ($SkipBuild) { "not built yet (run without -SkipBuild)" } else { "MISSING - package.ps1 did not produce it" }
        Write-Host ("  [absent]  {0,-46} {1}" -f $name, $hint)
    }
}

# ---------------------------------------------------------------- checklist
Write-Host ""
Write-Host "GitHub form (already filled in the browser):"
Write-Host "  tag      $tag"
Write-Host "  target   main"
Write-Host "  title    $Title"
Write-Host "  notes    $clipboardState  (taken from $notesFrom)"
Write-Host ""
Write-Host "Still yours to do:"
Write-Host "  1. Commit version.json + CHANGELOG.md to main. The in-app update dialog reads"
Write-Host "     version.json from raw.githubusercontent, so an unpushed file means users see"
Write-Host "     the previous release's notes."
Write-Host "  2. version.json 'desc' is the update-dialog text. Rotating the old one into"
Write-Host "     'history' is done; the new prose is yours - this script stops while it still"
Write-Host "     reads as a placeholder or repeats the previous release."
Write-Host "  3. Tick 'Set as the latest release' unless this is a pre-release."
if (-not (Get-Item -LiteralPath (Join-Path $distPath $expected[2]) -ErrorAction SilentlyContinue)) {
    Write-Host "  4. NOTE: without checksums.txt the in-app updater refuses to install this"
    Write-Host "     release (app/updater/integrity.go fails closed). Build it if you skipped it."
}

# ---------------------------------------------------------------- open the two windows
if (-not $NoOpen) {
    Start-Process explorer.exe -ArgumentList "/select,`"$distPath`"" | Out-Null
    # Owner/repo come from the origin remote rather than a hardcoded string, so a fork or
    # a rename does not silently open a release form for the wrong repository.
    $remote = (& git config --get remote.origin.url)
    $m = [regex]::Match("$remote", 'github\.com[:/]+([^/]+/[^/]+?)(?:\.git)?/?$', 'IgnoreCase')
    if (-not $m.Success) {
        Write-Host "  browser  skipped: could not read a github owner/repo from 'origin' ($remote)"
    } else {
        $url = "https://github.com/{0}/releases/new?tag={1}&title={2}" -f `
            $m.Groups[1].Value, [URI]::EscapeDataString($tag), [URI]::EscapeDataString($Title)
        Start-Process $url | Out-Null
    }
}

Write-Host ""
Write-Host "done."
