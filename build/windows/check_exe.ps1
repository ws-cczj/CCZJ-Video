# Reads back the Windows version resource from a built .exe and fails if it is
# missing. `go build` links a .syso silently: if the resource file is absent or
# malformed the binary still builds fine, just without an icon or version info,
# so the packaging step needs to look at the produced file rather than trust
# that the .syso existed.
param(
    [Parameter(Mandatory = $true)][string] $Path,
    [string] $ExpectVersion = ""
)

$ErrorActionPreference = "Stop"

$resolved = (Resolve-Path -LiteralPath $Path).Path
$info = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($resolved)

if ([string]::IsNullOrWhiteSpace($info.FileVersion)) {
    throw "no version resource in $resolved - the .syso was missing or not linked. Re-run: wails3 task windows:generate:syso"
}

Write-Host "file        : $resolved"
Write-Host "FileVersion : $($info.FileVersion)"
Write-Host "ProductVer  : $($info.ProductVersion)"
Write-Host "ProductName : $($info.ProductName)"
Write-Host "Company     : $($info.CompanyName)"
Write-Host "Description : $($info.FileDescription)"
Write-Host "Copyright   : $($info.LegalCopyright)"

if ($ExpectVersion -and $info.ProductVersion -ne $ExpectVersion) {
    throw "ProductVersion is '$($info.ProductVersion)', expected '$ExpectVersion'"
}
