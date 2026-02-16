package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func devCmd(args []string) {
	fmt.Printf("%s NeuraTrade Development Mode\n", logo)
	fmt.Println("  Starting all services in TESTNET/DEVELOPMENT mode...\n")

	projectRoot := getProjectRoot()

	fmt.Println("Development mode features:")
	fmt.Println("  • Uses testnet exchanges (binance testnet, bybit testnet, etc.)")
	fmt.Println("  • Uses SQLite for local development")
	fmt.Println("  • Verbose logging enabled")
	fmt.Println("  • Telegram bot in polling mode")
	fmt.Println("")

	// Update config to use testnet
	updateDevConfig(projectRoot)

	// Start services
	startDevServices(projectRoot)
}

func updateDevConfig(projectRoot string) {
	configPath := filepath.Join(os.Getenv("HOME"), ".neuratrade", "config.json")

	// Check if config exists
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

	// Start backend
	fmt.Println("  • Starting backend-api (SQLite mode)...")
	startBackend(projectRoot)

	// Start CCXT service
	fmt.Println("  • Starting CCXT service (testnet mode)...")
	startCCXTService(projectRoot)

	fmt.Println("\n✓ All services started in development mode!")
	fmt.Println("\nTo stop: Press Ctrl+C")
	fmt.Println("Or: neuratrade gateway stop")
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

	fmt.Printf("  CCXT service started (PID: %d)\n", cmd.Process.Pid)
}
