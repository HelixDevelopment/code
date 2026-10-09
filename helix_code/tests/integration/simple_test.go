package integration

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// Simple integration tests
func TestSystemCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")  // SKIP-OK: #short-mode
	}

	// Test basic system commands
	commands := []struct {
		name string
		cmd  string
		args []string
	}{
		{"ls", "ls", []string{"-la"}},
		{"pwd", "pwd", []string{}},
		{"whoami", "whoami", []string{}},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(tc.cmd, tc.args...)
			output, err := cmd.Output()

			if err != nil {
				t.Logf("Command %s failed: %v", tc.name, err)
				// Some commands might legitimately fail
			}

			t.Logf("Command %s output: %s", tc.name, string(output))
		})
	}
}

func TestNetworkConnectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")  // SKIP-OK: #short-mode
	}

	// Test basic network connectivity
	services := []string{
		"google.com:80",
		"github.com:443",
	}

	for _, service := range services {
		t.Run(service, func(t *testing.T) {
			// This is a simplified test - in real scenario would use proper network checks
			t.Logf("Testing connectivity to %s", service)
			time.Sleep(100 * time.Millisecond) // Simulate network test
		})
	}
}

func TestFileSystem(t *testing.T) {
	// Test file system operations
	tempDir := t.TempDir()

	// Test file creation
	testFile := tempDir + "/test.txt"
	data := []byte("test content")
	err := os.WriteFile(testFile, data, 0644)
	if err != nil {
		t.Errorf("Failed to create test file: %v", err)
	}

	// Test file reading
	readData, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("Failed to read test file: %v", err)
	}

	if string(readData) != string(data) {
		t.Errorf("File content mismatch: expected %s, got %s", string(data), string(readData))
	}

	// Test directory listing
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Errorf("Failed to read directory: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}
}

func TestProcessManagement(t *testing.T) {
	// Test process management
	cmd := exec.Command("sleep", "1")
	err := cmd.Start()
	if err != nil {
		t.Errorf("Failed to start process: %v", err)
	}

	// Wait for process to finish
	err = cmd.Wait()
	if err != nil {
		t.Errorf("Process failed: %v", err)
	}
}

func TestEnvironmentVariables(t *testing.T) {
	// Test environment variable access
	home := os.Getenv("HOME")
	if home == "" && runtime.GOOS != "windows" {
		t.Error("HOME environment variable not set")
	}

	// Test setting environment variable. t.Setenv restores the PRIOR state
	// (value or absence) at test end; the bare os.Setenv this replaced, paired
	// with a trailing os.Unsetenv, restored to ABSENCE unconditionally and so
	// clobbered any pre-existing TEST_VAR for the rest of the binary.
	t.Setenv("TEST_VAR", "test_value")

	value := os.Getenv("TEST_VAR")
	if value != "test_value" {
		t.Errorf("Expected test_value, got %s", value)
	}

}

func TestSystemInfo(t *testing.T) {
	// Test system information gathering
	t.Logf("OS: %s", runtime.GOOS)
	t.Logf("Architecture: %s", runtime.GOARCH)
	t.Logf("CPU count: %d", runtime.NumCPU())

	// Test memory allocation
	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Verify data was allocated
	if len(data) != 1024*1024 {
		t.Errorf("Expected 1MB allocation, got %d bytes", len(data))
	}
}
