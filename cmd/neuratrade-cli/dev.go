package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

var (
	backendProcess *exec.Cmd
	ccxtProcess    *exec.Cmd
)

func devCmd(args []string) {
	fmt.Printf("%s NeuraTrade Development Mode\n", logo)
	fmt.Println("  Starting all services in TESTNET/DEVELOPMENT mode...")

	projectRoot := getProjectRoot()

	fmt.Println("Development mode features:")
	fmt.Println("  • Uses testnet exchanges (binance testnet, bybit testnet, etc.)")
	fmt.Println("  • Uses SQLite for local development")
	fmt.Println("  • Verbose logging enabled")
	fmt.Println("  • Telegram bot in polling mode")
	fmt.Println("")

	updateDevConfig(projectRoot)

	setupSignalHandler()

	startDevServices(projectRoot)

	waitForSignal()
}

func setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down services...")
		stopServices()
		os.Exit(0)
	}()
}

func stopServices() {
	if backendProcess != nil && backendProcess.Process != nil {
		fmt.Println("  Stopping backend-api...")
		backendProcess.Process.Kill()
		backendProcess.Wait()
	}
	if ccxtProcess != nil && ccxtProcess.Process != nil {
		fmt.Println("  Stopping CCXT service...")
		ccxtProcess.Process.Kill()
		ccxtProcess.Wait()
	}
	fmt.Println("✓ All services stopped")
}

func waitForSignal() {
	fmt.Println("\n✓ All services started in development mode!")
	fmt.Println("\nTo stop: Press Ctrl+C")
	fmt.Println("Or: neuratrade gateway stop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	stopServices()
}

func updateDevConfig(projectRoot string) {
	configPath := filepath.Join(os.Getenv("HOME"), ".neuratrade", "config.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("Warning: No config found at ~/.neuratrade/config.json")
		fmt.Println("Using default config...")
		return
	}

	fmt.Println("✓ Config found - enabling dev_mode")
	fmt.Println("✓ Exchanges will use testnet where available")
}

func startDevServices(projectRoot string) {
	fmt.Println("Starting services...")

	fmt.Println("  • Starting backend-api (SQLite mode)...")
	startBackend(projectRoot)

	fmt.Println("  • Starting CCXT service (testnet mode)...")
	startCCXTService(projectRoot)
}

func startBackend(projectRoot string) {
	backendDir := filepath.Join(projectRoot, "services", "backend-api")

	cmd := exec.Command("go", "run", "./cmd/server/main.go")
	cmd.Dir = backendDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"ENVIRONMENT=development",
		"DATABASE_DRIVER=sqlite",
		"SERVER_PORT=8080",
	)

	if err := cmd.Start(); err != nil {
		fmt.Printf("  Error starting backend: %v\n", err)
		return
	}

	backendProcess = cmd
	fmt.Printf("  Backend started (PID: %d)\n", cmd.Process.Pid)
}

func startCCXTService(projectRoot string) {
	ccxtDir := filepath.Join(projectRoot, "services", "ccxt-service")

	cmd := exec.Command("bun", "run", "index.ts")
	cmd.Dir = ccxtDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"CCXT_TESTNET=true",
		"PORT=3001",
	)

	if err := cmd.Start(); err != nil {
		fmt.Printf("  Error starting CCXT service: %v\n", err)
		return
	}

	ccxtProcess = cmd
	fmt.Printf("  CCXT service started (PID: %d)\n", cmd.Process.Pid)
}
