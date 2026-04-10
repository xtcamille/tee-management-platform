package occlum

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func Start(taskID string, uploadedCodePath string) (int, error) {
	enclaveDir := fmt.Sprintf("/tmp/occlum_workspace_%s", taskID)
	log.Printf("[Occlum] Starting enclave setup. taskID=%s workspace=%s uploadedCodePath=%s", taskID, enclaveDir, uploadedCodePath)

	log.Printf("[Occlum] Validating uploaded binary format: %s", uploadedCodePath)
	if err := validateELF(uploadedCodePath); err != nil {
		return 0, fmt.Errorf("uploaded binary is invalid: %v. Please ensure you compiled for GOOS=linux GOARCH=amd64 -buildmode=pie", err)
	}

	log.Printf("[Occlum] Cleaning workspace: %s", enclaveDir)
	if err := os.RemoveAll(enclaveDir); err != nil {
		return 0, fmt.Errorf("failed to clear workspace: %v", err)
	}
	log.Printf("[Occlum] Workspace cleaned")

	log.Printf("[Occlum] Creating workspace directory: %s", enclaveDir)
	if err := os.MkdirAll(enclaveDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create directory: %v", err)
	}
	log.Printf("[Occlum] Workspace directory created")

	log.Printf("[Occlum] Stage 1/4: initializing Occlum workspace")
	if err := execCmd(enclaveDir, "occlum", "init"); err != nil {
		return 0, fmt.Errorf("occlum init failed: %v", err)
	}

	port, err := getFreePort()
	if err != nil {
		return 0, fmt.Errorf("failed to get free port: %v", err)
	}

	if err := tuneOcclumConfig(enclaveDir, taskID, port); err != nil {
		return 0, fmt.Errorf("failed to tune Occlum.json: %v", err)
	}
	log.Printf("[Occlum] Stage 1/4 completed: Occlum workspace initialized")

	appPath := filepath.Join(enclaveDir, "image", "bin", "enclave-app")
	// uploadedCodePath = "/zxt/tee-management-platform/enclave-app/enclave-app"
	log.Printf("[Occlum] Stage 2/4: copying uploaded enclave app from %s to %s", uploadedCodePath, appPath)
	if err := execCmd(enclaveDir, "cp", uploadedCodePath, appPath); err != nil {
		return 0, fmt.Errorf("failed to copy enclave-app binary: %v", err)
	}
	log.Printf("[Occlum] Setting executable permission on %s", appPath)
	if err := os.Chmod(appPath, 0755); err != nil {
		return 0, fmt.Errorf("failed to set executable permission on enclave-app: %v", err)
	}
	log.Printf("[Occlum] Stage 2/4 completed: enclave app binary copied and permissions set")

	log.Printf("[Occlum] Stage 3/4: building Occlum image")
	if err := execCmd(enclaveDir, "occlum", "build"); err != nil {
		return 0, fmt.Errorf("occlum build failed: %v", err)
	}
	log.Printf("[Occlum] Stage 3/4 completed: Occlum image built")

	log.Printf("[Occlum] Stage 4/4: starting enclave process in background")
	go func() {
		log.Println("[Occlum] Running enclave process: occlum run /bin/enclave-app")
		cmdRun := exec.Command("occlum", "run", "/bin/enclave-app")
		cmdRun.Dir = enclaveDir
		cmdRun.Stdout = os.Stdout
		cmdRun.Stderr = os.Stderr
		if err := cmdRun.Run(); err != nil {
			log.Printf("[Occlum] Enclave process exited with error: %v", err)
			return
		}
		log.Printf("[Occlum] Enclave process exited successfully")
	}()

	log.Printf("[Occlum] Stage 4/4 completed: enclave process launched, RA-TLS port=%d", port)
	return port, nil
}

func execCmd(dir string, name string, args ...string) error {
	log.Printf("[Occlum] Executing command in %s: %s %v", dir, name, args)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[Occlum] Command failed in %s: %s %v, err: %v, output: %s", dir, name, args, err, string(out))
		return err
	}
	log.Printf("[Occlum] Command succeeded in %s: %s %v", dir, name, args)
	return nil
}

