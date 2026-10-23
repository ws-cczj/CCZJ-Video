param(
    [ValidateSet('amd64', 'arm64')][string]$Arch = 'arm64',
    [string]$Sdk = $env:ANDROID_HOME,
    [string]$JavaHome = $env:JAVA_HOME,
    [string]$Device = '',
    [ValidateSet('com.cczj.video.preview', 'com.cczj.video.acceptance')][string]$ApplicationId = 'com.cczj.video.preview',
    [ValidateSet('Debug', 'Release')][string]$BuildType = 'Debug',
    [switch]$UseExistingPreviewKey,
    [switch]$SkipFrontend,
    [switch]$Install
)
$ErrorActionPreference = 'Stop'
$workspace = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$previousDirectory = Get-Location
$environmentKeys = @('GOOS', 'GOARCH', 'GOWORK', 'CGO_ENABLED', 'CC', 'CXX', 'JAVA_HOME', 'ANDROID_HOME', 'Path')
$savedEnvironment = @{}
foreach ($key in $environmentKeys) { $savedEnvironment[$key] = [Environment]::GetEnvironmentVariable($key, 'Process') }
function Assert-NativeSuccess([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed with exit code $LASTEXITCODE" }
}
try {
    Set-Location -LiteralPath $workspace
    if (!$Sdk) { $Sdk = Join-Path $env:LOCALAPPDATA 'Android/Sdk' }
    $Sdk = (Resolve-Path -LiteralPath $Sdk).Path
    # Keep the machine's Java default unchanged. Older desktop projects may use Java 8.
    $javaCandidates = @($JavaHome)
    if ($JavaHome) {
        $javaCandidates += @(Get-ChildItem -LiteralPath (Split-Path $JavaHome) -Directory |
            Where-Object Name -Match '^jdk-(21|17)' | Sort-Object Name -Descending | Select-Object -ExpandProperty FullName)
    }
    $selectedJava = $null
    foreach ($candidate in $javaCandidates) {
        if (!$candidate -or !(Test-Path -LiteralPath "$candidate/bin/javac.exe")) { continue }
        # Java 8 prints its version to stderr; Windows PowerShell 5 treats it as
        # an error record. Read it without aborting the search for a newer JDK.
        try {
            $ErrorActionPreference = 'Continue'
            $javaVersion = & "$candidate/bin/javac.exe" -version 2>&1
        } finally { $ErrorActionPreference = 'Stop' }
        if ("$javaVersion" -match 'javac (\d+)' -and [int]$Matches[1] -ge 17) { $selectedJava = $candidate; break }
    }
    if (!$selectedJava) { throw 'Set JAVA_HOME or pass -JavaHome pointing to JDK 17 or newer.' }
    $ndk = Get-ChildItem -LiteralPath "$Sdk/ndk" -Directory | Sort-Object Name -Descending |
        Where-Object { Test-Path -LiteralPath "$($_.FullName)/toolchains/llvm/prebuilt/windows-x86_64/bin/clang.exe" } |
        Select-Object -First 1
    if (!$ndk) { throw 'An installed Windows Android NDK with clang.exe is required.' }
    $env:ANDROID_HOME = $Sdk
    $env:JAVA_HOME = $selectedJava
    $env:Path = "$selectedJava/bin;$Sdk/platform-tools;$env:Path"
    if ($BuildType -eq 'Release' -and !$env:ANDROID_KEYSTORE_FILE) {
        if (!$UseExistingPreviewKey) { throw 'Release requires ANDROID_KEYSTORE_FILE or explicit -UseExistingPreviewKey for existing preview installations.' }
        if (!(Test-Path -LiteralPath (Join-Path $env:USERPROFILE '.android/debug.keystore'))) { throw 'Existing preview signing key is missing; refusing to generate a different identity.' }
    }
    if (!$SkipFrontend) {
        Push-Location frontend
        try { npm run build; Assert-NativeSuccess 'Frontend build' } finally { Pop-Location }
    }
    wails3 android overlay:gen -out build/android/overlay.json -config build/config.yml
    Assert-NativeSuccess 'Android entry point overlay'
    node scripts/prepare-android-runtime.mjs
    Assert-NativeSuccess 'Android structured error overlay'
    $env:GOWORK = 'off'
    $env:GOOS = 'android'
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = '1'
    $triple = if ($Arch -eq 'arm64') { 'aarch64' } else { 'x86_64' }
    $abi = if ($Arch -eq 'arm64') { 'arm64-v8a' } else { 'x86_64' }
    $toolchain = "$($ndk.FullName)/toolchains/llvm/prebuilt/windows-x86_64/bin"
    $env:CC = "$toolchain/clang.exe --target=$triple-linux-android24"
    $env:CXX = "$toolchain/clang++.exe --target=$triple-linux-android24"
    $nativeDirectory = Join-Path $PSScriptRoot "app/src/main/jniLibs/$abi"
    New-Item -ItemType Directory -Force -Path $nativeDirectory | Out-Null
    $version = (Get-Content version.json -Raw -Encoding UTF8 | ConvertFrom-Json).version
    if ($version -notmatch '^(\d+)\.(\d{1,2})\.(\d{1,2})$') { throw 'Android version must be numeric major.minor.patch with minor and patch below 100.' }
    $versionCode = [int]$Matches[1] * 10000 + [int]$Matches[2] * 100 + [int]$Matches[3]
    if ($versionCode -lt 1 -or $versionCode -gt 2100000000) { throw 'Android versionCode is outside its valid range.' }
    $nativeTags = if ($BuildType -eq 'Release') { 'android,production' } else { 'android,debug' }
    go build -buildmode=c-shared -modfile build/android/gen/go.android.mod -overlay build/android/overlay.json -tags $nativeTags -buildvcs=false `
        -ldflags "-w -s -X cczjVideo/app/updater.Version=$version" -o "$nativeDirectory/libwails.so" .
    Assert-NativeSuccess 'Android shared library build'
    Push-Location -LiteralPath $PSScriptRoot
    $signingOpt = if ($UseExistingPreviewKey) { 'true' } else { 'false' }
    try { & ./gradlew.bat clean "assemble$BuildType" --no-daemon --console=plain "-PcczjAbi=$abi" "-PcczjApplicationId=$ApplicationId" "-PcczjVersionName=$version" "-PcczjVersionCode=$versionCode" "-PcczjPreviewSigning=$signingOpt"; Assert-NativeSuccess 'Android APK build' }
    finally { Pop-Location }
    $flavor = if ($ApplicationId -eq 'com.cczj.video.acceptance') { 'acceptance' } else { 'preview' }
    $typeSuffix = if ($BuildType -eq 'Release') { '-release' } else { '' }
    $output = Join-Path $workspace "bin/CCZJ-Video-android-$Arch-$flavor$typeSuffix.apk"
    New-Item -ItemType Directory -Force -Path (Split-Path $output) | Out-Null
    $typeDir = $BuildType.ToLowerInvariant()
    Copy-Item -LiteralPath "$PSScriptRoot/app/build/outputs/apk/$typeDir/app-$typeDir.apk" -Destination $output
    Write-Output "APK: $output"
    if ($Install) {
        $targets = @(& adb devices | Select-String '^([^\s]+)\s+device$' | ForEach-Object { $_.Matches[0].Groups[1].Value })
        if (!$Device) {
            if ($targets.Count -ne 1) { throw 'Pass -Device when zero or multiple Android devices are connected.' }
            $Device = $targets[0]
        }
        if ($Device -notin $targets) { throw "Android device '$Device' is not connected." }
        adb -s $Device install -r $output
        Assert-NativeSuccess 'Install preview APK'
        adb -s $Device shell am start -n "$ApplicationId/com.wails.app.MainActivity"
        Assert-NativeSuccess 'Launch Android preview'
    }
} finally {
    foreach ($key in $environmentKeys) { [Environment]::SetEnvironmentVariable($key, $savedEnvironment[$key], 'Process') }
    Set-Location -LiteralPath $previousDirectory
}
