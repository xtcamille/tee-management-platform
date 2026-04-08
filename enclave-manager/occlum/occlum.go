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

	// 1. Initialize
	log.Printf("[Occlum] Stage 1/5: initializing Occlum workspace")
	if err := execCmd(enclaveDir, "occlum", "init"); err != nil {
		return fmt.Errorf("occlum init failed: %v", err)
	}
	if err := tuneOcclumConfig(enclaveDir); err != nil {
		return fmt.Errorf("failed to tune Occlum.json: %v", err)
	}
	if err := preparePythonRuntime(enclaveDir); err != nil {
		return fmt.Errorf("failed to prepare Python runtime: %v", err)
	}
	log.Printf("[Occlum] Stage 1/5 completed: Occlum workspace initialized")

	// 2. Build the Go enclave-app
	// Note: We need to compile it for Linux/AMD64 since it runs inside Occlum (LibOS)
	// We'll assume the manager is running on the same platform as the enclave.
	appPath := filepath.Join(enclaveDir, "image", "bin", "enclave-app")
	sourcePath := "../enclave-app/main.go"
	log.Printf("[Occlum] Stage 2/5: building enclave app from %s to %s", sourcePath, appPath)
	if err := buildEnclaveApp(sourcePath, appPath); err != nil {
		return fmt.Errorf("failed to build enclave-app: %v", err)
	}
	log.Printf("[Occlum] Stage 2/5 completed: enclave app built")

	// 3. Copy the Python code
	processPath := filepath.Join(enclaveDir, "image", "bin", "process.py")
	log.Printf("[Occlum] Stage 3/5: copying uploaded code to %s", processPath)
	if err := execCmd(enclaveDir, "cp", uploadedCodePath, processPath); err != nil {
		return fmt.Errorf("copy uploaded code failed: %v", err)
	}
	log.Printf("[Occlum] Stage 3/5 completed: uploaded code copied")

	// 4. Occlum Build
	log.Printf("[Occlum] Stage 4/5: building Occlum image")
	if err := execCmd(enclaveDir, "occlum", "build"); err != nil {
		return fmt.Errorf("occlum build failed: %v", err)
	}
	log.Printf("[Occlum] Stage 4/5 completed: Occlum image built")

	// 5. Run Enclave in Background
	// For production, this would be managed by a service runner.
	// Here we run it as a background process.
	log.Printf("[Occlum] Stage 5/5: starting enclave process in background")
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

	log.Printf("[Occlum] Stage 5/5 completed: enclave process launched, RA-TLS port=8443")
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

func buildEnclaveApp(sourcePath string, appPath string) error {
	buildTool := "occlum-go"
	buildArgs := []string{"build", "-o", appPath, sourcePath}
	if _, err := exec.LookPath(buildTool); err != nil {
		log.Printf("[Occlum] occlum-go not found, falling back to go build -buildmode=pie")
		buildTool = "go"
		buildArgs = []string{"build", "-x", "-buildmode=pie", "-o", appPath, sourcePath}
	}

	log.Printf("[Occlum] Building enclave app with %s %v", buildTool, buildArgs)
	cmdBuildApp := exec.Command(buildTool, buildArgs...)
	cmdBuildApp.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	if out, err := cmdBuildApp.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v failed: %v, output: %s", buildTool, buildArgs, err, string(out))
	}
	return nil
}

type pythonRuntime struct {
	Executable string   `json:"executable"`
	LibDirs    []string `json:"lib_dirs"`
}

func preparePythonRuntime(workspace string) error {
	runtime, err := discoverPythonRuntime()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(workspace, "image", "etc"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workspace, "image", "etc", "python3_path"), []byte(runtime.Executable+"\n"), 0644); err != nil {
		return err
	}

	bomPath := filepath.Join(workspace, "python-runtime.yaml")
	if err := os.WriteFile(bomPath, []byte(renderPythonRuntimeBOM(runtime)), 0644); err != nil {
		return err
	}

	log.Printf("[Occlum] Copying Python runtime into Occlum image: executable=%s lib_dirs=%v", runtime.Executable, runtime.LibDirs)
	if err := execCmd(
		workspace,
		"copy_bom",
		"--file", bomPath,
		"--root", "image",
		"--include-dir", "/opt/occlum/etc/template",
	); err != nil {
		return err
	}
	return nil
}

