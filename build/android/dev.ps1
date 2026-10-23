param(
    [string]$Device = '',
    [ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64',
    [ValidateRange(1024, 65535)][int]$Port = 19545,
    [switch]$InstallHost
)
$ErrorActionPreference = 'Stop'
$workspace = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
$viteProcess = $null
$ownedReverse = $false
$devEnabled = $false
$previousDevPort = $env:CCZJ_ANDROID_DEV_PORT
function Assert-Exit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed ($LASTEXITCODE)." }
}
try {
    $targets = @(& adb devices | Select-String '^([^\s]+)\s+device$' | ForEach-Object { $_.Matches[0].Groups[1].Value })
    if (!$Device) {
        if ($targets.Count -ne 1) { throw 'Pass -Device when zero or multiple devices are connected.' }
        $Device = $targets[0]
    }
    if ($Device -notin $targets) { throw "Android device '$Device' is not connected." }
    if ($InstallHost) {
        & (Join-Path $PSScriptRoot 'build.ps1') -Arch $Arch -Install -Device $Device
    }
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $Port)
    try { $listener.Start() } finally { $listener.Stop() }
    $reverse = @(& adb -s $Device reverse --list)
    if ($reverse | Where-Object { $_ -match "\s+tcp:$Port\s+" }) { throw "ADB reverse tcp:$Port already exists; choose another -Port." }
    $logs = Join-Path $workspace 'dist/audit'
    New-Item -ItemType Directory -Force -Path $logs | Out-Null
    $vite = Join-Path $workspace 'frontend/node_modules/vite/bin/vite.js'
    $env:CCZJ_ANDROID_DEV_PORT = [string]$Port
    $viteProcess = Start-Process -FilePath (Get-Command node).Source -ArgumentList @(('"' + $vite + '"'), '--mode', 'android') `
        -WorkingDirectory (Join-Path $workspace 'frontend') -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $logs "android-vite-$Port.log") -RedirectStandardError (Join-Path $logs "android-vite-$Port.err.log")
    $ready = $false
    for ($attempt = 0; $attempt -lt 40; $attempt++) {
        if ($viteProcess.HasExited) { throw 'Vite stopped; see dist/audit/android-vite-*.err.log.' }
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/@vite/client" -TimeoutSec 1
            if ($response.StatusCode -eq 200) { $ready = $true; break }
        } catch { Start-Sleep -Milliseconds 250 }
    }
    if (!$ready) { throw 'Vite did not become ready.' }
    adb -s $Device reverse "tcp:$Port" "tcp:$Port"
    Assert-Exit 'ADB reverse'
    $ownedReverse = $true
    adb -s $Device shell am start -W --activity-single-top --activity-clear-top -n com.cczj.video.preview/com.wails.app.MainActivity --ei cczjFrontendPort $Port
    Assert-Exit 'Enable native frontend development'
    $devEnabled = $true
    Write-Host "Android frontend HMR: http://127.0.0.1:$Port (Vite PID $($viteProcess.Id))"
    Write-Host 'Save Vue/CSS/TS to update MuMu. Native Go/Java changes require rebuilding the host.'
    Write-Host 'Keep this terminal running. Ctrl+C restores embedded assets and removes this script''s reverse port.'
    while (!$viteProcess.HasExited) { Start-Sleep -Milliseconds 500 }
} finally {
    if ($devEnabled) {
        & adb -s $Device shell am start -W --activity-single-top --activity-clear-top -n com.cczj.video.preview/com.wails.app.MainActivity --ei cczjFrontendPort 0 | Out-Null
    }
    if ($ownedReverse) { & adb -s $Device reverse --remove "tcp:$Port" | Out-Null }
    if ($viteProcess -and !$viteProcess.HasExited) { Stop-Process -Id $viteProcess.Id -ErrorAction SilentlyContinue }
    $env:CCZJ_ANDROID_DEV_PORT = $previousDevPort
}
