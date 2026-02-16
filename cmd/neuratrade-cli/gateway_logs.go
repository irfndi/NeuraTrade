package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type LogsOptions struct {
	Service     string
	Follow      bool
	Tail        int
	ProjectRoot string
}

func gatewayLogsCmd(args []string) {
	opts := parseLogsArgs(args)

	fmt.Printf("%s Showing service logs...\n\n", logo)

	showDockerLogs(opts)
}

func parseLogsArgs(args []string) LogsOptions {
	opts := LogsOptions{
		Tail:        100,
		ProjectRoot: getProjectRoot(),
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--service", "-s":
			if i+1 < len(args) {
				opts.Service = args[i+1]
				i++
			}
		case "--follow", "-f":
			opts.Follow = true
		case "--tail", "-n":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &opts.Tail)
				i++
			}
		}
	}

	return opts
}

func showDockerLogs(opts LogsOptions) {
	projectRoot := opts.ProjectRoot
	composeFile := filepath.Join(projectRoot, "docker-compose.yaml")

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		fmt.Printf("Error: docker-compose.yaml not found at %s\n", composeFile)
		os.Exit(1)
	}

	var cmd *exec.Cmd

	if opts.Service != "" {
		serviceName := opts.Service

		switch serviceName {
		case "backend", "api":
			serviceName = "backend-api"
		case "ccxt":
			serviceName = "ccxt-service"
		case "telegram":
			serviceName = "telegram-service"
		}

		fmt.Printf("Showing logs for %s...\n", serviceName)

		if opts.Follow {
			cmd = exec.Command("docker", "compose", "-f", composeFile, "logs", "-f", "--tail", fmt.Sprintf("%d", opts.Tail), serviceName)
		} else {
			cmd = exec.Command("docker", "compose", "-f", composeFile, "logs", "--tail", fmt.Sprintf("%d", opts.Tail), serviceName)
		}
	} else {
		fmt.Println("Showing logs for all services...")

		if opts.Follow {
			cmd = exec.Command("docker", "compose", "-f", composeFile, "logs", "-f", "--tail", fmt.Sprintf("%d", opts.Tail))
		} else {
			cmd = exec.Command("docker", "compose", "-f", composeFile, "logs", "--tail", fmt.Sprintf("%d", opts.Tail))
		}
	}

	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			fmt.Printf("Error showing logs: %v\n", err)
			os.Exit(1)
		}
	}
}
