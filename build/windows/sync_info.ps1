# Writes build/windows/info.generated.json from the tracked info.json template,
# stamping the real version into both version fields.
#
# The template keeps "0.0.0" on purpose: writing the stamped file to a separate
# path means `wails3 task build` never dirties a tracked file, so the version in
# the .exe resources cannot drift from version.json and git status stays clean.
param(
    [Parameter(Mandatory = $true)][string] $Version,
    [string] $Template = "build/windows/info.json",
    [string] $Out = "build/windows/info.generated.json"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$templatePath = Join-Path $root $Template
$outPath = Join-Path $root $Out

if (-not (Test-Path $templatePath)) { throw "info.json template not found: $templatePath" }
if ($Version -notmatch '^\d+(\.\d+){1,3}(-[\w.]+)?$') { throw "not a usable file version: $Version" }

$text = [IO.File]::ReadAllText($templatePath, [Text.Encoding]::UTF8)
$info = $text | ConvertFrom-Json

# Windows file versions need four numeric parts; a pre-release suffix stays in
# the free-text ProductVersion only, otherwise the resource compiler rejects it.
# `-split '.'` would treat the dot as "any character" and shatter the version,
# which is why the separator is escaped here.
$parts = @(($Version -split '-')[0] -split '\.')
while ($parts.Count -lt 4) { $parts += '0' }
$fileVersion = ($parts[0..3] -join '.')

$info.fixed.file_version = $fileVersion
$info.fixed.product_version = $fileVersion

# The "info" object is keyed by Windows language ID (winres parses the key as
# hex), so the stamping target is whatever block the template declares rather
# than a hardcoded key. The key must be a real langID such as "0409": with "0000"
# winres still emits a self-consistent StringFileInfo\000004b0 block, and every
# Win32 version API that picks a table by language skips it, so Explorer and
# FileVersionInfo report the file as having no version at all.
#
# FileVersion and ProductVersion exist twice: as the numeric fixed block (read
# from "fixed") and as free-text strings in the string table. .NET's
# FileVersionInfo.FileVersion only ever reads the string, so a template that
# omits it builds a perfectly valid exe that still reports an empty version.
foreach ($lang in @($info.info.PSObject.Properties.Name)) {
    $info.info.$lang.FileVersion = $fileVersion
    $info.info.$lang.ProductVersion = $Version
}

$json = $info | ConvertTo-Json -Depth 6
[IO.File]::WriteAllText($outPath, $json, (New-Object Text.UTF8Encoding $false))

Write-Host "wrote $outPath (FileVersion=$fileVersion, ProductVersion=$Version)"
