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
    Write-Error "app.go must remain a composition root under 600 lines (actual: $appLineCount)."
}

$downloadInApp = rg -n '^func \(a \*App\) (runDownload|downloadDirect|downloadDirectSingle|downloadDirectParallel|downloadM3u8)' app/service/app.go
if ($downloadInApp) {
    Write-Error "Download transport implementation must stay in app/service/download.go.`n$($downloadInApp -join "`n")"
}

# 根目录只允许 main.go（入口）和 app.go（装配）。业务代码一律进 app/ 下的包，
# 否则会重新长出 app_xxx.go 这样的散装文件。
$looseRootGo = Get-ChildItem -File -Filter '*.go' | Where-Object { $_.Name -notin @('main.go', 'app.go') }
if ($looseRootGo) {
    Write-Error "Root must only contain main.go and app.go; move these into app/...`n$(($looseRootGo | ForEach-Object { $_.Name }) -join "`n")"
}

if (Test-Path 'frontend/vite.config.ts.timestamp-*.mjs') {
    Write-Error 'Generated Vite timestamp artifacts must not be present in the source tree.'
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
