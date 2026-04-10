package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"tee-management-platform/enclave-manager/occlum"
	"time"
)

var taskMap sync.Map // maps taskID string -> *TaskInfo

type TaskState string

const (
	StateCodeUploaded TaskState = "CODE_UPLOADED"
	StateStarting     TaskState = "STARTING_ENCLAVE"
	StateRunning      TaskState = "ENCLAVE_RUNNING"
	StateDataReceived TaskState = "DATA_RECEIVED"
	StateDone         TaskState = "DONE"
	StateFailed       TaskState = "FAILED"
)

type TaskInfo struct {
	ID       string    `json:"task_id"`
	Status   TaskState `json:"status"`
	CodePath string    `json:"-"`
	Port     int       `json:"port,omitempty"`
	Error    string    `json:"error,omitempty"`
}

func UploadCode(w http.ResponseWriter, r *http.Request) {
	log.Printf("[UploadCode] Received POST request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("[UploadCode] Error reading body: %v", err)
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	log.Printf("[UploadCode] Read %d bytes from request body", len(body))

	taskID := fmt.Sprintf("%d", time.Now().UnixNano())
	tempDir := filepath.Join(os.TempDir(), "tee-code", taskID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("[UploadCode] Error creating temp dir %s: %v", tempDir, err)
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}
	log.Printf("[UploadCode] Temp directory ensured: %s", tempDir)

	gzr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		log.Printf("[UploadCode] Error creating gzip reader: %v", err)
		http.Error(w, fmt.Sprintf("Failed to read gzip: %v", err), http.StatusBadRequest)
		return
	}
	defer gzr.Close()
	log.Printf("[UploadCode] Gzip reader created successfully")

	tr := tar.NewReader(gzr)

	uploadedCodePath := filepath.Join(tempDir, "uploaded_code")
	out, err := os.OpenFile(uploadedCodePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		log.Printf("[UploadCode] Error opening output file %s: %v", uploadedCodePath, err)
		http.Error(w, "Failed to create extracted file", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			log.Printf("[UploadCode] Reached end of tar archive")
			break
		}
		if err != nil {
			log.Printf("[UploadCode] Error reading tar header: %v", err)
			http.Error(w, fmt.Sprintf("Failed to read tar: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("[UploadCode] Found tar entry: %s, Typeflag: %c", header.Name, header.Typeflag)

		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			log.Printf("[UploadCode] Extracting file: %s", header.Name)
			if _, err := io.Copy(out, tr); err != nil {
				log.Printf("[UploadCode] Error writing extracted file %s: %v", header.Name, err)
				http.Error(w, "Failed to write extracted file", http.StatusInternalServerError)
				return
			}
			found = true
			log.Printf("[UploadCode] Extraction successful")
			break
		}
	}

	if !found {
		log.Printf("[UploadCode] No regular file found in tar.gz archive")
		http.Error(w, "No file found in tar.gz archive", http.StatusBadRequest)
		return
	}

	taskInfo := &TaskInfo{
		ID:       taskID,
		Status:   StateCodeUploaded,
		CodePath: uploadedCodePath,
	}
	taskMap.Store(taskID, taskInfo)
	log.Printf("[UploadCode] Finished processing completely: saved to %s for task %s", uploadedCodePath, taskID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"task_id": taskID,
		"status":  "success",
		"message": fmt.Sprintf("Code uploaded and extracted successfully to %s", uploadedCodePath),
	})
}

type StartRequest struct {
	TaskID string `json:"task_id"`
}

func StartEnclave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.TaskID == "" {
		http.Error(w, "Missing task_id", http.StatusBadRequest)
		return
	}

	taskInfoAny, ok := taskMap.Load(req.TaskID)
	if !ok {
		http.Error(w, "Invalid or expired task_id", http.StatusBadRequest)
		return
	}
	taskInfo := taskInfoAny.(*TaskInfo)

	taskInfo.Status = StateStarting

	// This starts the Go-based Enclave App inside Occlum, which handles RA-TLS
	port, err := occlum.Start(req.TaskID, taskInfo.CodePath)
	if err != nil {
		taskInfo.Status = StateFailed
		taskInfo.Error = err.Error()
		http.Error(w, fmt.Sprintf("Failed to start enclave: %v", err), http.StatusInternalServerError)
		return
	}

	taskInfo.Status = StateRunning
	taskInfo.Port = port

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"task_id": req.TaskID,
		"port":    port,
	})
}

func GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		http.Error(w, "Missing task_id", http.StatusBadRequest)
		return
	}

	taskInfoAny, ok := taskMap.Load(taskID)
	if !ok {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	taskInfo := taskInfoAny.(*TaskInfo)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(taskInfo)
}

type CallbackRequest struct {
	TaskID string    `json:"task_id"`
	Status TaskState `json:"status"`
	Error  string    `json:"error,omitempty"`
}

func UpdateTaskCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	taskInfoAny, ok := taskMap.Load(req.TaskID)
	if !ok {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	taskInfo := taskInfoAny.(*TaskInfo)

	taskInfo.Status = req.Status
	if req.Error != "" {
		taskInfo.Error = req.Error
	}

	log.Printf("[Callback] Task %s reached status %s", req.TaskID, req.Status)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// ProcessData is now handled directly between Data Connector and Enclave
func ProcessData(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Data processing is now handled directly via RA-TLS on port 8443. Use the Data Connector client.", http.StatusGone)
}
