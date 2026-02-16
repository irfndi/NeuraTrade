package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIntegrationCLIBuild(t *testing.T) {
	if os.Getenv("SKIP_INTEGRATION") == "1" {
		t.Skip("Skipping integration tests")
	}

	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "neuratrade")

	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = "/Users/irfandi/Coding/2025/NeuraTrade/cmd/neuratrade-cli"

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build CLI: %v\nOutput: %s", err, output)
	}

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Fatal("Binary was not created")
	}

	t.Log("CLI binary built successfully")
}

func TestIntegrationCLIHelp(t *testing.T) {
	if os.Getenv("SKIP_INTEGRATION") == "1" {
		t.Skip("Skipping integration tests")
	}

	cmd := exec.Command("go", "run", ".", "--help")
	cmd.Dir = "/Users/irfandi/Coding/2025/NeuraTrade/cmd/neuratrade-cli"

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI help failed: %v\nOutput: %s", err, output)
	}

	expectedStrings := []string{
		"NeuraTrade CLI",
		"gateway start",
		"gateway stop",
		"gateway status",
		"gateway logs",
	}

	outputStr := string(output)
	for _, expected := range expectedStrings {
		if !contains(outputStr, expected) {
			t.Errorf("Help output missing: %s", expected)
		}
	}

	t.Log("CLI help output verified")
}

func TestIntegrationCLIVersion(t *testing.T) {
	if os.Getenv("SKIP_INTEGRATION") == "1" {
		t.Skip("Skipping integration tests")
	}

	cmd := exec.Command("go", "run", ".", "version")
	cmd.Dir = "/Users/irfandi/Coding/2025/NeuraTrade/cmd/neuratrade-cli"

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI version failed: %v\nOutput: %s", err, output)
	}

	expectedStrings := []string{
		"NeuraTrade CLI",
		"Go:",
	}

	outputStr := string(output)
	for _, expected := range expectedStrings {
		if !contains(outputStr, expected) {
			t.Errorf("Version output missing: %s", expected)
		}
	}

	t.Log("CLI version output verified")
}

func TestProjectRootDetection(t *testing.T) {
	root := getProjectRoot()

	if root == "" {
		t.Error("Project root should not be empty")
	}

	if root == "." {
		t.Log("Warning: Project root is current directory")
	}

	composeFile := filepath.Join(root, "docker-compose.yaml")
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		t.Logf("Warning: docker-compose.yaml not found at %s", composeFile)
	} else {
		t.Logf("Found docker-compose.yaml at %s", composeFile)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
