package main

import (
	"crypto/tls"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"tee-management-platform/internal/ratls"
	"time"
)

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

	result, err := processCSV(data)
	if err != nil {
		log.Printf("[Enclave App] CSV processing failed: %v", err)
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

func processCSV(data []byte) ([]byte, error) {
	log.Printf("[Enclave App] Starting native CSV processing pipeline for %d input bytes", len(data))

	rows, err := parseCSVRows(data)
	if err != nil {
		return nil, err
	}
	log.Printf("[Enclave App] Parsed %d CSV data rows", len(rows))

	report, err := buildReport(rows)
	if err != nil {
		return nil, err
	}
	log.Printf("[Enclave App] Generated native CSV analysis report: output_bytes=%d", len(report))
	return []byte(report), nil
}

func parseCSVRows(data []byte) ([]map[string]string, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse csv failed: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv must contain a header row and at least one data row")
	}

	headers := make([]string, len(records[0]))
	for i, header := range records[0] {
		headers[i] = strings.TrimPrefix(strings.TrimSpace(header), "\ufeff")
	}

	rows := make([]map[string]string, 0, len(records)-1)
	for rowIndex, record := range records[1:] {
		if len(record) != len(headers) {
			return nil, fmt.Errorf("csv row %d has %d columns, expected %d", rowIndex+2, len(record), len(headers))
		}

		row := make(map[string]string, len(headers))
		for i, header := range headers {
			row[header] = strings.TrimSpace(record[i])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func buildReport(rows []map[string]string) (string, error) {
	total := len(rows)
	if total == 0 {
		return "", fmt.Errorf("csv contains no data rows")
	}

	classCounts := make(map[string]int)
	majorCounts := make(map[string]int)
	genderCounts := make(map[string]int)
	totalAge := 0

	for index, row := range rows {
		classCounts[row["class"]]++
		majorCounts[row["major"]]++
		genderCounts[row["gender"]]++

		age, err := strconv.Atoi(row["age"])
		if err != nil {
			return "", fmt.Errorf("invalid age at row %d: %w", index+2, err)
		}
		totalAge += age
	}

	averageAge := float64(totalAge) / float64(total)
	reportLines := []string{
		"========================================",
		"      Student Data Analysis Report      ",
		"========================================",
		fmt.Sprintf("Total Students: %d", total),
		fmt.Sprintf("Average Age: %.1f", averageAge),
		"",
		"Students by Class:",
	}
	reportLines = append(reportLines, formatCounterLines(classCounts)...)
	reportLines = append(reportLines, "")
	reportLines = append(reportLines, "Students by Major:")
	reportLines = append(reportLines, formatCounterLines(majorCounts)...)
	reportLines = append(reportLines, "")
	reportLines = append(reportLines, "Gender Distribution:")
	reportLines = append(reportLines, formatPercentageLines(genderCounts, total)...)
	reportLines = append(reportLines, "========================================")

	return strings.Join(reportLines, "\n"), nil
}

func formatCounterLines(counter map[string]int) []string {
	keys := make([]string, 0, len(counter))
	for key := range counter {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("  %s: %d", key, counter[key]))
	}
	return lines
}

func formatPercentageLines(counter map[string]int, total int) []string {
	keys := make([]string, 0, len(counter))
	for key := range counter {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		percentage := float64(counter[key]) / float64(total) * 100
		lines = append(lines, fmt.Sprintf("  %s: %d (%.1f%%)", key, counter[key], percentage))
	}
	return lines
}
