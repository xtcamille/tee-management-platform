package main

import (
	"bytes"
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
const pythonExecutionTimeout = 60 * time.Second
const pythonPreflightTimeout = 10 * time.Second
const commandProgressLogInterval = 5 * time.Second
const maxLoggedOutputBytes = 512

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
	if err := runPythonPreflight(pythonPath); err != nil {
		log.Printf("[Enclave App] Python interpreter preflight failed: %v", err)
		return nil, err
	}
	log.Printf(
		"[Enclave App] Executing Python script with interpreter=%s script=%s csv=%s input_bytes=%d",
		pythonPath,
		processScriptPath,
		csvPath,
		len(data),
	)
	output, err := runCommandWithTimeout(
		"[Enclave App] Python script",
		pythonExecutionTimeout,
		pythonPath,
		processScriptPath,
		csvPath,
	)
	if err != nil {
		log.Printf("[Enclave App] Python script execution failed: err=%v output=%s", err, truncateForLog(string(output)))
		return nil, fmt.Errorf("python script failed: %v, output: %s", err, string(output))
	}
	log.Printf("[Enclave App] Python script completed successfully: output_bytes=%d", len(output))

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

func runPythonPreflight(pythonPath string) error {
	log.Printf("[Enclave App] Running Python interpreter preflight: interpreter=%s", pythonPath)
	output, err := runCommandWithTimeout(
		"[Enclave App] Python preflight",
		pythonPreflightTimeout,
		pythonPath,
		"-c",
		`import sys; print("python preflight ok"); print(sys.version)`,
	)
	if err != nil {
		return fmt.Errorf("python preflight failed: %w", err)
	}
	log.Printf("[Enclave App] Python interpreter preflight output: %s", truncateForLog(string(output)))
	return nil
}

func runCommandWithTimeout(logPrefix string, timeout time.Duration, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(),
		"PYTHONUNBUFFERED=1",
		"PYTHONDONTWRITEBYTECODE=1",
	)

	var combinedOutput bytes.Buffer
	cmd.Stdout = &combinedOutput
	cmd.Stderr = &combinedOutput

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command failed: %w", err)
	}

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	log.Printf("%s started: pid=%d command=%s args=%v timeout=%s", logPrefix, pid, name, args, timeout)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(commandProgressLogInterval)
	defer ticker.Stop()

	startedAt := time.Now()

	for {
		select {
		case err := <-done:
			log.Printf("%s finished after %s: pid=%d", logPrefix, time.Since(startedAt), pid)
			return combinedOutput.Bytes(), err
		case <-ticker.C:
			log.Printf(
				"%s still running after %s: pid=%d partial_output=%s",
				logPrefix,
				time.Since(startedAt).Round(time.Second),
				pid,
				truncateForLog(combinedOutput.String()),
			)
		case <-timer.C:
			killErr := error(nil)
			if cmd.Process != nil {
				killErr = cmd.Process.Kill()
			}
			log.Printf(
				"%s timed out after %s: pid=%d kill_err=%v partial_output=%s",
				logPrefix,
				timeout,
				pid,
				killErr,
				truncateForLog(combinedOutput.String()),
			)
			return combinedOutput.Bytes(), fmt.Errorf("command timed out after %s", timeout)
		}
	}
}

func truncateForLog(value string) string {
	if len(value) <= maxLoggedOutputBytes {
		return value
	}
	return value[:maxLoggedOutputBytes] + "...(truncated)"
}
