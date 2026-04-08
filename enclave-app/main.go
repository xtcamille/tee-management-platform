package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"tee-management-platform/internal/ratls"
	"time"
)

const processScriptPath = "/bin/process.py"

func main() {
	log.Println("[Enclave App] Starting enclave application with RA-TLS")

	// 1. Generate RA-TLS Certificate
	// Simulation mode is enabled for development
	log.Println("[Enclave App] Generating RA-TLS certificate in simulation mode")
	cert, err := ratls.GenerateCertificate(true)
	if err != nil {
		log.Fatalf("[Enclave App] Failed to generate RA-TLS certificate: %v", err)
	}
	log.Println("[Enclave App] RA-TLS certificate generated successfully")

	// 2. Start TLS Server
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/data", handleSecureData)

	server := &http.Server{
		Addr:      ":8443",
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	log.Printf("[Enclave App] RA-TLS server listening on %s", server.Addr)
	if err := server.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("[Enclave App] RA-TLS server exited with error: %v", err)
	}
}

func handleSecureData(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()

	if r.Method != http.MethodPost {
		log.Printf("[Enclave App] Rejected request: method=%s path=%s remote=%s", r.Method, r.URL.Path, r.RemoteAddr)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Printf(
		"[Enclave App] Received secure request: method=%s path=%s remote=%s content_type=%s content_length=%d",
		r.Method,
		r.URL.Path,
		r.RemoteAddr,
		r.Header.Get("Content-Type"),
		r.ContentLength,
	)

	// Read and process data
	data, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[Enclave App] Failed to read request body: remote=%s err=%v", r.RemoteAddr, err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("[Enclave App] Read %d bytes from secure request body", len(data))

	// For demonstration, let's process it using Python (matching existing workflow)
	// Or we can just process it directly in Go
	result, err := processWithPython(data)
	if err != nil {
		log.Printf("[Enclave App] Python processing failed: %v", err)
		fmt.Fprintf(w, "Error during processing: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := w.Write(result); err != nil {
		log.Printf("[Enclave App] Failed to write response body: err=%v", err)
		return
	}
	log.Printf("[Enclave App] Successfully returned %d bytes in %s", len(result), time.Since(startedAt))
}

func processWithPython(data []byte) ([]byte, error) {
	log.Printf("[Enclave App] Starting Python processing pipeline for %d input bytes", len(data))

	csvPath, err := writeCSVInput(data)
	if err != nil {
		log.Printf("[Enclave App] Failed to persist CSV input: %v", err)
		return nil, err
	}
	defer func() {
		if err := os.Remove(csvPath); err != nil {
			log.Printf("[Enclave App] Failed to remove temporary CSV file %s: %v", csvPath, err)
			return
		}
		log.Printf("[Enclave App] Removed temporary CSV file %s", csvPath)
	}()

	pythonPath := discoverPythonPath()
	if _, err := os.Stat(processScriptPath); err != nil {
		log.Printf("[Enclave App] Python script is unavailable: script=%s err=%v", processScriptPath, err)
		return nil, fmt.Errorf("python script not found: %w", err)
	}
	log.Printf(
		"[Enclave App] Executing Python script with interpreter=%s script=%s csv=%s input_bytes=%d",
		pythonPath,
		processScriptPath,
		csvPath,
		len(data),
	)
	cmd := exec.Command(pythonPath, processScriptPath, csvPath)
	startedAt := time.Now()

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf(
			"[Enclave App] Python script execution failed after %s: err=%v output=%s",
			time.Since(startedAt),
			err,
			string(output),
		)
		return nil, fmt.Errorf("python script failed: %v, output: %s", err, string(output))
	}
	log.Printf("[Enclave App] Python script completed successfully in %s: output_bytes=%d", time.Since(startedAt), len(output))

	return output, err
}

func writeCSVInput(data []byte) (string, error) {
	tempFile, err := os.CreateTemp("", "secure-input-*.csv")
	if err != nil {
		return "", fmt.Errorf("create temp csv file failed: %w", err)
	}

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return "", fmt.Errorf("write temp csv file failed: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("close temp csv file failed: %w", err)
	}

	csvPath := tempFile.Name()
	log.Printf("[Enclave App] Stored secure CSV input at %s (%d bytes)", filepath.Clean(csvPath), len(data))
	return csvPath, nil
}

func discoverPythonPath() string {
	configuredPath, err := os.ReadFile("/etc/python3_path")
	if err == nil {
		if path := strings.TrimSpace(string(configuredPath)); path != "" {
			log.Printf("[Enclave App] Using configured Python interpreter: %s", path)
			return path
		}
		log.Println("[Enclave App] /etc/python3_path exists but is empty, trying fallback locations")
	} else {
		log.Printf("[Enclave App] Unable to read /etc/python3_path, trying fallback locations: %v", err)
	}

	// Fall back to common interpreter locations if the config file is missing.
	for _, candidate := range []string{"/usr/bin/python3", "/usr/local/bin/python3", "/bin/python3", "python3"} {
		if _, err := os.Stat(candidate); err == nil || candidate == "python3" {
			log.Printf("[Enclave App] Using fallback Python interpreter: %s", candidate)
			return candidate
		}
	}
	log.Println("[Enclave App] Falling back to python3 from PATH")
	return "python3"
}
