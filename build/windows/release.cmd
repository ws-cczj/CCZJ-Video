@echo off
REM Double-click entry point for the release helper. All logic lives in release.ps1;
REM this file exists only because a .ps1 opens in Notepad when you double-click it.
REM Arguments are forwarded, e.g.  release.cmd -SkipBuild  -NoOpen
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0release.ps1" %*
set "RC=%ERRORLEVEL%"
REM When launched from Explorer there is no console left to read the output, so keep the
REM window open. Running it from a terminal must not block on a pause.
echo %cmdcmdline% | find /i "%~f0" >nul 2>&1
if %ERRORLEVEL%==0 (
  echo.
  echo ^(exit code %RC%^)
  pause
)
exit /b %RC%
