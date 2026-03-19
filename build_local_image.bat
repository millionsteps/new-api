@echo off
setlocal
cd /d "%~dp0"
echo ========================================
echo new-api local image build
echo ========================================
echo Repo: %~dp0
echo.
powershell -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0build_local_image.ps1" %*
if errorlevel 1 (
  echo.
  echo Build failed.
  echo Check logs under: %~dp0logs
  pause
  exit /b 1
)
echo.
echo Build finished.
echo Check logs under: %~dp0logs
pause
