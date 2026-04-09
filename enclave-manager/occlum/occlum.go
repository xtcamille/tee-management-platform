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

const defaultPythonHome = "/opt/python-occlum"
const defaultPythonPath = "/opt/python-occlum/lib/python3.8:/opt/python-occlum/lib/python3.8/lib-dynload:/opt/python-occlum/lib/python3.8/site-packages"
const defaultPythonInterpreter = "/usr/bin/python3.8"

var pythonBOMCandidates = []string{
	"/opt/occlum/etc/template/python-glibc.yaml",
	"/opt/occlum/etc/template/python_glibc.yaml",
	"/opt/occlum/etc/template/python.yaml",
	"/opt/occlum/etc/template/python3.yaml",
	"/opt/occlum/etc/template/python3.8.yaml",
	"/opt/occlum/etc/template/bom/python-glibc.yaml",
	"/opt/occlum/etc/template/bom/python_glibc.yaml",
	"/opt/occlum/etc/template/bom/python.yaml",
}

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
	Home       string   `json:"home"`
	LibDirs    []string `json:"lib_dirs"`
}

func preparePythonRuntime(workspace string) error {
	runtime, err := discoverPythonRuntime()
	if err != nil {
		return err
	}

	log.Printf("[Occlum] Host python runtime discovered: executable=%s lib_dirs=%v", runtime.Executable, runtime.LibDirs)
	if err := writePythonPathConfig(workspace, defaultPythonInterpreter); err != nil {
		return err
	}
	log.Printf("[Occlum] Wrote enclave Python interpreter config: %s", defaultPythonInterpreter)

	if err := preparePythonRuntimeFromTemplate(workspace, runtime); err == nil {
		log.Printf("[Occlum] Python runtime prepared from Occlum template successfully")
		return nil
	} else {
		log.Printf("[Occlum] Occlum Python template path not available or failed, falling back to manual runtime copy: %v", err)
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

func writePythonPathConfig(workspace string, executable string) error {
	if err := os.MkdirAll(filepath.Join(workspace, "image", "etc"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workspace, "image", "etc", "python3_path"), []byte(executable+"\n"), 0644); err != nil {
		return err
	}
	return nil
}

func preparePythonRuntimeFromTemplate(workspace string, runtime *pythonRuntime) error {
	if _, err := exec.LookPath("copy_bom"); err != nil {
		return fmt.Errorf("copy_bom not found on host: %v", err)
	}

	bomPath, err := findPythonBOMTemplate()
	if err != nil {
		return err
	}

	log.Printf("[Occlum] Preparing Python runtime from Occlum template: template=%s executable=%s", bomPath, runtime.Executable)
	if err := execCmd(
		workspace,
		"copy_bom",
		"--file", bomPath,
		"--root", "image",
		"--include-dir", "/opt/occlum/etc/template",
	); err != nil {
		return fmt.Errorf("copy_bom with Occlum Python template failed: %w", err)
	}

	// Keep the uploaded script workflow consistent by ensuring the host interpreter
	// path is also present in the image when the template does not include it.
	hostExecutableDir := filepath.Dir(runtime.Executable)
	if pathExists(hostExecutableDir) {
		extraBOMPath := filepath.Join(workspace, "python-runtime-exec.yaml")
		if err := os.WriteFile(extraBOMPath, []byte(renderPythonExecutableBOM(runtime.Executable)), 0644); err != nil {
			return err
		}
		if err := execCmd(
			workspace,
			"copy_bom",
			"--file", extraBOMPath,
			"--root", "image",
			"--include-dir", "/opt/occlum/etc/template",
		); err != nil {
			return fmt.Errorf("copy_bom for host python executable failed: %w", err)
		}
	}
	return nil
}

func findPythonBOMTemplate() (string, error) {
	if override := strings.TrimSpace(os.Getenv("OCCLUM_PYTHON_BOM")); override != "" {
		if pathExists(override) {
			return override, nil
		}
		return "", fmt.Errorf("OCCLUM_PYTHON_BOM is set but file does not exist: %s", override)
	}

	for _, candidate := range pythonBOMCandidates {
		if pathExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no Occlum Python BOM template found in known locations")
}

func discoverPythonRuntime() (*pythonRuntime, error) {
	if _, err := exec.LookPath("copy_bom"); err != nil {
		return nil, fmt.Errorf("copy_bom not found on host: %v", err)
	}

	pythonHome := strings.TrimSpace(os.Getenv("PYTHONHOME"))
	if pythonHome == "" {
		pythonHome = defaultPythonHome
	}

	if runtime, err := discoverPythonRuntimeFromHome(pythonHome); err == nil {
		log.Printf("[Occlum] Host python runtime discovered from PYTHONHOME: home=%s executable=%s lib_dirs=%v", runtime.Home, runtime.Executable, runtime.LibDirs)
		return runtime, nil
	} else {
		log.Printf("[Occlum] PYTHONHOME runtime discovery failed for %s, falling back to host python3 lookup: %v", pythonHome, err)
	}

	if _, err := exec.LookPath("python3"); err != nil {
		return nil, fmt.Errorf("python3 not found on host: %v", err)
	}

	script := strings.Join([]string{
		"import json, os, sys, sysconfig",
		`paths = []`,
		`for key in ("stdlib", "platstdlib", "purelib", "platlib"):`,
		`    value = sysconfig.get_path(key)`,
		`    if value and os.path.isdir(value) and value not in paths:`,
		`        paths.append(value)`,
		`print(json.dumps({"executable": os.path.realpath(sys.executable), "home": os.environ.get("PYTHONHOME", ""), "lib_dirs": paths}))`,
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
	if runtime.Home == "" {
		runtime.Home = filepath.Dir(filepath.Dir(runtime.Executable))
	}
	if len(runtime.LibDirs) == 0 {
		return nil, fmt.Errorf("host python3 inspection returned no library directories")
	}
	log.Printf("[Occlum] Host python runtime discovered: executable=%s home=%s lib_dirs=%v", runtime.Executable, runtime.Home, runtime.LibDirs)
	return &runtime, nil
}

func discoverPythonRuntimeFromHome(pythonHome string) (*pythonRuntime, error) {
	if pythonHome == "" {
		return nil, fmt.Errorf("python home is empty")
	}
	if !pathExists(pythonHome) {
		return nil, fmt.Errorf("python home does not exist: %s", pythonHome)
	}

	candidates := []string{
		filepath.Join(pythonHome, "bin", "python3"),
		filepath.Join(pythonHome, "bin", "python3.8"),
		filepath.Join(pythonHome, "bin", "python"),
	}

	executable := ""
	for _, candidate := range candidates {
		if pathExists(candidate) {
			executable = candidate
			break
		}
	}
	if executable == "" {
		return nil, fmt.Errorf("no python executable found under %s/bin", pythonHome)
	}

	libDirs := make([]string, 0)
	for _, candidate := range []string{
		filepath.Join(pythonHome, "lib", "python3.8"),
		filepath.Join(pythonHome, "lib", "python3.8", "lib-dynload"),
		filepath.Join(pythonHome, "lib", "python3.8", "site-packages"),
	} {
		if pathExists(candidate) {
			libDirs = append(libDirs, candidate)
		}
	}
	if len(libDirs) == 0 {
		return nil, fmt.Errorf("no python library directories found under %s/lib", pythonHome)
	}

	return &pythonRuntime{
		Executable: executable,
		Home:       pythonHome,
		LibDirs:    libDirs,
	}, nil
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
	if runtime.Home != "" && pathExists(runtime.Home) {
		builder.WriteString("      - dirs:\n")
		builder.WriteString(fmt.Sprintf("          - %s\n", runtime.Home))
		return builder.String()
	}
	builder.WriteString("      - dirs:\n")
	for _, dir := range libDirs {
		builder.WriteString(fmt.Sprintf("          - %s\n", dir))
	}
	return builder.String()
}

func renderPythonExecutableBOM(executable string) string {
	var builder strings.Builder
	builder.WriteString("targets:\n")
	builder.WriteString("  - target: /\n")
	builder.WriteString("    copy:\n")
	builder.WriteString("      - files:\n")
	builder.WriteString(fmt.Sprintf("          - %s\n", executable))
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
	envConfig := ensureMap(config, "env")

	// Go reserves a large virtual address space during runtime bootstrap.
	// Occlum defaults are often too small, which triggers
	// "failed to reserve page summary memory" before main() executes.
	ensureMinSize(resourceLimits, "user_space_size", "2048MB")
	ensureMinInt(resourceLimits, "max_num_of_threads", 128)
	ensureMinSize(process, "default_heap_size", "512MB")
	ensureMinSize(process, "default_mmap_size", "1024MB")
	ensureStringListContains(config, "entry_points", "/bin")
	ensureStringListContains(config, "entry_points", "/usr/bin")
	ensureStringListContains(config, "entry_points", "/usr/local/bin")
	ensureStringListContains(config, "entry_points", "/opt/python-occlum/bin")
	ensureStringListContains(envConfig, "default", "OCCLUM=yes")
	ensureStringListContains(envConfig, "default", "PYTHONHOME="+defaultPythonHome)
	ensureStringListContains(envConfig, "default", "PYTHONPATH="+defaultPythonPath)

	updated, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(configPath, updated, 0644); err != nil {
		return err
	}

	log.Printf(
		"[Occlum] Tuned Occlum.json: resource_limits.user_space_size=%v process.default_heap_size=%v process.default_mmap_size=%v entry_points=%v env.default=%v",
		resourceLimits["user_space_size"],
		process["default_heap_size"],
		process["default_mmap_size"],
		config["entry_points"],
		envConfig["default"],
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
