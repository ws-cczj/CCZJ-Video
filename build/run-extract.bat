@echo off
echo === Anime4K 权重提取工具 ===
echo.

REM 尝试常见 Node.js 路径
where node >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    echo [OK] 找到 node
    node "%~dp0extract-weights.js"
    goto :done
)

if exist "C:\Program Files\nodejs\node.exe" (
    echo [OK] 找到 C:\Program Files\nodejs\node.exe
    "C:\Program Files\nodejs\node.exe" "%~dp0extract-weights.js"
    goto :done
)

if exist "%LOCALAPPDATA%\Programs\nodejs\node.exe" (
    echo [OK] 找到 %LOCALAPPDATA%\Programs\nodejs\node.exe
    "%LOCALAPPDATA%\Programs\nodejs\node.exe" "%~dp0extract-weights.js"
    goto :done
)

if exist "%APPDATA%\nvm\current\node.exe" (
    echo [OK] 找到 nvm node
    "%APPDATA%\nvm\current\node.exe" "%~dp0extract-weights.js"
    goto :done
)

echo [ERROR] 未找到 Node.js，请先安装 Node.js: https://nodejs.org/
echo 安装后重新运行此脚本。
exit /b 1

:done
echo.
echo 提取完成！权重文件已更新到 frontend/src/utils/ 目录。
pause
