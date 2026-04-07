package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: code-connector <script_path>")
	}

	scriptPath := os.Args[1]
	scriptFile, err := os.Open(scriptPath)
	if err != nil {
		log.Fatalf("Failed to open script: %v", err)
	}
	defer scriptFile.Close()

	// Read and upload the script
	body, err := io.ReadAll(scriptFile)
	if err != nil {
		log.Fatalf("Failed to read script: %v", err)
	}

	resp, err := http.Post("http://192.168.0.248:8081/upload-code", "application/octet-stream", bytes.NewBuffer(body))
	if err != nil {
		log.Fatalf("Upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Upload failed with status: %v", resp.Status)
	}

	fmt.Println("Code uploaded successfully!")

	// Now start the enclave
	respStart, err := http.Post("http://192.168.0.248:8081/start-enclave", "application/json", nil)
	if err != nil {
		log.Fatalf("Failed to start enclave: %v", err)
	}
	defer respStart.Body.Close()

	if respStart.StatusCode != http.StatusOK {
		log.Fatalf("Enclave start failed: %v", respStart.Status)
	}

	fmt.Println("Enclave started successfully on the platform!")
}
