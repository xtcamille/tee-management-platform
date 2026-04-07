package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"tee-management-platform/enclave-manager/occlum"
)

var codePath string

func UploadCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	tempDir := filepath.Join(os.TempDir(), "tee-code")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	codePath = filepath.Join(tempDir, "process.py")
	if err := os.WriteFile(codePath, body, 0644); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Code uploaded successfully to %s", codePath)
}

func StartEnclave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if codePath == "" {
		http.Error(w, "No code uploaded yet", http.StatusBadRequest)
		return
	}

	// This starts the Go-based Enclave App inside Occlum, which handles RA-TLS
	if err := occlum.Start(codePath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start enclave: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Enclave started successfully. RA-TLS server listening on port 8443.")
}

// ProcessData is now handled directly between Data Connector and Enclave
func ProcessData(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Data processing is now handled directly via RA-TLS on port 8443. Use the Data Connector client.", http.StatusGone)
}
