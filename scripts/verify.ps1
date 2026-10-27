param([switch]$StaticOnly)
$ErrorActionPreference = 'Stop'

node scripts/check-document-links.mjs
if ($LASTEXITCODE -ne 0) { Write-Error 'Maintained documentation contains missing local links.' }

node scripts/check-release-metadata.mjs (Get-Location).Path
if ($LASTEXITCODE -ne 0) {
    Write-Error 'Release documentation does not match version.json.'
}

# This file must stay ASCII-only: Windows PowerShell 5.1 reads BOM-less files with the
# ANSI (GBK on this machine) codepage, and UTF-8 Chinese comments then decode into
# sequences that eat the following newline, silently mangling the next command line.

# Check executable imports and browser storage access, not comment text.
if (!$StaticOnly) {
node --test scripts/check-architecture.test.mjs
if ($LASTEXITCODE -ne 0) { Write-Error 'Architecture checker regression tests failed.' }
node --test scripts/check-release-artifacts.test.mjs
if ($LASTEXITCODE -ne 0) { Write-Error 'Release artifact checker regression tests failed.' }
node --test scripts/publish-release.test.mjs
if ($LASTEXITCODE -ne 0) { Write-Error 'Release publication workflow regression tests failed.' }
}
node scripts/check-architecture.mjs (Get-Location).Path
if ($LASTEXITCODE -ne 0) { Write-Error 'Frontend dependency or storage check failed.' }

if (Test-Path 'frontend/vite.config.ts.timestamp-*.mjs') {
    Write-Error 'Generated Vite timestamp artifacts must not be present in the source tree.'
}
node scripts/gen-request-adapter.mjs --check
if ($LASTEXITCODE -ne 0) { Write-Error 'Request policies or generated request adapter are stale.' }

# Error classification uses types or codes (see AGENTS.md).
# Matching on err.Error() text is the silent kind of breakage: reword one message and a
# retry policy or a UI grouping stops firing with no compile error and no failing test.
# Comment lines are excluded: apperror's own doc comment spells out the forbidden call as
# the rule's wording, and matching that would fail the gate on the file that defines it.
# Use built-in scanning so a clean Windows runner needs no ripgrep installation.
$errorTextMatching = @(Get-ChildItem -LiteralPath app -Recurse -File -Filter '*.go' |
    Where-Object { $_.Name -notlike '*_test.go' } |
    Select-String -CaseSensitive -Pattern 'strings\.Contains\(\s*(err|e|lastErr)\.Error\(\)' |
    Where-Object { $_.Line -notmatch '^\s*//' })
if ($errorTextMatching) {
    Write-Error "Classify errors with apperror.CodeOf / errors.Is / errors.As, never by message text.`n$($errorTextMatching -join "`n")"
}

# The single-instance handoff wait must read a process snapshot, never spawn a console child.
# This app is built -H windowsgui, so Windows allocates a console window for every console
# subsystem program it starts: the old tasklist poll painted roughly 150 flashing black
# windows during one 30s wait (reported on the desktop 2026-10-06). Comment lines are
# excluded because procs_windows.go names the forbidden call while explaining why.
$handoffSubprocess = @(Get-ChildItem -LiteralPath app/handoff -Recurse -File -Filter '*.go' |
    Where-Object { $_.Name -notlike '*_test.go' } |
    Select-String -CaseSensitive -Pattern 'exec\.|\btasklist\b|os/exec' |
    Where-Object { $_.Line -notmatch '^\s*//' })
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

# Locale parity and literal references supplement type checking:
# a wrong key or class name passes vue-tsc and vite silently, so only this static check catches it.
node scripts/check-frontend-conventions.mjs (Get-Location).Path
if ($LASTEXITCODE -ne 0) {
    Write-Error 'Frontend convention check failed (i18n parity / locale keys / cczj-* classes).'
}
node scripts/gen-cczj-vscode-data.mjs --check
if ($LASTEXITCODE -ne 0) { Write-Error 'CCZJ CSS documentation or editor data is stale.' }
node scripts/audit-cczj-styles.mjs --check-reuse
if ($LASTEXITCODE -ne 0) { Write-Error 'Reuse CCZJ shared layout utilities instead of duplicating their declarations.' }

Push-Location frontend
try {
    if (!$StaticOnly) {
    npm run test
    if ($LASTEXITCODE -ne 0) {
        Write-Error 'Frontend regression tests failed.'
    }
    }
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

node scripts/audit-cczj-styles.mjs --check-build
if ($LASTEXITCODE -ne 0) { Write-Error 'Production CSS contains unexpanded CCZJ composition.' }

go vet ./...
if ($LASTEXITCODE -ne 0) {
    Write-Error 'go vet failed.'
}

if ($StaticOnly) {
go build ./app/...
if ($LASTEXITCODE -ne 0) { Write-Error 'Go application build failed.' }
} else {
go test -count=1 ./...
if ($LASTEXITCODE -ne 0) {
    Write-Error 'go test failed.'
}
}
