# Prepare local release assets. Public GitHub publication remains explicit.
param(
    [string]$Version = '',
    [string]$OutDir = 'dist/releases',
    [ValidateSet('ExistingPreview','Environment')][string]$AndroidSigning = 'ExistingPreview',
    [switch]$SkipBuild,
    [switch]$SkipVerify,
    [switch]$NoOpen
)
$ErrorActionPreference = 'Stop'
$workspace = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location -LiteralPath $workspace
$metadata = Get-Content -LiteralPath version.json -Raw -Encoding UTF8 | ConvertFrom-Json
if (!$Version) { $Version=$metadata.version }
if ($Version -ne $metadata.version -or $Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Release version must exactly match version.json.' }
$tag = "v$Version"
$base = if ([IO.Path]::IsPathRooted($OutDir)) { $OutDir } else { Join-Path $workspace $OutDir }
$stage = [IO.Path]::GetFullPath((Join-Path $base $tag))
New-Item -ItemType Directory -Force -Path $stage | Out-Null
function Run-Step([string]$Label,[scriptblock]$Work) {
    Write-Host "==> $Label"
    & $Work
    if ($LASTEXITCODE -ne 0) { throw "$Label failed with exit $LASTEXITCODE" }
}
if (!$SkipBuild) {
    if (!$SkipVerify) { Run-Step 'Verify repository' { & powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify.ps1 } }
    Run-Step 'Build frontend' { Push-Location frontend; try { & npm run build } finally { Pop-Location } }
    Run-Step 'Package Windows' { & powershell -NoProfile -ExecutionPolicy Bypass -File build/windows/package.ps1 -Version $Version -OutDir $stage -BinOut "bin/cczjVideo-$Version.exe" }
    $androidArgs = @('-NoProfile','-ExecutionPolicy','Bypass','-File','build/android/build.ps1','-Arch','arm64','-BuildType','Release','-SkipFrontend')
    if ($AndroidSigning -eq 'ExistingPreview') { $androidArgs += '-UseExistingPreviewKey' }
    Run-Step 'Build Android ARM64' { & powershell @androidArgs }
    Copy-Item -LiteralPath "bin/CCZJ-Video-android-arm64-preview-release.apk" -Destination (Join-Path $stage "cczjVideo-android-arm64-$Version.apk")
}
$exe = "cczjVideo-windows-amd64-$Version.exe"
$zip = "cczjVideo-windows-amd64-$Version-portable.zip"
$apk = "cczjVideo-android-arm64-$Version.apk"
foreach ($name in @($exe,$zip,$apk)) { if (!(Test-Path -LiteralPath (Join-Path $stage $name))) { throw "Missing release asset: $name" } }
Run-Step 'Read EXE version' { & powershell -NoProfile -ExecutionPolicy Bypass -File build/windows/check_exe.ps1 -Path (Join-Path $stage $exe) -ExpectVersion $Version }
$apkMetadata = Join-Path $stage 'android-metadata.json'
$checkArgs = @('-NoProfile','-ExecutionPolicy','Bypass','-File','build/android/check_apk.ps1','-Path',(Join-Path $stage $apk),'-Version',$Version,'-JsonOut',$apkMetadata)
if ($AndroidSigning -eq 'ExistingPreview' -and (Test-Path -LiteralPath 'bin/CCZJ-Video-android-arm64-preview.apk')) { $checkArgs += @('-CompareSigningWith',(Resolve-Path -LiteralPath 'bin/CCZJ-Video-android-arm64-preview.apk').Path) }
Run-Step 'Read APK version, architecture and signature' { & powershell @checkArgs }
$remote = & git config --get remote.origin.url
$repo = [regex]::Match("$remote",'github\.com[:/]([^/]+/[^/]+?)(?:\.git)?$').Groups[1].Value
if (!$repo) { throw 'origin must identify the GitHub release repository.' }
$notesText = [IO.File]::ReadAllText((Join-Path $workspace 'CHANGELOG.md'),[Text.Encoding]::UTF8)
$section = [regex]::Match($notesText,'(?ms)^## \['+[regex]::Escape($Version)+'\][^\n]*\n(.*?)(?=^## |\z)')
if (!$section.Success -or !$section.Groups[1].Value.Trim()) { throw 'A version-specific CHANGELOG section is required.' }
[IO.File]::WriteAllText((Join-Path $stage 'release-notes.md'),$section.Groups[1].Value.Trim()+"`n",(New-Object Text.UTF8Encoding $false))
$artifacts = @()
foreach ($name in @($exe,$zip,$apk)) {
    $file=Get-Item -LiteralPath (Join-Path $stage $name)
    $artifacts += [ordered]@{name=$name;bytes=$file.Length;sha256=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
}
$assets = @($artifacts | ForEach-Object { [ordered]@{name=$_.name;browser_download_url="https://github.com/$repo/releases/download/$tag/$($_.name)";size=$_.bytes} })
$releaseVersion = [ordered]@{version=$Version;desc=$metadata.desc;history=$metadata.history;assets=$assets}
[IO.File]::WriteAllText((Join-Path $stage 'version.json'),($releaseVersion | ConvertTo-Json -Depth 12)+"`n",(New-Object Text.UTF8Encoding $false))
$head = & git rev-parse HEAD
$dirty = [bool](& git status --porcelain)
$manifest = [ordered]@{version=$Version;tag=$tag;repository=$repo;sourceCommit="$head";workingTreeDirty=$dirty;createdAt=[DateTime]::UtcNow.ToString('o');androidSigning=$AndroidSigning;android=(Get-Content -LiteralPath $apkMetadata -Raw | ConvertFrom-Json);artifacts=$artifacts}
[IO.File]::WriteAllText((Join-Path $stage 'artifact-manifest.json'),($manifest | ConvertTo-Json -Depth 12)+"`n",(New-Object Text.UTF8Encoding $false))
$upload = @($exe,$zip,$apk,'version.json','artifact-manifest.json','android-metadata.json','release-notes.md')
$lines = @($upload | ForEach-Object { (Get-FileHash -LiteralPath (Join-Path $stage $_) -Algorithm SHA256).Hash.ToLowerInvariant()+"  $_" })
[IO.File]::WriteAllText((Join-Path $stage 'checksums.txt'),($lines -join "`n")+"`n",(New-Object Text.UTF8Encoding $false))
Run-Step 'Validate staged release' { & node scripts/check-release-artifacts.mjs $stage }
Write-Host "Release folder: $stage"
Write-Host "Upload all assets in this folder to $repo / $tag, then publish the release."
if ($dirty) { Write-Host 'Source has uncommitted changes. Commit and push the intended release sources before selecting the GitHub tag target.' }
if (!$NoOpen) {
    Start-Process explorer.exe -ArgumentList @($stage) | Out-Null
    Start-Process "https://github.com/$repo/releases/new?tag=$tag&title=$tag" | Out-Null
}