func tuneOcclumConfig(workspace string, taskID string, port int) error {
	configPath := filepath.Join(workspace, "Occlum.json")
	raw, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}

	resourceLimits := ensureMap(config, "resource_limits")
	process := ensureMap(config, "process")
	env := ensureMap(config, "env")

	ensureMinSize(resourceLimits, "user_space_size", "2048MB")
	ensureMinInt(resourceLimits, "max_num_of_threads", 128)
	ensureMinSize(process, "default_heap_size", "512MB")
	ensureMinSize(process, "default_mmap_size", "1024MB")
	ensureStringListContains(config, "entry_points", "/bin")
	ensureStringListContains(config, "entry_points", "/usr/bin")
	ensureStringListContains(config, "entry_points", "/usr/local/bin")
	ensureStringListContains(env, "default", fmt.Sprintf("APP_PORT=%d", port))
	ensureStringListContains(env, "default", fmt.Sprintf("TASK_ID=%s", taskID))
	ensureStringListContains(env, "default", "MANAGER_URL=http://127.0.0.1:8081")

	updated, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := ioutil.WriteFile(configPath, updated, 0644); err != nil {
		return err
	}

	log.Printf(
		"[Occlum] Tuned Occlum.json: resource_limits.user_space_size=%v process.default_heap_size=%v process.default_mmap_size=%v entry_points=%v",
		resourceLimits["user_space_size"],
		process["default_heap_size"],
		process["default_mmap_size"],
		config["entry_points"],
	)
	return nil
}

func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	if existing, ok := parent[key].(map[string]interface{}); ok {
		return existing
	}
	child := map[string]interface{}{}
	parent[key] = child
	return child
}

func ensureMinInt(parent map[string]interface{}, key string, minValue int64) {
	if current, ok := toInt64(parent[key]); ok && current >= minValue {
		return
	}
	parent[key] = minValue
}

func ensureMinSize(parent map[string]interface{}, key string, minValue string) {
	minBytes, _ := parseOcclumSize(minValue)
	if currentBytes, ok := parseOcclumSize(parent[key]); ok && currentBytes >= minBytes {
		return
	}
	parent[key] = minValue
}

func ensureStringListContains(parent map[string]interface{}, key string, value string) {
	current := make([]string, 0)
	switch values := parent[key].(type) {
	case []string:
		current = append(current, values...)
	case []interface{}:
		for _, item := range values {
			if s, ok := item.(string); ok {
				current = append(current, s)
			}
		}
	}

	for _, item := range current {
		if item == value {
			parent[key] = current
			return
		}
	}
	current = append(current, value)
	parent[key] = current
}

func toInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseOcclumSize(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case string:
		s := strings.ToUpper(strings.TrimSpace(v))
		if s == "" {
			return 0, false
		}
		units := []struct {
			suffix     string
			multiplier int64
		}{
			{"TB", 1024 * 1024 * 1024 * 1024},
			{"GB", 1024 * 1024 * 1024},
			{"MB", 1024 * 1024},
			{"KB", 1024},
			{"B", 1},
		}
		for _, unit := range units {
			if strings.HasSuffix(s, unit.suffix) {
				base := strings.TrimSpace(strings.TrimSuffix(s, unit.suffix))
				n, err := strconv.ParseInt(base, 10, 64)
				if err != nil {
					return 0, false
				}
				return n * unit.multiplier, true
			}
		}
		n, err := strconv.ParseInt(s, 10, 64)
		return n, err == nil
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return toInt64(value)
	}
}

func validateELF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	header := make([]byte, 20)
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("failed to read file header: %v", err)
	}

	// 1. Check ELF magic bytes (\x7fELF)
	if header[0] != 0x7f || header[1] != 'E' || header[2] != 'L' || header[3] != 'F' {
		return fmt.Errorf("not a valid Linux ELF binary (missing magic bytes)")
	}

	// 2. Check architecture (e_machine at offset 18)
	// EM_X86_64 = 0x3e (62)
	// Note: header is Little-Endian or Big-Endian depending on EI_DATA, but EM_X86_64 is 62 in both if low byte is 0x3e.
	// Typically on x86-64 it's 0x3e 0x00.
	if header[18] != 0x3e || header[19] != 0x00 {
		return fmt.Errorf("invalid architecture: expected x86-64 (0x3e), got 0x%02x%02x", header[18], header[19])
	}

	// 3. Check ELF type (e_type at offset 16)
	// ET_DYN = 0x03 (Shared object file / PIE)
	// Occlum requires PIE executables, which are ET_DYN.
	if header[16] != 0x03 || header[17] != 0x00 {
		return fmt.Errorf("invalid ELF type: expected ET_DYN (0x03) for PIE, got 0x%02x%02x. Please compile with -buildmode=pie", header[16], header[17])
	}

	return nil
}

func Process(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("not implemented: communicate directly with enclave over RA-TLS")
}
