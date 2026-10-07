$ErrorActionPreference = 'Stop'

# This file must stay ASCII-only: Windows PowerShell 5.1 reads BOM-less files with the
# ANSI (GBK on this machine) codepage, and UTF-8 Chinese comments then decode into
# sequences that eat the following newline, silently mangling the next command line.

# Generated Wails bindings and runtime events must stay behind the frontend
# adapter seam. This makes accidental contract drift fail in CI immediately.
# rg spells the first path segment with the separator we passed and later segments with
# the native one, so normalize the whole line to '/' before testing; the whitelist check
# must not depend on how the search path happened to be written.
$directBindingImports = rg -n 'bindings/cczjVideo/app|@wailsio/runtime' frontend/src -g '*.ts' -g '*.vue' |
    Where-Object { $_.Replace('\', '/') -notmatch '^frontend/src/api/' }
if ($directBindingImports) {
    Write-Error "Frontend boundary violation: use frontend/src/api adapters.`n$($directBindingImports -join "`n")"
}

$directStorageAccess = rg -n 'localStorage\.' frontend/src -g '*.ts' -g '*.vue' |
    Where-Object { $_.Replace('\', '/') -notmatch '^frontend/src/platform/' }
if ($directStorageAccess) {
    Write-Error "Frontend storage violation: use frontend/src/platform/storage.ts.`n$($directStorageAccess -join "`n")"
}

$appLineCount = (Get-Content 'app.go').Count
if ($appLineCount -gt 600) {
    Write-Error "app.go must remain a composition root under 600 lines (actual: $appLineCount)."
}

# Same reasoning for the rest of the Go tree: a 1400-line file mixing fetch, anti-bot
# backoff, HTML parsing and scoring is the thing that made the douban rate limit hard to
# audit. 900 is the ceiling the tree currently sits under, so the guard only bites when a
# file grows past what can be reviewed in one sitting.
$oversizedGo = @(Get-ChildItem -Recurse -File -Filter '*.go' -Path app |
    Where-Object { $_.Name -notlike '*_test.go' } |
    ForEach-Object { [PSCustomObject]@{ Name = $_.FullName.Replace((Get-Location).Path + '\', ''); Lines = @(Get-Content $_.FullName).Count } } |
    Where-Object { $_.Lines -gt 900 })
if ($oversizedGo) {
    Write-Error "Split these Go files by responsibility (tests excluded):`n$(($oversizedGo | ForEach-Object { "  $($_.Name): $($_.Lines) lines" }) -join "`n")"
}

$downloadInApp = rg -n '^func \(a \*App\) (runDownload|downloadDirect|downloadDirectSingle|downloadDirectParallel|downloadM3u8)' app/service/app.go
if ($downloadInApp) {
    Write-Error "Download transport implementation must stay in app/service/download*.go.`n$($downloadInApp -join "`n")"
}

# Only main.go (entry) and app.go (composition root) may sit in the repo root, otherwise
# loose app_xxx.go files start growing back.
$looseRootGo = Get-ChildItem -File -Filter '*.go' | Where-Object { $_.Name -notin @('main.go', 'app.go') }
if ($looseRootGo) {
    Write-Error "Root must only contain main.go and app.go; move these into app/...`n$(($looseRootGo | ForEach-Object { $_.Name }) -join "`n")"
}

if (Test-Path 'frontend/vite.config.ts.timestamp-*.mjs') {
    Write-Error 'Generated Vite timestamp artifacts must not be present in the source tree.'
}

# Error classification goes through app/apperror codes (docs/adr/0009-coded-errors.md).
# Matching on err.Error() text is the silent kind of breakage: reword one message and a
# retry policy or a UI grouping stops firing with no compile error and no failing test.
# Comment lines are excluded: apperror's own doc comment spells out the forbidden call as
# the rule's wording, and matching that would fail the gate on the file that defines it.
$errorTextMatching = @(rg -n 'strings\.Contains\(\s*(err|e|lastErr)\.Error\(\)' app -g '*.go' -g '!*_test.go' |
    Where-Object { $_ -notmatch ':\s*//' })
if ($errorTextMatching) {
    Write-Error "Classify errors with apperror.CodeOf / errors.Is / errors.As, never by message text.`n$($errorTextMatching -join "`n")"
}

# The single-instance handoff wait must read a process snapshot, never spawn a console child.
# This app is built -H windowsgui, so Windows allocates a console window for every console
# subsystem program it starts: the old tasklist poll painted roughly 150 flashing black
# windows during one 30s wait (reported on the desktop 2026-10-06). Comment lines are
# excluded because procs_windows.go names the forbidden call while explaining why.
$handoffSubprocess = @(rg -n 'exec\.|\btasklist\b|os/exec' app/handoff -g '*.go' -g '!*_test.go' |
    Where-Object { $_ -notmatch ':\s*//' })
if ($handoffSubprocess) {
    Write-Error "app/handoff must not spawn subprocesses while waiting for the old instance; read the snapshot in procs_windows.go.`n$($handoffSubprocess -join "`n")"
}

# The Windows version resource has three silent failure modes that all still produce a
# buildable exe, so the template is checked here instead of only at release time:
#  - the "info" key is parsed by winres as a hex language ID; "0000" makes Win32 skip the
#    string table entirely, so Explorer reports the exe as having no version at all;
#  - FileVersion/ProductVersion exist twice (numeric "fixed" block and free-text strings)
#    and .NET's FileVersionInfo only ever reads the strings;
#  - a stale *.syso left in the root gets linked into every later build.
$winInfoPath = 'build/windows/info.json'
if (Test-Path $winInfoPath) {
    $winInfo = [IO.File]::ReadAllText((Join-Path (Get-Location).Path $winInfoPath), [Text.Encoding]::UTF8) | ConvertFrom-Json
    $langs = @($winInfo.info.PSObject.Properties.Name)
    if ($langs.Count -ne 1) {
        Write-Error "build/windows/info.json must declare exactly one language block (found: $($langs -join ', '))."
    }
    if ($langs[0] -notmatch '^[0-9A-Fa-f]{4}$' -or $langs[0] -eq '0000') {
        Write-Error "build/windows/info.json language key must be a real hex language ID such as 0409, not '$($langs[0])'."
    }
    foreach ($prop in @('FileVersion', 'ProductVersion')) {
        if (-not $winInfo.info.($langs[0]).$prop) {
            Write-Error "build/windows/info.json must declare the $prop string; FileVersionInfo reads the string, not the fixed block."
        }
    }
    foreach ($prop in @('file_version', 'product_version')) {
        if (-not $winInfo.fixed.$prop) {
            Write-Error "build/windows/info.json fixed block must declare $prop."
        }
    }
}

$staleSyso = @(Get-ChildItem -File -Filter '*.syso' -ErrorAction SilentlyContinue)
if ($staleSyso) {
    Write-Error "Generated resource files must not be committed or left in the root: $(($staleSyso | ForEach-Object { $_.Name }) -join ', ')"
}

# Locales are untyped `export default {}` objects and cczj-* utilities are hand-written CSS:
# a wrong key or class name passes vue-tsc and vite silently, so only this static check catches it.
node scripts/check-frontend-conventions.mjs (Get-Location).Path
if ($LASTEXITCODE -ne 0) {
    Write-Error 'Frontend convention check failed (i18n parity / locale keys / cczj-* classes / inline style size).'
}

go vet ./...
if ($LASTEXITCODE -ne 0) {
    Write-Error 'go vet failed.'
}

go test -count=1 ./...
if ($LASTEXITCODE -ne 0) {
    Write-Error 'go test failed.'
}

Push-Location frontend
try {
    # Native commands do not fail a PowerShell script by themselves, and vue-tsc errors only
    # show up in the exit code. Without this the whole script used to exit 0 on a broken build.
    npm run build
    if ($LASTEXITCODE -ne 0) {
        Write-Error 'Frontend build failed (vue-tsc or vite).'
    }
}
finally {
    Pop-Location
}
