package main

import (
	"bytes"
	"encoding/json"
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

	var uploadResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		log.Fatalf("Failed to decode upload response: %v", err)
	}
	taskID := uploadResp["task_id"]
	fmt.Printf("Code uploaded successfully! Task ID: %s\n", taskID)

	// Now start the enclave
	startReqBody, _ := json.Marshal(map[string]string{"task_id": taskID})
	respStart, err := http.Post("http://192.168.0.248:8081/start-enclave", "application/json", bytes.NewBuffer(startReqBody))
	if err != nil {
		log.Fatalf("Failed to start enclave: %v", err)
	}
	defer respStart.Body.Close()

	if respStart.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(respStart.Body)
		log.Fatalf("Enclave start failed: %v, Body: %s", respStart.Status, string(respBody))
	}

	var startResp map[string]interface{}
	if err := json.NewDecoder(respStart.Body).Decode(&startResp); err != nil {
		log.Fatalf("Failed to decode start response: %v", err)
	}
	portFloat, ok := startResp["port"].(float64)
	if !ok {
		log.Fatalf("Missing or invalid port in response: %v", startResp)
	}
	fmt.Printf("Enclave started successfully on the platform! RA-TLS Connection Port: %d\n", int(portFloat))
}
