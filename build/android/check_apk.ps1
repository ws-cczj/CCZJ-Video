param(
    [Parameter(Mandatory=$true)][string]$Path,
    [string]$Version = '',
    [ValidateSet('arm64','amd64')][string]$Arch = 'arm64',
    [string]$ApplicationId = 'com.cczj.video.preview',
    [string]$CompareSigningWith = '',
    [string]$JsonOut = '',
    [switch]$AllowDebug
)
$ErrorActionPreference = 'Stop'
$workspace = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$apkPath = (Resolve-Path -LiteralPath $Path).Path
if (!$Version) { $Version = (Get-Content -LiteralPath (Join-Path $workspace 'version.json') -Raw -Encoding UTF8 | ConvertFrom-Json).version }
$sdk = $env:ANDROID_HOME
if (!$sdk) { $sdk = Join-Path $env:LOCALAPPDATA 'Android/Sdk' }
$tools = Get-ChildItem -LiteralPath (Join-Path $sdk 'build-tools') -Directory | Sort-Object Name -Descending | Select-Object -First 1
if (!$tools) { throw 'Android build tools are unavailable.' }
$oldJava = $env:JAVA_HOME
try {
    $javaCandidates = @($oldJava)
    if ($oldJava) { $javaCandidates += @(Get-ChildItem -LiteralPath (Split-Path $oldJava) -Directory | Where-Object Name -Match '^jdk-(21|17)' | Sort-Object Name -Descending | Select-Object -ExpandProperty FullName) }
    foreach ($candidate in $javaCandidates) {
        if ($candidate -and (Test-Path -LiteralPath "$candidate/bin/javac.exe")) {
            try { $ErrorActionPreference='Continue'; $v = & "$candidate/bin/javac.exe" -version 2>&1 }
            finally { $ErrorActionPreference='Stop' }
            if ("$v" -match 'javac (\d+)' -and [int]$Matches[1] -ge 17) { $env:JAVA_HOME=$candidate; break }
        }
    }
    $badging = & "$($tools.FullName)/aapt2.exe" dump badging $apkPath
    if ($LASTEXITCODE -ne 0) { throw 'APK manifest cannot be read.' }
    $package = [regex]::Match(($badging -join "`n"), "package: name='([^']+)' versionCode='(\d+)' versionName='([^']+)'")
    $parts = ($Version.Split('-')[0]).Split('.')
    $code = [int]$parts[0]*10000+[int]$parts[1]*100+[int]$parts[2]
    if (!$package.Success -or $package.Groups[1].Value -ne $ApplicationId -or $package.Groups[2].Value -ne "$code" -or $package.Groups[3].Value -ne $Version) { throw 'APK package, versionName or versionCode does not match this release.' }
    if (!$AllowDebug -and ($badging -match '^application-debuggable')) { throw 'A debuggable APK cannot enter the release folder.' }
    $abi = if ($Arch -eq 'arm64') { 'arm64-v8a' } else { 'x86_64' }
    $native = @($badging | Where-Object { $_ -match '^native-code:' })
    if ($native.Count -ne 1 -or $native[0] -ne "native-code: '$abi'") { throw "APK must contain only $abi." }
    function Read-Certificate([string]$File) {
        $output = & "$($tools.FullName)/apksigner.bat" verify --print-certs $File
        if ($LASTEXITCODE -ne 0) { throw 'APK signature verification failed.' }
        $match = [regex]::Match(($output -join "`n"),'certificate SHA-256 digest: ([a-fA-F0-9]{64})')
        if (!$match.Success) { throw 'APK signing certificate is missing.' }
        return $match.Groups[1].Value.ToLowerInvariant()
    }
    $certificate = Read-Certificate $apkPath
    if ($CompareSigningWith -and $certificate -ne (Read-Certificate (Resolve-Path -LiteralPath $CompareSigningWith).Path)) { throw 'APK signing identity differs from the existing preview; it cannot update those installations.' }
    $metadata = [ordered]@{package=$ApplicationId;version=$Version;versionCode=$code;abi=$abi;debuggable=[bool]($badging -match '^application-debuggable');certificateSHA256=$certificate}
    $json = $metadata | ConvertTo-Json
    if ($JsonOut) { [IO.File]::WriteAllText($JsonOut,$json+"`n",(New-Object Text.UTF8Encoding $false)) }
    Write-Output $json
} finally { $env:JAVA_HOME=$oldJava }
