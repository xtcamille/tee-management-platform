package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"tee-management-platform/internal/ratls"
)

func main() {
	fmt.Println("Enclave Application starting with RA-TLS...")

	// 1. Generate RA-TLS Certificate
	// Simulation mode is enabled for development
	cert, err := ratls.GenerateCertificate(true)
	if err != nil {
		log.Fatalf("Failed to generate cert: %v", err)
	}

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

	fmt.Println("RA-TLS Server listening on :8443")
	if err := server.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleSecureData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("Received encrypted data via RA-TLS channel")

	// Read and process data
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// For demonstration, let's process it using Python (matching existing workflow)
	// Or we can just process it directly in Go
	result, err := processWithPython(data)
	if err != nil {
		fmt.Fprintf(w, "Error during processing: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(result)
}

func processWithPython(data []byte) ([]byte, error) {
	// Call the uploaded process.py (it should be at /bin/process.py inside Occlum)
	cmd := exec.Command("python3", "/bin/process.py")
	
	// Set stdin to the received data
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	go func() {
		defer stdinPipe.Close()
		stdinPipe.Write(data)
	}()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python script failed: %v, output: %s", err, string(output))
	}

	return output, err
}
