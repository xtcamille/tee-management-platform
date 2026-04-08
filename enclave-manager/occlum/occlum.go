package occlum

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	log.Printf("[Occlum] Stage 1/5 completed: Occlum workspace initialized")

	// 2. Build the Go enclave-app
	// Note: We need to compile it for Linux/AMD64 since it runs inside Occlum (LibOS)
	// We'll assume the manager is running on the same platform as the enclave.
	appPath := filepath.Join(enclaveDir, "image", "bin", "enclave-app")
	sourcePath := "../enclave-app/main.go"
	log.Printf("[Occlum] Stage 2/5: building enclave app from %s to %s", sourcePath, appPath)
	cmdBuildApp := exec.Command("go", "build", "-x", "-o", appPath, sourcePath)
	if out, err := cmdBuildApp.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build enclave-app: %v, output: %s", err, string(out))
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

// Process is now handled directly by the Data Connector via RA-TLS,
// but we keep the function for compatibility if needed.
func Process(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("not implemented: communicate directly with enclave over RA-TLS")
}
