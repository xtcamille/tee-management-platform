package occlum

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var enclaveDir string

func Start(uploadedCodePath string) error {
	enclaveDir = "/tmp/occlum_workspace"
	log.Printf("[Occlum] Starting enclave setup. workspace=%s uploadedCodePath=%s", enclaveDir, uploadedCodePath)

	log.Printf("[Occlum] Cleaning workspace: %s", enclaveDir)
	if err := os.RemoveAll(enclaveDir); err != nil {
		return fmt.Errorf("failed to clear workspace: %v", err)
	}
	log.Printf("[Occlum] Workspace cleaned")

	log.Printf("[Occlum] Creating workspace directory: %s", enclaveDir)
	if err := os.MkdirAll(enclaveDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}
	log.Printf("[Occlum] Workspace directory created")

	log.Printf("[Occlum] Stage 1/4: initializing Occlum workspace")
	if err := execCmd(enclaveDir, "occlum", "init"); err != nil {
		return fmt.Errorf("occlum init failed: %v", err)
	}
	if err := tuneOcclumConfig(enclaveDir); err != nil {
		return fmt.Errorf("failed to tune Occlum.json: %v", err)
	}
	log.Printf("[Occlum] Stage 1/4 completed: Occlum workspace initialized")

	appPath := filepath.Join(enclaveDir, "image", "bin", "enclave-app")
	log.Printf("[Occlum] Stage 2/4: copying uploaded enclave app from %s to %s", uploadedCodePath, appPath)
	if err := execCmd(enclaveDir, "cp", uploadedCodePath, appPath); err != nil {
		return fmt.Errorf("failed to copy enclave-app binary: %v", err)
	}
	log.Printf("[Occlum] Setting executable permission on %s", appPath)
	if err := os.Chmod(appPath, 0755); err != nil {
		return fmt.Errorf("failed to set executable permission on enclave-app: %v", err)
	}
	log.Printf("[Occlum] Stage 2/4 completed: enclave app binary copied and permissions set")

	log.Printf("[Occlum] Stage 3/4: building Occlum image")
	if err := execCmd(enclaveDir, "occlum", "build"); err != nil {
		return fmt.Errorf("occlum build failed: %v", err)
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

	log.Printf("[Occlum] Stage 4/4 completed: enclave process launched, RA-TLS port=8443")
	return nil
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

func tuneOcclumConfig(workspace string) error {
	configPath := filepath.Join(workspace, "Occlum.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}

	resourceLimits := ensureMap(config, "resource_limits")
	process := ensureMap(config, "process")

	ensureMinSize(resourceLimits, "user_space_size", "2048MB")
	ensureMinInt(resourceLimits, "max_num_of_threads", 128)
	ensureMinSize(process, "default_heap_size", "512MB")
	ensureMinSize(process, "default_mmap_size", "1024MB")
	ensureStringListContains(config, "entry_points", "/bin")
	ensureStringListContains(config, "entry_points", "/usr/bin")
	ensureStringListContains(config, "entry_points", "/usr/local/bin")

	updated, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(configPath, updated, 0644); err != nil {
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

func ensureMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	child := map[string]any{}
	parent[key] = child
	return child
}

func ensureMinInt(parent map[string]any, key string, minValue int64) {
	if current, ok := toInt64(parent[key]); ok && current >= minValue {
		return
	}
	parent[key] = minValue
}

func ensureMinSize(parent map[string]any, key string, minValue string) {
	minBytes, _ := parseOcclumSize(minValue)
	if currentBytes, ok := parseOcclumSize(parent[key]); ok && currentBytes >= minBytes {
		return
	}
	parent[key] = minValue
}

func ensureStringListContains(parent map[string]any, key string, value string) {
	current := make([]string, 0)
	switch values := parent[key].(type) {
	case []string:
		current = append(current, values...)
	case []any:
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

func toInt64(value any) (int64, bool) {
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

func parseOcclumSize(value any) (int64, bool) {
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

func Process(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("not implemented: communicate directly with enclave over RA-TLS")
}
