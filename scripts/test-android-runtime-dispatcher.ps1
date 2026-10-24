param([string]$JavaHome = $env:JAVA_HOME)
$ErrorActionPreference = 'Stop'
$workspace = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (!(Test-Path -LiteralPath "$JavaHome/bin/javac.exe")) { throw 'Pass -JavaHome pointing to JDK 17 or newer.' }
$output = Join-Path $workspace 'dist/audit/lifecycle/android-dispatcher'
New-Item -ItemType Directory -Force -Path $output | Out-Null
$source = Join-Path $workspace 'build/android/app/src/main/java/com/wails/app/CczjRuntimeDispatcher.java'
$test = Join-Path $PSScriptRoot 'fixtures/android-runtime-dispatcher/DispatcherTest.java'
& "$JavaHome/bin/javac.exe" -encoding UTF-8 -d $output $source $test
if ($LASTEXITCODE -ne 0) { throw 'Dispatcher compilation failed.' }
& "$JavaHome/bin/java.exe" -cp $output com.wails.app.DispatcherTest | Tee-Object (Join-Path $output 'result.log')
if ($LASTEXITCODE -ne 0) { throw 'Dispatcher behavior failed.' }
