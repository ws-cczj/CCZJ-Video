# Builds build/appicon.png (square) from the repo-root wordmark icon.png.
# icon.png is 290x193 with transparency: a non-square source makes the ICO
# generator stretch the logo, so we pad onto a square canvas instead of cropping.
param(
    [string] $Source = "icon.png",
    [string] $Out = "build/appicon.png",
    [int] $Size = 1024,
    [double] $FillRatio = 0.82
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$root = Split-Path -Parent $PSScriptRoot
$srcPath = Join-Path $root $Source
$dstPath = Join-Path $root $Out

if (-not (Test-Path $srcPath)) { throw "source icon not found: $srcPath" }
$dir = Split-Path -Parent $dstPath
if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir | Out-Null }

$src = [System.Drawing.Image]::FromFile($srcPath)
try {
    $scale = [Math]::Min($Size * $FillRatio / $src.Width, $Size * $FillRatio / $src.Height)
    $w = [int]([Math]::Round($src.Width * $scale))
    $h = [int]([Math]::Round($src.Height * $scale))
    $x = [int](($Size - $w) / 2)
    $y = [int](($Size - $h) / 2)

    $bmp = New-Object System.Drawing.Bitmap $Size, $Size, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    try {
        $g.Clear([System.Drawing.Color]::Transparent)
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
        $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $rect = New-Object System.Drawing.Rectangle $x, $y, $w, $h
        $g.DrawImage($src, $rect)
    } finally {
        $g.Dispose()
    }
    $bmp.Save($dstPath, [System.Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()
} finally {
    $src.Dispose()
}

Write-Host "wrote $dstPath ($Size x $Size, logo fitted to $w x $h)"
