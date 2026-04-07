package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"tee-management-platform/internal/ratls"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <data_path> [enclave_addr]", os.Args[0])
	}

	dataPath := os.Args[1]
	enclaveAddr := "localhost:8443" // Default to local enclave
	if len(os.Args) > 2 {
		enclaveAddr = os.Args[2]
	}

	dataFile, err := os.Open(dataPath)
	if err != nil {
		log.Fatalf("Failed to open data file: %v", err)
	}
	defer dataFile.Close()

	// 1. Read input data
	body, err := io.ReadAll(dataFile)
	if err != nil {
		log.Fatalf("Failed to read data file: %v", err)
	}

	// 2. Setup RA-TLS Client Configuration
	// We'll use a custom VerifyPeerCertificate to perform Remote Attestation
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // We perform custom verification in VerifyPeerCertificate
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return ratls.VerifyPeerCertificate(rawCerts, verifiedChains)
		},
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	// 3. Connect to the Enclave directly over RA-TLS
	url := fmt.Sprintf("https://%s/data", enclaveAddr)
	fmt.Printf("[Data Connector] Connecting to Enclave at %s via RA-TLS...\n", url)

	resp, err := client.Post(url, "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("Secure processing failed (RA-TLS check failed?): %v", err)
	}
	defer resp.Body.Close()

	// 4. Extract and print result
	result, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read secure response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Secure processing failed with status (%v): %s", resp.Status, string(result))
	}

	fmt.Printf("Processing complete via secure RA-TLS channel. Result:\n%s\n", string(result))
}
