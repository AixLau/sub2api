@echo off
setlocal

cd /d "%~dp0"
set SINCE=2026-05-26
if not "%CODEX_RECOVER_SINCE%"=="" set SINCE=%CODEX_RECOVER_SINCE%

echo Scanning local Codex sessions...
echo.

set RESULT=%TEMP%\codex-usage-single\amount.txt
if not exist "%TEMP%\codex-usage-single" mkdir "%TEMP%\codex-usage-single"
"%~dp0bin\codex-usage-windows-amd64.exe" --since "%SINCE%" --total-only --status --price-file "%~dp0model_prices_and_context_window.json" > "%RESULT%"
if errorlevel 1 goto failed
set /p AMOUNT=<"%RESULT%"

echo.
echo Final usage cost: %AMOUNT% USD
echo.
pause
exit /b 0

:failed
echo.
echo Failed to calculate usage.
echo.
pause
exit /b 1
