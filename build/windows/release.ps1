# Compatibility entry point; shared release preparation now includes Android.
param([string]$Version='', [string]$Title='', [string]$OutDir='dist/releases', [switch]$SkipBuild, [switch]$AllowStaleDesc, [switch]$NoOpen)
$arguments=@('-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $PSScriptRoot '../release.ps1'),'-OutDir',$OutDir)
if($Version){$arguments+=@('-Version',$Version)}
if($SkipBuild){$arguments+='-SkipBuild'}
if($NoOpen){$arguments+='-NoOpen'}
if($Title){Write-Warning 'The shared release helper uses the version tag as the initial GitHub title.'}
if($AllowStaleDesc){Write-Warning 'Release metadata is now validated against version.json.'}
& powershell @arguments
exit $LASTEXITCODE
