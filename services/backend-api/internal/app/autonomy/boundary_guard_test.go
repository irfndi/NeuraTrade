package autonomy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundaryGuard_NoLegacyScalpingRuntimeWiring(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	backendRoot := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	mainPath := filepath.Join(backendRoot, "cmd", "server", "main.go")
	routesPath := filepath.Join(backendRoot, "internal", "api", "routes.go")

	mainContent := mustReadFile(t, mainPath)
	routesContent := mustReadFile(t, routesPath)

	forbidden := []struct {
		path    string
		content string
		token   string
	}{
		{path: mainPath, content: mainContent, token: "RegisterIntegratedHandlers("},
		{path: routesPath, content: routesContent, token: "NewIntegratedQuestHandlers("},
		{path: routesPath, content: routesContent, token: "NewIntegratedQuestHandlersWithAutonomyStore("},
		{path: routesPath, content: routesContent, token: "RegisterIntegratedHandlers("},
	}

	for _, check := range forbidden {
		if strings.Contains(check.content, check.token) {
			t.Fatalf("legacy autonomy runtime coupling token %q found in %s", check.token, check.path)
		}
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	return string(content)
}
