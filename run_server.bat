@echo off
setlocal EnableExtensions

set "SERVER_DIR=%~dp0"
if "%SERVER_DIR:~-1%"=="\" set "SERVER_DIR=%SERVER_DIR:~0,-1%"

if not defined PORT set "PORT=8080"
if not defined DATABASE_URL set "DATABASE_URL=%SERVER_DIR%\metrochat.db"
if not defined JWT_SECRET set "JWT_SECRET=local-dev-secret-change-me-please-32-chars"
if not defined JWT_ISSUER set "JWT_ISSUER=metrochat"
if not defined ACCESS_TOKEN_TTL set "ACCESS_TOKEN_TTL=0"
if not defined REFRESH_TOKEN_TTL set "REFRESH_TOKEN_TTL=2592000"

set "BIN_PATH="
if defined BIN_NAME if exist "%SERVER_DIR%\%BIN_NAME%" set "BIN_PATH=%SERVER_DIR%\%BIN_NAME%"
if not defined BIN_PATH if exist "%SERVER_DIR%\ocserver_windows_amd64.exe" set "BIN_PATH=%SERVER_DIR%\ocserver_windows_amd64.exe"

if not defined BIN_PATH (
  echo Server binary not found. Build it with: .\build.ps1
  exit /b 1
)

echo Starting server on port %PORT%...
call "%BIN_PATH%"
set "EXIT_CODE=%ERRORLEVEL%"

endlocal & exit /b %EXIT_CODE%
