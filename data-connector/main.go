package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"tee-management-platform/internal/ratls"
	"time"
)

func main() {
	port := getenv("PORT", "8082")
	fmt.Printf("[Data Connector Service] Starting HTTP server on port %s...\n", port)

	http.HandleFunc("/forward", handleForward)

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleForward(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Only POST is allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(50 << 20) // 50MB
	if err != nil {
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}

	taskId := r.FormValue("task_id")
	if taskId == "" {
		http.Error(w, "Missing task_id form value", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file form value", http.StatusBadRequest)
		return
	}
	defer file.Close()

	body, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file contents", http.StatusInternalServerError)
		return
	}

	// Dynamic lookup
	managerBaseURL := strings.TrimRight(getenv("MANAGER_BASE_URL", "http://127.0.0.1:8081"), "/")
	managerAPI := managerBaseURL + "/task-status?task_id=" + url.QueryEscape(taskId)
	fmt.Printf("[Data Connector Backend] Querying %s...\n", managerAPI)

	enclaveHost := getenv("ENCLAVE_HOST", deriveHostFromURL(managerBaseURL, "127.0.0.1"))

	targetUrl := ""
	for {
		statusResp, err := http.Get(managerAPI)
		if err != nil || statusResp.StatusCode != 200 {
			http.Error(w, "Failed to fetch task status. Manager down?", http.StatusInternalServerError)
			return
		}

		var taskInfo struct {
			Port   int    `json:"port"`
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.NewDecoder(statusResp.Body).Decode(&taskInfo); err != nil {
			http.Error(w, "Failed to decode manager JSON", http.StatusInternalServerError)
			return
		}
		statusResp.Body.Close()

		if taskInfo.Status == "FAILED" {
			http.Error(w, fmt.Sprintf("Task failed to start: %s", taskInfo.Error), http.StatusInternalServerError)
			return
		}
		if taskInfo.Status == "DONE" {
			http.Error(w, "Task is already DONE", http.StatusConflict)
			return
		}
		if taskInfo.Status == "ENCLAVE_RUNNING" && taskInfo.Port != 0 {
			targetUrl = fmt.Sprintf("https://%s:%d/data", enclaveHost, taskInfo.Port)
			fmt.Printf("[Data Connector Backend] Target resolved to: %s\n", targetUrl)
			break
		}

		fmt.Printf("[Data Connector Backend] Task %s status '%s', waiting...\n", taskId, taskInfo.Status)
		time.Sleep(2 * time.Second)
	}

	// Connect to Enclave over RA-TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return ratls.VerifyPeerCertificate(rawCerts, verifiedChains)
		},
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	fmt.Printf("[Data Connector Backend] Forwarding data to Enclave...\n")
	resp, err := client.Post(targetUrl, "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		fmt.Printf("[Data Connector Backend] Enclave POST Error: %v\n", err)
		http.Error(w, fmt.Sprintf("Communication with Enclave failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	result, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response from Enclave", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	w.Write(result)
	fmt.Printf("[Data Connector Backend] Success! Forwarded result to client.\n")
}

func getenv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func deriveHostFromURL(rawURL string, fallback string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return fallback
	}
	return parsed.Hostname()
}