func discoverPythonRuntime() (*pythonRuntime, error) {
	if _, err := exec.LookPath("python3"); err != nil {
		return nil, fmt.Errorf("python3 not found on host: %v", err)
	}
	if _, err := exec.LookPath("copy_bom"); err != nil {
		return nil, fmt.Errorf("copy_bom not found on host: %v", err)
	}

	script := strings.Join([]string{
		"import json, os, sys, sysconfig",
		`paths = []`,
		`for key in ("stdlib", "platstdlib", "purelib", "platlib"):`,
		`    value = sysconfig.get_path(key)`,
		`    if value and os.path.isdir(value) and value not in paths:`,
		`        paths.append(value)`,
		`print(json.dumps({"executable": os.path.realpath(sys.executable), "lib_dirs": paths}))`,
	}, "\n")

	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect host python3 runtime: %v, output: %s", err, string(out))
	}

	var runtime pythonRuntime
	if err := json.Unmarshal(out, &runtime); err != nil {
		return nil, fmt.Errorf("failed to parse host python3 runtime info: %v, output: %s", err, string(out))
	}
	if runtime.Executable == "" {
		return nil, fmt.Errorf("host python3 inspection returned empty executable path")
	}
	if len(runtime.LibDirs) == 0 {
		return nil, fmt.Errorf("host python3 inspection returned no library directories")
	}
	return &runtime, nil
}

func renderPythonRuntimeBOM(runtime *pythonRuntime) string {
	libDirs := append([]string{}, runtime.LibDirs...)
	if !containsString(libDirs, "/etc/python3") && pathExists("/etc/python3") {
		libDirs = append(libDirs, "/etc/python3")
	}

	var builder strings.Builder
	builder.WriteString("targets:\n")
	builder.WriteString("  - target: /\n")
	builder.WriteString("    copy:\n")
	builder.WriteString("      - files:\n")
	builder.WriteString(fmt.Sprintf("          - %s\n", runtime.Executable))
	builder.WriteString("      - dirs:\n")
	for _, dir := range libDirs {
		builder.WriteString(fmt.Sprintf("          - %s\n", dir))
	}
	return builder.String()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

	// Go reserves a large virtual address space during runtime bootstrap.
	// Occlum defaults are often too small, which triggers
	// "failed to reserve page summary memory" before main() executes.
	ensureMinSize(resourceLimits, "user_space_size", "2048MB")
	ensureMinInt(resourceLimits, "max_num_of_threads", 128)
	ensureMinSize(process, "default_heap_size", "512MB")
	ensureMinSize(process, "default_mmap_size", "1024MB")

	updated, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(configPath, updated, 0644); err != nil {
		return err
	}

	log.Printf(
		"[Occlum] Tuned Occlum.json: resource_limits.user_space_size=%v process.default_heap_size=%v process.default_mmap_size=%v",
		resourceLimits["user_space_size"],
		process["default_heap_size"],
		process["default_mmap_size"],
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
		units := map[string]int64{
			"KB": 1024,
			"MB": 1024 * 1024,
			"GB": 1024 * 1024 * 1024,
			"TB": 1024 * 1024 * 1024 * 1024,
			"B":  1,
		}
		for suffix, multiplier := range units {
			if strings.HasSuffix(s, suffix) {
				base := strings.TrimSpace(strings.TrimSuffix(s, suffix))
				n, err := strconv.ParseInt(base, 10, 64)
				if err != nil {
					return 0, false
				}
				return n * multiplier, true
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

// Process is now handled directly by the Data Connector via RA-TLS,
// but we keep the function for compatibility if needed.
func Process(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("not implemented: communicate directly with enclave over RA-TLS")
}
