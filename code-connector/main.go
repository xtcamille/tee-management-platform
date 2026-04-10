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
		log.Fatal("Usage: code-connector <code_path>")
	}

	codePath := os.Args[1]
	codeFile, err := os.Open(codePath)
	if err != nil {
		log.Fatalf("Failed to open code: %v", err)
	}
	defer codeFile.Close()

	// Read and upload the code
	body, err := io.ReadAll(codeFile)
	if err != nil {
		log.Fatalf("Failed to read code: %v", err)
	}

	resp, err := http.Post("http://192.168.0.248:8081/upload-code", "application/octet-stream", bytes.NewBuffer(body))
	if err != nil {
		log.Fatalf("Upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Fatalf("Upload failed with status: %v, Body: %s", resp.Status, string(respBody))
	}

	fmt.Println("Code uploaded successfully!")

	// Now start the enclave
	respStart, err := http.Post("http://192.168.0.248:8081/start-enclave", "application/json", nil)
	if err != nil {
		log.Fatalf("Failed to start enclave: %v", err)
	}
	defer respStart.Body.Close()

	if respStart.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(respStart.Body)
		log.Fatalf("Enclave start failed: %v, Body: %s", respStart.Status, string(respBody))
	}

	fmt.Println("Enclave started successfully on the platform!")
}
