@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0release.ps1" -Publish -Commit %*
set "result=%errorlevel%"
if not "%result%"=="0" echo Publish failed. Read the error above; published releases are never overwritten.
pause
exit /b %result%
