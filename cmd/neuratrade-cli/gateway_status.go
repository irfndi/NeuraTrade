package main

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type StatusOptions struct {
	ProjectRoot string
}

func gatewayStatusCmd(args []string) {
	opts := parseStatusArgs(args)

	fmt.Printf("%s NeuraTrade Service Status\n\n", logo)

	checkDockerStatus()
	fmt.Println()
	checkServiceHealth(opts)
}

func parseStatusArgs(args []string) StatusOptions {
	return StatusOptions{
		ProjectRoot: getProjectRoot(),
	}
}

func checkDockerStatus() {
	fmt.Println("Docker Status:")

	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("  ✗ Docker is not running")
		return
	}

	version := strings.TrimSpace(string(output))
	fmt.Printf("  ✓ Docker running (version %s)\n", version)

	projectRoot := getProjectRoot()
	composeFile := filepath.Join(projectRoot, "docker-compose.yaml")

	cmd = exec.Command("docker", "compose", "-f", composeFile, "ps", "--format", "table")
	cmd.Dir = projectRoot
	output, err = cmd.Output()
	if err != nil {
		fmt.Println("  No containers running")
		return
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) > 1 {
		fmt.Println("\n  Running Containers:")
		for i, line := range lines {
			if i == 0 {
				fmt.Printf("    %s\n", line)
			} else if strings.TrimSpace(line) != "" {
				fmt.Printf("    %s\n", line)
			}
		}
	}
}

func checkServiceHealth(opts StatusOptions) {
	fmt.Println("Service Health:")

	services := []struct {
		name string
		url  string
	}{
		{"Backend API", "http://localhost:8080/health"},
		{"CCXT Service", "http://localhost:3001/health"},
		{"Telegram Service", "http://localhost:3002/health"},
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	allHealthy := true
	for _, svc := range services {
		resp, err := client.Get(svc.url)
		if err != nil {
			fmt.Printf("  ✗ %s: unreachable (%v)\n", svc.name, err)
			allHealthy = false
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			fmt.Printf("  ✓ %s: healthy\n", svc.name)
		} else {
			fmt.Printf("  ⚠ %s: unhealthy (status %d)\n", svc.name, resp.StatusCode)
			allHealthy = false
		}
	}

	if allHealthy {
		fmt.Println("\n✓ All services are healthy")
	} else {
		fmt.Println("\n⚠ Some services are not healthy")
		fmt.Println("  Run 'neuratrade gateway logs' for more details")
	}
}
