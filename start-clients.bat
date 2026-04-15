@echo off
setlocal

if "%MANAGER_PORT%"=="" set MANAGER_PORT=8081
if "%FRONTEND_PORT%"=="" set FRONTEND_PORT=5174
if "%DATA_CONNECTOR_PORT%"=="" set DATA_CONNECTOR_PORT=8082
if "%MANAGER_BASE_URL%"=="" set MANAGER_BASE_URL=http://127.0.0.1:%MANAGER_PORT%
if "%RATLS_PROBE_HOST%"=="" set RATLS_PROBE_HOST=127.0.0.1
if "%ENCLAVE_HOST%"=="" set ENCLAVE_HOST=%RATLS_PROBE_HOST%
if "%DATA_CONNECTOR_BASE_URL%"=="" set DATA_CONNECTOR_BASE_URL=http://127.0.0.1:%DATA_CONNECTOR_PORT%
if "%DISPLAY_HOST%"=="" set DISPLAY_HOST=127.0.0.1

echo ========================================================
echo       TEE Management Platform - Full System Launcher
echo ========================================================

echo.
echo [1/3] Starting Enclave Manager Backend (Port 8081)...
start "Enclave Manager" cmd /k "cd enclave-manager && set PORT=%MANAGER_PORT% && go run main.go"

timeout /t 2 /nobreak >nul

echo [2/3] Starting Enclave Manager Frontend (Port 5174)...
start "Enclave Manager Frontend" cmd /k "cd enclave-manager-frontend && set PORT=%FRONTEND_PORT% && go run main.go"

echo [3/3] Starting Data Connector Proxy Service (Port 8082)...
start "Data Connector Proxy" cmd /k "cd data-connector && set PORT=%DATA_CONNECTOR_PORT% && go run main.go"

echo.
echo All core services have been launched in separate windows!
echo - Manager API: http://%DISPLAY_HOST%:%MANAGER_PORT%
echo - Frontend UI: http://%DISPLAY_HOST%:%FRONTEND_PORT%
echo - Data API:    http://%DISPLAY_HOST%:%DATA_CONNECTOR_PORT%
echo.
echo Runtime wiring:
echo - MANAGER_BASE_URL=%MANAGER_BASE_URL%
echo - RATLS_PROBE_HOST=%RATLS_PROBE_HOST%
echo - ENCLAVE_HOST=%ENCLAVE_HOST%
echo - DATA_CONNECTOR_BASE_URL=%DATA_CONNECTOR_BASE_URL%
echo.
pause
