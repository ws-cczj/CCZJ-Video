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

$downloadInApp = rg -n '^func \(a \*App\) (runDownload|downloadDirect|downloadDirectSingle|downloadDirectParallel|downloadM3u8)' app/service/app.go
if ($downloadInApp) {
    Write-Error "Download transport implementation must stay in app/service/download.go.`n$($downloadInApp -join "`n")"
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

# Locales are untyped `export default {}` objects and cczj-* utilities are hand-written CSS:
# a wrong key or class name passes vue-tsc and vite silently, so only this static check catches it.
node scripts/check-frontend-conventions.mjs (Get-Location).Path
if ($LASTEXITCODE -ne 0) {
    Write-Error 'Frontend convention check failed (i18n parity / locale keys / cczj-* classes).'
}

go vet ./...
go test -count=1 ./...

Push-Location frontend
try {
    npm run build
}
finally {
    Pop-Location
}
