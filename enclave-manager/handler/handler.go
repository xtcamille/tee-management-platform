package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"tee-management-platform/enclave-manager/occlum"
)

var uploadedCodePath string

func UploadCode(w http.ResponseWriter, r *http.Request) {
	log.Printf("[UploadCode] Received POST request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("[UploadCode] Error reading body: %v", err)
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	log.Printf("[UploadCode] Read %d bytes from request body", len(body))

	tempDir := filepath.Join(os.TempDir(), "tee-code")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("[UploadCode] Error creating temp dir %s: %v", tempDir, err)
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}
	log.Printf("[UploadCode] Temp directory ensured: %s", tempDir)

	gzr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		log.Printf("[UploadCode] Error creating gzip reader: %v", err)
		http.Error(w, fmt.Sprintf("Failed to read gzip: %v", err), http.StatusBadRequest)
		return
	}
	defer gzr.Close()
	log.Printf("[UploadCode] Gzip reader created successfully")

	tr := tar.NewReader(gzr)

	uploadedCodePath = filepath.Join(tempDir, "uploaded_code")
	out, err := os.OpenFile(uploadedCodePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		log.Printf("[UploadCode] Error opening output file %s: %v", uploadedCodePath, err)
		http.Error(w, "Failed to create extracted file", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			log.Printf("[UploadCode] Reached end of tar archive")
			break
		}
		if err != nil {
			log.Printf("[UploadCode] Error reading tar header: %v", err)
			http.Error(w, fmt.Sprintf("Failed to read tar: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("[UploadCode] Found tar entry: %s, Typeflag: %c", header.Name, header.Typeflag)

		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			log.Printf("[UploadCode] Extracting file: %s", header.Name)
			if _, err := io.Copy(out, tr); err != nil {
				log.Printf("[UploadCode] Error writing extracted file %s: %v", header.Name, err)
				http.Error(w, "Failed to write extracted file", http.StatusInternalServerError)
				return
			}
			found = true
			log.Printf("[UploadCode] Extraction successful")
			break
		}
	}

	if !found {
		log.Printf("[UploadCode] No regular file found in tar.gz archive")
		http.Error(w, "No file found in tar.gz archive", http.StatusBadRequest)
		return
	}

	log.Printf("[UploadCode] Finished processing completely: saved to %s", uploadedCodePath)
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
