package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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
	mu       sync.RWMutex       `json:"-"`
	ID       string             `json:"task_id"`
	Status   TaskState          `json:"status"`
	CodePath string             `json:"-"`
	Port     int                `json:"port,omitempty"`
	Error    string             `json:"error,omitempty"`
	StopFunc context.CancelFunc `json:"-"`
	Version  int64              `json:"-"`
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

	// If a user restarts a task that hasn't finished, forcefully kill the running process first.
	taskInfo.mu.Lock()
	existingStop := taskInfo.StopFunc
	taskInfo.StopFunc = nil
	taskInfo.Version++
	startVersion := taskInfo.Version
	taskInfo.Status = StateStarting
	taskInfo.Error = ""
	taskInfo.Port = 0
	taskInfo.mu.Unlock()

	if existingStop != nil {
		log.Printf("[StartEnclave] Task %s is being restarted. Shutting down existing enclave process.", req.TaskID)
		existingStop()
	}

	// This starts the Go-based Enclave App inside Occlum, which handles RA-TLS
	taskInfo.mu.RLock()
	codePath := taskInfo.CodePath
	taskInfo.mu.RUnlock()

	port, cancel, readinessCh, err := occlum.Start(req.TaskID, codePath)
	if err != nil {
		taskInfo.mu.Lock()
		taskInfo.Status = StateFailed
		taskInfo.Error = err.Error()
		taskInfo.Port = 0
		taskInfo.mu.Unlock()
		http.Error(w, fmt.Sprintf("Failed to start enclave: %v", err), http.StatusInternalServerError)
		return
	}

	taskInfo.mu.Lock()
	taskInfo.Status = StateStarting
	taskInfo.Port = port
	taskInfo.Error = ""
	taskInfo.StopFunc = cancel
	taskInfo.mu.Unlock()

	go trackRATLSReadiness(req.TaskID, taskInfo, startVersion, port, readinessCh)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "starting",
		"task_id": req.TaskID,
		"port":    port,
	})
}

func trackRATLSReadiness(taskID string, taskInfo *TaskInfo, startVersion int64, port int, readinessCh <-chan error) {
	err, ok := <-readinessCh
	if !ok {
		return
	}

	taskInfo.mu.Lock()
	defer taskInfo.mu.Unlock()

	if taskInfo.Version != startVersion {
		log.Printf("[StartEnclave] Ignoring stale RA-TLS readiness result for task %s (version=%d current=%d)", taskID, startVersion, taskInfo.Version)
		return
	}

	if err != nil {
		taskInfo.Status = StateFailed
		taskInfo.Error = err.Error()
		taskInfo.Port = port
		stopFunc := taskInfo.StopFunc
		taskInfo.StopFunc = nil
		if stopFunc != nil {
			go stopFunc()
		}
		log.Printf("[StartEnclave] Task %s failed before RA-TLS became reachable: %v", taskID, err)
		return
	}

	taskInfo.Status = StateRunning
	taskInfo.Port = port
	taskInfo.Error = ""
	log.Printf("[StartEnclave] Task %s RA-TLS is reachable. Status -> %s", taskID, StateRunning)
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
	json.NewEncoder(w).Encode(taskInfo.snapshot())
}

func (t *TaskInfo) snapshot() *TaskInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return &TaskInfo{
		ID:       t.ID,
		Status:   t.Status,
		CodePath: t.CodePath,
		Port:     t.Port,
		Error:    t.Error,
	}
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

	taskInfo.mu.Lock()
	taskInfo.Status = req.Status
	if req.Error != "" {
		taskInfo.Error = req.Error
	}
	stopFunc := taskInfo.StopFunc
	if req.Status == StateDone || req.Status == StateFailed {
		taskInfo.StopFunc = nil
	}
	taskInfo.mu.Unlock()

	log.Printf("[Callback] Task %s reached status %s", req.TaskID, req.Status)

	if (req.Status == StateDone || req.Status == StateFailed) && stopFunc != nil {
		log.Printf("[Callback] Final state reached. Instructing enclave to shutdown for task %s", req.TaskID)
		stopFunc()

		// Cleanup Workspace Environment
		go func(id string) {
			time.Sleep(2 * time.Second) // wait for process to release file handles
			workspaceDir := fmt.Sprintf("/tmp/occlum_workspace_%s", id)
			log.Printf("[Cleanup] Deleting enclave environment at %s", workspaceDir)
			if err := os.RemoveAll(workspaceDir); err != nil {
				log.Printf("[Cleanup] Error deleting workspace for %s: %v", id, err)
			} else {
				log.Printf("[Cleanup] Successfully deleted environment for %s", id)
			}
		}(req.TaskID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
	})
}

type CallbackRequest struct {
	TaskID string    `json:"task_id"`
	Status TaskState `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// ProcessData is now handled directly between Data Connector and Enclave
func ProcessData(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Data processing is now handled directly via RA-TLS on port 8443. Use the Data Connector client.", http.StatusGone)
}
