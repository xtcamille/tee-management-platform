package main

import (
	"log"
	"net/http"
	"tee-management-platform/enclave-manager/handler"
)

func main() {
	mux := http.NewServeMux()

	// Upload processing code (e.g., Go app source or binary)
	mux.HandleFunc("/upload-code", handler.UploadCode)

	// Start the Enclave with the uploaded code
	mux.HandleFunc("/start-enclave", handler.StartEnclave)

	// Send data to the running Enclave and get result
	mux.HandleFunc("/process-data", handler.ProcessData)

	log.Println("TEE Management Platform Server starting on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
