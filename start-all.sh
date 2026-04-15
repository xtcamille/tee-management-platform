#!/bin/bash

set -e

echo "========================================================"
echo "      TEE Management Platform - Full System Launcher"
echo "========================================================"
echo ""

MANAGER_PORT="${MANAGER_PORT:-8081}"
FRONTEND_PORT="${FRONTEND_PORT:-5174}"
DATA_CONNECTOR_PORT="${DATA_CONNECTOR_PORT:-8082}"

# Internal service routing. Override these when manager / enclave / connector
# do not live on the same host or network namespace.
MANAGER_BASE_URL="${MANAGER_BASE_URL:-http://127.0.0.1:${MANAGER_PORT}}"
RATLS_PROBE_HOST="${RATLS_PROBE_HOST:-127.0.0.1}"
ENCLAVE_HOST="${ENCLAVE_HOST:-$RATLS_PROBE_HOST}"
DATA_CONNECTOR_BASE_URL="${DATA_CONNECTOR_BASE_URL:-http://127.0.0.1:${DATA_CONNECTOR_PORT}}"

# What we print to humans. Override if you want startup logs to show a public IP
# or domain instead of localhost.
DISPLAY_HOST="${DISPLAY_HOST:-127.0.0.1}"

export PORT
export MANAGER_BASE_URL
export MANAGER_URL="${MANAGER_URL:-$MANAGER_BASE_URL}"
export DATA_CONNECTOR_BASE_URL
export RATLS_PROBE_HOST
export ENCLAVE_HOST

# Function to clean up background processes on script exit
cleanup() {
    echo ""
    echo "Stopping all services..."
    # Suppress output if processes are already gone
    kill $MANAGER_PID $FRONTEND_PID $DATA_PID 2>/dev/null
    
    # Remove the trap so we don't infinitely loop on exit
    trap - SIGINT SIGTERM EXIT
    exit 0
}

# Catch Ctrl+C and script exit
trap cleanup SIGINT SIGTERM EXIT

echo "[1/3] Starting Enclave Manager Backend (Port 8081)..."
cd enclave-manager
PORT="$MANAGER_PORT" go run main.go &
MANAGER_PID=$!
cd ..

# Wait for the manager to initialize
sleep 2

echo "[2/3] Starting Enclave Manager Frontend (Port 5174)..."
cd enclave-manager-frontend
PORT="$FRONTEND_PORT" go run main.go &
FRONTEND_PID=$!
cd ..

echo "[3/3] Starting Data Connector Proxy Service (Port 8082)..."
cd data-connector
PORT="$DATA_CONNECTOR_PORT" go run main.go &
DATA_PID=$!
cd ..

echo ""
echo "All core services have been launched in the background!"
echo "- Manager API: http://${DISPLAY_HOST}:${MANAGER_PORT}"
echo "- Frontend UI: http://${DISPLAY_HOST}:${FRONTEND_PORT}"
echo "- Data API:    http://${DISPLAY_HOST}:${DATA_CONNECTOR_PORT}"
echo ""
echo "Runtime wiring:"
echo "- MANAGER_BASE_URL=${MANAGER_BASE_URL}"
echo "- MANAGER_URL=${MANAGER_URL}"
echo "- RATLS_PROBE_HOST=${RATLS_PROBE_HOST}"
echo "- ENCLAVE_HOST=${ENCLAVE_HOST}"
echo "- DATA_CONNECTOR_BASE_URL=${DATA_CONNECTOR_BASE_URL}"
echo ""
echo "Press Ctrl+C at any time to gracefully stop all services."

# Wait indefinitely for processes
wait $MANAGER_PID $FRONTEND_PID $DATA_PID
