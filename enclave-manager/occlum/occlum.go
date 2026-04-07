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
	if err := os.RemoveAll(enclaveDir); err != nil {
		return fmt.Errorf("failed to clear workspace: %v", err)
	}

	if err := os.MkdirAll(enclaveDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// 1. Initialize
	execCmd(enclaveDir, "occlum", "init")

	// 2. Build the Go enclave-app
	// Note: We need to compile it for Linux/AMD64 since it runs inside Occlum (LibOS)
	// We'll assume the manager is running on the same platform as the enclave.
	appPath := filepath.Join(enclaveDir, "image", "bin", "enclave-app")
	cmdBuildApp := exec.Command("go", "build", "-o", appPath, "../../enclave-app/main.go")
	if out, err := cmdBuildApp.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build enclave-app: %v, output: %s", err, string(out))
	}

	// 3. Copy the Python code
	execCmd(enclaveDir, "cp", uploadedCodePath, filepath.Join(enclaveDir, "image", "bin", "process.py"))

	// 4. Occlum Build
	execCmd(enclaveDir, "occlum", "build")

	// 5. Run Enclave in Background
	// For production, this would be managed by a service runner.
	// Here we run it as a background process.
	go func() {
		log.Println("Starting Enclave Process (Occlum Run)...")
		cmdRun := exec.Command("occlum", "run", "/bin/enclave-app")
		cmdRun.Dir = enclaveDir
		cmdRun.Stdout = os.Stdout
		cmdRun.Stderr = os.Stderr
		if err := cmdRun.Run(); err != nil {
			log.Printf("Enclave process exited with error: %v", err)
		}
	}()

	log.Printf("Enclave started successfully and listening on RA-TLS port (8443)")
	return nil
}

func execCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Command failed: %s %v, output: %s", name, args, string(out))
		return err
	}
	return nil
}

// Process is now handled directly by the Data Connector via RA-TLS,
// but we keep the function for compatibility if needed.
func Process(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("not implemented: communicate directly with enclave over RA-TLS")
}
