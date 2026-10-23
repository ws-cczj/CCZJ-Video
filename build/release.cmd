@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0release.ps1" %*
set "result=%errorlevel%"
if not "%result%"=="0" echo Release preparation failed with exit %result%.
pause
exit /b %result%
