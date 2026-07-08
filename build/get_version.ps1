$c = [IO.File]::ReadAllText("$PSScriptRoot\..\version.json", [Text.Encoding]::UTF8)
$m = [regex]::Match($c, '"version"\s*:\s*"([^"]+)"')
Write-Host -NoNewline $m.Groups[1].Value
