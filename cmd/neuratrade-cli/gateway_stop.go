package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type StopOptions struct {
	ProjectRoot string
}

func gatewayStopCmd(args []string) {
	opts := parseStopArgs(args)

	fmt.Printf("%s Stopping NeuraTrade services...\n", logo)

	if isDockerRunning() {
		stopDocker(opts)
	} else {
		fmt.Println("Services are not running (Docker not detected)")
	}
}

func parseStopArgs(args []string) StopOptions {
	return StopOptions{
		ProjectRoot: getProjectRoot(),
	}
}

func stopDocker(opts StopOptions) {
	projectRoot := opts.ProjectRoot
	composeFile := filepath.Join(projectRoot, "docker-compose.yaml")

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		fmt.Printf("Error: docker-compose.yaml not found at %s\n", composeFile)
		os.Exit(1)
	}

	fmt.Println("Stopping Docker services...")

	cmd := exec.Command("docker", "compose", "-f", composeFile, "down")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error stopping services: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Services stopped successfully")
}

func isDockerRunning() bool {
	cmd := exec.Command("docker", "compose", "ps", "-q")
	cmd.Dir = getProjectRoot()
	output, _ := cmd.Output()
	return len(output) > 0
}
