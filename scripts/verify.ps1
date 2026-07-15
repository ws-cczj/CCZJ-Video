$ErrorActionPreference = 'Stop'

# Generated Wails bindings and runtime events must stay behind the frontend
# adapter seam. This makes accidental contract drift fail in CI immediately.
$directBindingImports = rg -n 'bindings/cczjVideo/app|@wailsio/runtime' frontend/src -g '*.ts' -g '*.vue' |
    Where-Object { $_ -notmatch 'frontend/src\\api\\' }
if ($directBindingImports) {
    Write-Error "Frontend boundary violation: use frontend/src/api adapters.`n$($directBindingImports -join "`n")"
}

$directStorageAccess = rg -n 'localStorage\.' frontend/src -g '*.ts' -g '*.vue' |
    Where-Object { $_ -notmatch 'frontend/src\\platform\\' }
if ($directStorageAccess) {
    Write-Error "Frontend storage violation: use frontend/src/platform/storage.ts.`n$($directStorageAccess -join "`n")"
}

$appLineCount = (Get-Content 'app.go').Count
if ($appLineCount -gt 600) {
    Write-Error "app.go must remain a composition root/facade under 600 lines (actual: $appLineCount)."
}

$downloadInApp = rg -n '^func \(a \*App\) (runDownload|downloadDirect|downloadDirectSingle|downloadDirectParallel|downloadM3u8)' app.go
if ($downloadInApp) {
    Write-Error "Download transport implementation must stay outside app.go.`n$($downloadInApp -join "`n")"
}

if (Test-Path 'frontend/vite.config.ts.timestamp-*.mjs') {
    Write-Error 'Generated Vite timestamp artifacts must not be present in the source tree.'
}

go vet ./...
go test ./...

Push-Location frontend
try {
    npm run build
}
finally {
    Pop-Location
}
