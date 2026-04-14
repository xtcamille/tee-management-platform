@echo off
echo ========================================================
echo       TEE Management Platform - Full System Launcher
echo ========================================================

echo.
echo [1/3] Starting Enclave Manager Backend (Port 8081)...
start "Enclave Manager" cmd /k "cd enclave-manager && go run main.go"

timeout /t 2 /nobreak >nul

echo [2/3] Starting Enclave Manager Frontend (Port 5174)...
start "Enclave Manager Frontend" cmd /k "cd enclave-manager-frontend && go run main.go"

echo [3/3] Starting Data Connector Proxy Service (Port 8082)...
start "Data Connector Proxy" cmd /k "cd data-connector && go run main.go"

echo.
echo All core services have been launched in separate windows!
echo - Manager API: http://127.0.0.1:8081
echo - Frontend UI: http://127.0.0.1:5174
echo - Data API:    http://127.0.0.1:8082
echo.
pause
