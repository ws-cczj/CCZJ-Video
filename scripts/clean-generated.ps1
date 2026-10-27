[CmdletBinding(SupportsShouldProcess = $true)]
param([switch]$Apply)

$ErrorActionPreference = 'Stop'
$workspace = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$workspacePrefix = $workspace + [IO.Path]::DirectorySeparatorChar
$targets = [Collections.Generic.List[object]]::new()
$tracked = @(& git -C $workspace -c core.quotePath=false ls-files)
if ($LASTEXITCODE -ne 0) { throw 'Cannot verify tracked files; cleanup stopped.' }
$activeFiles = @(Get-CimInstance Win32_Process | ForEach-Object { $_.ExecutablePath } | Where-Object { $_ })

function Add-GeneratedTarget([string]$relative, [string]$reason) {
    $path = [IO.Path]::GetFullPath((Join-Path $workspace $relative))
    if (-not $path.StartsWith($workspacePrefix, [StringComparison]::OrdinalIgnoreCase)) { throw "Path escaped workspace: $relative" }
    if (-not (Test-Path -LiteralPath $path)) { return }
    $relativeKey = $relative.Replace('\', '/').TrimEnd('/')
    foreach ($file in $tracked) {
        if ($file -eq $relativeKey -or $file.StartsWith($relativeKey + '/')) { throw "Tracked file in cleanup target: $relative" }
    }
    foreach ($file in $activeFiles) {
        if ($file -eq $path -or $file.StartsWith($path + '\', [StringComparison]::OrdinalIgnoreCase)) {
            Write-Warning "Kept running executable: $relative"
            return
        }
    }
    $item = Get-Item -LiteralPath $path -Force
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Reparse point is not a cleanup target: $relative" }
    if ($item.PSIsContainer) {
        try { $entries = @(Get-ChildItem -LiteralPath $path -Recurse -Force -ErrorAction Stop) }
        catch { Write-Warning "Kept directory that could not be fully inspected: $relative"; return }
        if ($entries |
            Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint } | Select-Object -First 1) {
            Write-Warning "Kept directory containing a reparse point: $relative"
            return
        }
        $protected = $entries | Where-Object { -not $_.PSIsContainer } |
            Where-Object {
                $_.Extension -in @('.db','.sqlite','.sqlite3','.keystore','.jks','.p12','.pfx','.pem','.key','.zip','.7z') -or
                $_.Name -match '(?i)(backup|snapshot|handoff|before).*(\.json|\.xml|\.sql)$'
            } | Select-Object -First 1
        if ($protected) {
            Write-Warning "Kept possible user data, archive or recovery material: $relative"
            return
        }
    }
    $targets.Add([pscustomobject]@{ Path = $path; Relative = $relativeKey; Reason = $reason })
}

# Stable release output, source snapshots, original handoff archives and bin/data
# are deliberately retained. This list is limited to reproducible generated data.
foreach ($name in @('content-fixes-20261021','css-reuse-20261020','mobile-player-fixes-20261021',
    'novel-20261021','novel-fix-20261019','reading-fix-20261020','request-pipelines-2026-10-20','repository-cleanup')) {
    Add-GeneratedTarget "dist/$name" 'obsolete build or temporary audit'
}
$releaseVersion = (Get-Content -LiteralPath (Join-Path $workspace 'version.json') -Raw -Encoding UTF8 | ConvertFrom-Json).version
$keptAudit = @('novel-usability','fanqie-font','player-hold','reading-classics','cleanup',
    'cast-pairing','playback-routes','playback-live')
if (Test-Path -LiteralPath (Join-Path $workspace 'dist/audit')) {
    foreach ($item in Get-ChildItem -LiteralPath (Join-Path $workspace 'dist/audit') -Force) {
        if ($item.PSIsContainer -and $item.Name -notin $keptAudit -and $item.Name -notlike 'cleanup-*') { Add-GeneratedTarget "dist/audit/$($item.Name)" 'old temporary screenshots, logs and profiles' }
        elseif (-not $item.PSIsContainer -and $item.Name -notlike 'novel-usability-*' -and $item.Name -notlike "release-$releaseVersion-*") { Add-GeneratedTarget "dist/audit/$($item.Name)" 'old temporary audit output' }
    }
}
if (Test-Path -LiteralPath (Join-Path $workspace 'dist/test-builds')) {
    $builds = @(Get-ChildItem -LiteralPath (Join-Path $workspace 'dist/test-builds') -Directory | Sort-Object LastWriteTime -Descending)
    foreach ($item in ($builds | Select-Object -Skip 1)) { Add-GeneratedTarget "dist/test-builds/$($item.Name)" 'superseded test installation package' }
}
foreach ($item in Get-ChildItem -LiteralPath (Join-Path $workspace 'dist') -File -ErrorAction SilentlyContinue) {
    if ($item.Name -eq 'reading-source-audit-20261022.zip') { continue }
    if ($item.Extension -in @('.exe','.zip','.log') -or $item.Name -like '*.exe~' -or $item.Name -eq 'checksums.txt') {
        Add-GeneratedTarget "dist/$($item.Name)" 'loose obsolete executable or build log'
    }
}
foreach ($relative in @('build/android/app/build','build/android/build','build/android/gen','build/android/overlay.json',
    'build/android/app/src/main/jniLibs','bin/cczjVideo-v2.1.0-release.exe',
    'dist/source-batch-quality-20261021/tools/w64devkit','dist/source-batch-quality-20261021/tools/golangci-lint.exe',
    'dist/source-batch-quality-20261021/tools/w64devkit-x64-2.10.1.7z.exe',
    'dist/source-batch-quality-20261021/tools/wait-address-probe.exe','dist/source-batch-quality-20261021/tools/wait-address-probe.c')) {
    Add-GeneratedTarget $relative 'reproducible intermediate build or downloaded temporary tool'
}
foreach ($item in Get-ChildItem -LiteralPath (Join-Path $workspace 'dist/source-batch-quality-20261021') -File -Filter '*.exe' -ErrorAction SilentlyContinue) {
    Add-GeneratedTarget "dist/source-batch-quality-20261021/$($item.Name)" 'old benchmark executable; cmd sources retained'
}
foreach ($item in Get-ChildItem -LiteralPath (Join-Path $workspace '.cache') -File -ErrorAction SilentlyContinue) {
    if ($item.Extension -eq '.log' -or $item.Name -eq 'classic-search-continue.py') { Add-GeneratedTarget ".cache/$($item.Name)" 'temporary log or one-off script' }
}
foreach ($item in Get-ChildItem -LiteralPath (Join-Path $workspace 'dist/audit/novel-usability/browser') -File -Filter 'failure-*' -ErrorAction SilentlyContinue) {
    Add-GeneratedTarget "dist/audit/novel-usability/browser/$($item.Name)" 'superseded failed screenshot; passing evidence retained'
}

$unique = @($targets | Sort-Object Path -Unique)
$unique | Select-Object Relative, Reason | Format-Table -AutoSize
Write-Host "Selected $($unique.Count) generated paths. Source code, dependencies, source configurations, handoffs, current packages and user data are retained."
if (-not $Apply) { Write-Host 'Preview only. To delete the listed outputs, run this script with -Apply.'; return }

$removed = [Collections.Generic.List[string]]::new()
$skipped = [Collections.Generic.List[object]]::new()
$failures = [Collections.Generic.List[object]]::new()
foreach ($target in $unique) {
    try {
        if (-not (Test-Path -LiteralPath $target.Path)) { continue }
        $resolved = (Resolve-Path -LiteralPath $target.Path).Path
        if ($resolved -ne $target.Path -or -not $resolved.StartsWith($workspacePrefix, [StringComparison]::OrdinalIgnoreCase)) { throw 'Target resolution changed.' }
        if ($PSCmdlet.ShouldProcess($resolved, 'Remove reproducible generated output')) {
            Remove-Item -LiteralPath $resolved -Recurse -Force -ErrorAction Stop
            $removed.Add($target.Relative)
        }
    } catch {
        $cause = $_.Exception
        while ($cause.InnerException) { $cause = $cause.InnerException }
        if (($cause.HResult -band 0xffff) -in @(32,33)) {
            $skipped.Add([pscustomobject]@{ Path=$target.Relative; Reason='in use by another process' })
        } else { $failures.Add([pscustomobject]@{ Path=$target.Relative; Error=$_.Exception.Message }) }
    }
}
$resultPath = Join-Path $workspace 'dist/audit/cleanup'
New-Item -ItemType Directory -Force -Path $resultPath | Out-Null
@{ removed=$removed.ToArray(); skipped=$skipped.ToArray(); failed=$failures.ToArray() } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $resultPath 'result.json') -Encoding utf8
Write-Host "Removed $($removed.Count) generated paths; $($skipped.Count) locked paths retained; $($failures.Count) failures."
if ($failures.Count) { $failures | Format-Table -AutoSize; exit 1 }
