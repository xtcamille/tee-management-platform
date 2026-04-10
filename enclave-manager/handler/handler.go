package handler

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"tee-management-platform/enclave-manager/occlum"
)

var uploadedCodePath string

func UploadCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	tempDir := filepath.Join(os.TempDir(), "tee-code")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read zip: %v", err), http.StatusBadRequest)
		return
	}

	var extractedFile io.ReadCloser
	for _, f := range zipReader.File {
		if !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to open file in zip: %v", err), http.StatusInternalServerError)
				return
			}
			extractedFile = rc
			break
		}
	}

	if extractedFile == nil {
		http.Error(w, "No file found in zip archive", http.StatusBadRequest)
		return
	}
	defer extractedFile.Close()

	uploadedCodePath = filepath.Join(tempDir, "uploaded_code")
	out, err := os.OpenFile(uploadedCodePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		http.Error(w, "Failed to create extracted file", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, extractedFile); err != nil {
		http.Error(w, "Failed to write extracted file", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Code uploaded and extracted successfully to %s", uploadedCodePath)
}

func StartEnclave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if uploadedCodePath == "" {
		http.Error(w, "No code uploaded yet", http.StatusBadRequest)
		return
	}

	// This starts the Go-based Enclave App inside Occlum, which handles RA-TLS
	if err := occlum.Start(uploadedCodePath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start enclave: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Enclave started successfully. RA-TLS server listening on port 8443.")
}

// ProcessData is now handled directly between Data Connector and Enclave
func ProcessData(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Data processing is now handled directly via RA-TLS on port 8443. Use the Data Connector client.", http.StatusGone)
}
