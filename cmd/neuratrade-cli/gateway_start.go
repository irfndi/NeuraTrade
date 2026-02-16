package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type StartOptions struct {
	Native      bool
	NoBackend   bool
	NoCCXT      bool
	NoTelegram  bool
	Detach      bool
	ProjectRoot string
}

func gatewayStartCmd(args []string) {
	opts := parseStartArgs(args)

	fmt.Printf("%s Starting NeuraTrade services...\n", logo)

	if opts.Native {
		startNative(opts)
	} else {
		startDocker(opts)
	}
}

func parseStartArgs(args []string) StartOptions {
	opts := StartOptions{
		ProjectRoot: getProjectRoot(),
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--native":
			opts.Native = true
		case "--no-backend":
			opts.NoBackend = true
		case "--no-ccxt":
			opts.NoCCXT = true
		case "--no-telegram":
			opts.NoTelegram = true
		case "--detach", "-d":
			opts.Detach = true
		}
	}

	return opts
}

func startDocker(opts StartOptions) {
	projectRoot := opts.ProjectRoot

	if !checkDocker() {
		fmt.Println("Error: Docker is not installed or not running")
		fmt.Println("Please install Docker: https://docs.docker.com/get-docker/")
		os.Exit(1)
	}

	if !checkDockerCompose() {
		fmt.Println("Error: Docker Compose is not installed")
		os.Exit(1)
	}

	composeFile := filepath.Join(projectRoot, "docker-compose.yaml")
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		fmt.Printf("Error: docker-compose.yaml not found at %s\n", composeFile)
		os.Exit(1)
	}

	envFile := filepath.Join(projectRoot, ".env")
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		exampleFile := filepath.Join(projectRoot, ".env.example")
		if _, err := os.Stat(exampleFile); err == nil {
			fmt.Println("Creating .env from .env.example...")
			copyFile(exampleFile, envFile)
			fmt.Println("Please edit .env file with your configuration before running again")
			os.Exit(1)
		}
	}

	services := []string{}
	if !opts.NoBackend {
		services = append(services, "backend-api")
	}
	if !opts.NoCCXT {
		services = append(services, "ccxt-service")
	}
	if !opts.NoTelegram {
		services = append(services, "telegram-service")
	}

	if len(services) == 0 {
		fmt.Println("Error: No services selected to start")
		os.Exit(1)
	}

	fmt.Printf("Starting services: %s\n", strings.Join(services, ", "))

	var cmd *exec.Cmd
	if opts.Detach {
		cmd = exec.Command("docker", "compose", "-f", composeFile, "up", "-d", "--build")
		fmt.Println("Running in detached mode...")
	} else {
		cmd = exec.Command("docker", "compose", "-f", composeFile, "up", "--build")
		fmt.Println("Press Ctrl+C to stop all services")
	}

	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if opts.Detach {
		err := cmd.Run()
		if err != nil {
			fmt.Printf("Error starting services: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Services started successfully!")
		fmt.Println("Use 'neuratrade gateway logs' to view logs")
		fmt.Println("Use 'neuratrade gateway stop' to stop services")
	} else {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigChan
			fmt.Println("\n\nShutting down services...")
			stopCmd := exec.Command("docker", "compose", "-f", composeFile, "down")
			stopCmd.Dir = projectRoot
			stopCmd.Run()
			os.Exit(0)
		}()

		err := cmd.Run()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
				fmt.Printf("\nServices exited with error: %v\n", err)
				os.Exit(1)
			}
		}
	}
}

func startNative(opts StartOptions) {
	if !opts.NoBackend {
		checkBackendRequirements()
	}
	if !opts.NoCCXT || !opts.NoTelegram {
		checkBunRequirements()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	serviceErrors := make(chan error, 3)

	if !opts.NoBackend {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runBackendService(ctx); err != nil {
				serviceErrors <- fmt.Errorf("backend-api: %w", err)
			}
		}()
		time.Sleep(2 * time.Second)
	}

	if !opts.NoCCXT {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runCCXTService(ctx); err != nil {
				serviceErrors <- fmt.Errorf("ccxt-service: %w", err)
			}
		}()
		time.Sleep(1 * time.Second)
	}

	if !opts.NoTelegram {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runTelegramService(ctx); err != nil {
				serviceErrors <- fmt.Errorf("telegram-service: %w", err)
			}
		}()
	}

	fmt.Println("✓ All services started!")
	fmt.Println("Press Ctrl+C to stop all services")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		for err := range serviceErrors {
			fmt.Printf("Service error: %v\n", err)
		}
	}()

	<-sigChan
	fmt.Println("\n\nShutting down services...")
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("✓ All services stopped")
	case <-time.After(10 * time.Second):
		fmt.Println("⚠ Some services did not stop gracefully")
	}
}

func runBackendService(ctx context.Context) error {
	projectRoot := getProjectRoot()
	backendDir := filepath.Join(projectRoot, "services", "backend-api")

	fmt.Println("Starting backend-api service...")

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/server/main.go")
	cmd.Dir = backendDir
	cmd.Env = os.Environ()

	return runServiceWithOutput(cmd, "[backend-api]")
}

func runCCXTService(ctx context.Context) error {
	projectRoot := getProjectRoot()
	ccxtDir := filepath.Join(projectRoot, "services", "ccxt-service")

	fmt.Println("Starting ccxt-service...")

	cmd := exec.CommandContext(ctx, "bun", "run", "index.ts")
	cmd.Dir = ccxtDir
	cmd.Env = os.Environ()

	return runServiceWithOutput(cmd, "[ccxt-service]")
}

func runTelegramService(ctx context.Context) error {
	projectRoot := getProjectRoot()
	telegramDir := filepath.Join(projectRoot, "services", "telegram-service")

	fmt.Println("Starting telegram-service...")

	cmd := exec.CommandContext(ctx, "bun", "run", "index.ts")
	cmd.Dir = telegramDir
	cmd.Env = append(os.Environ(),
		"TELEGRAM_USE_POLLING=true",
		"TELEGRAM_API_BASE_URL=http://localhost:8080",
	)

	return runServiceWithOutput(cmd, "[telegram-service]")
}

func runServiceWithOutput(cmd *exec.Cmd, prefix string) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Printf("%s %s\n", prefix, scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Printf("%s %s\n", prefix, scanner.Text())
		}
	}()

	return cmd.Wait()
}

func checkBackendRequirements() {
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Println("Error: Go is not installed")
		fmt.Println("Please install Go: https://golang.org/dl/")
		os.Exit(1)
	}
}

func checkBunRequirements() {
	if _, err := exec.LookPath("bun"); err != nil {
		fmt.Println("Error: Bun is not installed")
		fmt.Println("Please install Bun: https://bun.sh/")
		os.Exit(1)
	}
}

func checkDocker() bool {
	cmd := exec.Command("docker", "version")
	err := cmd.Run()
	return err == nil
}

func checkDockerCompose() bool {
	cmd := exec.Command("docker", "compose", "version")
	err := cmd.Run()
	return err == nil
}

func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0644)
}
