#!/bin/bash

echo "========================================================"
echo "      TEE Management Platform - Full System Launcher"
echo "========================================================"
echo ""

# Function to clean up background processes on script exit
cleanup() {
    echo ""
    echo "Stopping all services..."
    kill $MANAGER_PID $FRONTEND_PID $DATA_PID 2>/dev/null
    exit
}

# Catch Ctrl+C and termination signals to run cleanup
trap cleanup SIGINT SIGTERM

echo "[1/3] Starting Enclave Manager Backend (Port 8081)..."
(cd enclave-manager && go run main.go) &
MANAGER_PID=$!

# Wait for the manager to initialize
sleep 2

echo "[2/3] Starting Enclave Manager Frontend (Port 5174)..."
(cd enclave-manager-frontend && go run main.go) &
FRONTEND_PID=$!

echo "[3/3] Starting Data Connector Proxy Service (Port 8082)..."
(cd data-connector && go run main.go) &
DATA_PID=$!

echo ""
echo "All core services have been launched in the background!"
echo "- Manager API: http://127.0.0.1:8081"
echo "- Frontend UI: http://127.0.0.1:5174"
echo "- Data API:    http://127.0.0.1:8082"
echo ""
echo "Press Ctrl+C at any time to gracefully stop all services."

# Wait indefinitely for processes
wait $MANAGER_PID $FRONTEND_PID $DATA_PID
