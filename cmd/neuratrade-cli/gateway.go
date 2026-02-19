package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/urfave/cli/v2"
)

// GatewayCommand represents the gateway command structure
type GatewayCommand struct {
	BackendPort    string
	CCXTPort       string
	TelegramPort   string
	BindHost       string
	NeuratradeHome string
}

// gatewayStart starts all NeuraTrade services
func gatewayStart(cCtx *cli.Context) error {
	fmt.Println("🚀 Starting NeuraTrade Gateway...")
	fmt.Println()

	home := defaultNeuraTradeHome()
	cfg := getConfigValue(home)

	backendPort := getEnvOrDefault("BACKEND_HOST_PORT", "")
	if backendPort == "" && cfg != nil && cfg.Server.Port > 0 {
		backendPort = strconv.Itoa(cfg.Server.Port)
	}
	if backendPort == "" {
		backendPort = "8080"
	}

	ccxtPort := getEnvOrDefault("CCXT_PORT", "3001")
	telegramPort := getEnvOrDefault("TELEGRAM_PORT", "3002")
	bindHost := getEnvOrDefault("BIND_HOST", "127.0.0.1")
	adminAPIKey := getEnvOrDefault("ADMIN_API_KEY", configAdminAPIKey(cfg))

	sqlitePath := getEnvOrDefault("SQLITE_PATH", "")
	if sqlitePath == "" && cfg != nil && cfg.Database.SQLitePath != "" {
		sqlitePath = cfg.Database.SQLitePath
	}
	if sqlitePath == "" {
		sqlitePath = filepath.Join(home, "data", "neuratrade.db")
	}

	telegramToken := getEnvOrDefault("TELEGRAM_BOT_TOKEN", "")
	if telegramToken == "" && cfg != nil {
		telegramToken = cfg.Telegram.BotToken
	}

	aiAPIKey := getEnvOrDefault("AI_API_KEY", "")
	aiBaseURL := getEnvOrDefault("AI_BASE_URL", "")
	aiProvider := getEnvOrDefault("AI_PROVIDER", "")
	aiModel := getEnvOrDefault("AI_MODEL", "")
	if cfg != nil {
		if aiAPIKey == "" {
			aiAPIKey = cfg.AI.APIKey
		}
		if aiBaseURL == "" {
			aiBaseURL = cfg.AI.BaseURL
		}
		if aiProvider == "" {
			aiProvider = cfg.AI.Provider
		}
		if aiModel == "" {
			aiModel = cfg.AI.Model
		}
	}

	fmt.Printf("📁 NeuraTrade Home: %s\n", home)
	fmt.Printf("⚙️  Config File: %s\n", filepath.Join(home, "config.json"))
	fmt.Printf("🌐 Backend Port: %s (public)\n", backendPort)
	fmt.Printf("🔌 CCXT Port: %s (internal, bound to %s)\n", ccxtPort, bindHost)
	fmt.Printf("📞 Telegram Port: %s (internal, bound to %s)\n", telegramPort, bindHost)
	fmt.Println()

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Join(home, "logs"), 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "pids"), 0755); err != nil {
		return fmt.Errorf("failed to create pids directory: %w", err)
	}

	// Get executable directory
	execDir, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execDir = filepath.Dir(execDir)

	// Start CCXT Service
	fmt.Println("📊 Starting CCXT Service...")
	ccxtCmd := startService(
		filepath.Join(execDir, "ccxt-service"),
		"CCXT Service",
		filepath.Join(home, "logs", "ccxt.log"),
		map[string]string{
			"PORT":          ccxtPort,
			"BIND_HOST":     bindHost,
			"NODE_ENV":      "production",
			"ADMIN_API_KEY": adminAPIKey,
		},
		filepath.Join(home, "pids", "ccxt.pid"),
	)
	if ccxtCmd == nil {
		return fmt.Errorf("failed to start CCXT service")
	}
	fmt.Println("✅ CCXT Service started")

	// Start Telegram Service
	fmt.Println("📞 Starting Telegram Service...")
	telegramCmd := startService(
		filepath.Join(execDir, "telegram-service"),
		"Telegram Service",
		filepath.Join(home, "logs", "telegram.log"),
		map[string]string{
			"PORT":                  telegramPort,
			"BIND_HOST":             bindHost,
			"TELEGRAM_BOT_TOKEN":    telegramToken,
			"TELEGRAM_USE_POLLING":  getEnvOrDefault("TELEGRAM_USE_POLLING", "true"),
			"TELEGRAM_API_BASE_URL": fmt.Sprintf("http://%s:%s", bindHost, backendPort),
			"NODE_ENV":              "production",
			"ADMIN_API_KEY":         adminAPIKey,
		},
		filepath.Join(home, "pids", "telegram.pid"),
	)
	if telegramCmd == nil {
		ccxtCmd.Process.Signal(syscall.SIGTERM)
		return fmt.Errorf("failed to start Telegram service")
	}
	fmt.Println("✅ Telegram Service started")

	// Start Backend API
	fmt.Println("🔧 Starting Backend API...")
	backendCmd := startService(
		filepath.Join(execDir, "neuratrade-server"),
		"Backend API",
		filepath.Join(home, "logs", "backend.log"),
		map[string]string{
			"PORT":                  backendPort,
			"HOST":                  "0.0.0.0", // Backend binds to all interfaces
			"DATABASE_DRIVER":       getEnvOrDefault("DATABASE_DRIVER", "sqlite"),
			"SQLITE_PATH":           sqlitePath,
			"SQLITE_DB_PATH":        sqlitePath,
			"REDIS_HOST":            getEnvOrDefault("REDIS_HOST", "localhost"),
			"REDIS_PORT":            getEnvOrDefault("REDIS_PORT", "6379"),
			"CCXT_SERVICE_URL":      fmt.Sprintf("http://%s:%s", bindHost, ccxtPort),
			"CCXT_GRPC_ADDRESS":     fmt.Sprintf("%s:%s", bindHost, getEnvOrDefault("CCXT_GRPC_PORT", "50051")),
			"TELEGRAM_SERVICE_URL":  fmt.Sprintf("http://%s:%s", bindHost, telegramPort),
			"TELEGRAM_GRPC_ADDRESS": fmt.Sprintf("%s:%s", bindHost, getEnvOrDefault("TELEGRAM_GRPC_PORT", "50052")),
			"JWT_SECRET":            getEnvOrDefault("JWT_SECRET", "dev-jwt-secret"),
			"ADMIN_API_KEY":         adminAPIKey,
			"SENTRY_ENVIRONMENT":    getEnvOrDefault("SENTRY_ENVIRONMENT", "production"),
			"SENTRY_DSN":            getEnvOrDefault("SENTRY_DSN", ""),
			"AI_API_KEY":            aiAPIKey,
			"AI_BASE_URL":           aiBaseURL,
			"AI_PROVIDER":           aiProvider,
			"AI_MODEL":              aiModel,
		},
		filepath.Join(home, "pids", "backend.pid"),
	)
	if backendCmd == nil {
		ccxtCmd.Process.Signal(syscall.SIGTERM)
		telegramCmd.Process.Signal(syscall.SIGTERM)
		return fmt.Errorf("failed to start backend API")
	}
	fmt.Println("✅ Backend API started")
	fmt.Println()
	fmt.Println("🎉 All services started successfully!")
	fmt.Println()
	fmt.Printf("📡 Backend API: http://localhost:%s\n", backendPort)
	fmt.Printf("🏥 Health Check: http://localhost:%s/health\n", backendPort)
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop all services")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println()
	fmt.Println("🛑 Shutting down services...")

	// Graceful shutdown
	backendCmd.Process.Signal(syscall.SIGTERM)
	telegramCmd.Process.Signal(syscall.SIGTERM)
	ccxtCmd.Process.Signal(syscall.SIGTERM)

	// Wait for processes to exit
	backendCmd.Wait()
	telegramCmd.Wait()
	ccxtCmd.Wait()

	fmt.Println("✅ All services stopped")
	return nil
}

// startService starts a service process and writes its PID to a file
func startService(binary, name, logFile string, env map[string]string, pidFile string) *exec.Cmd {
	cmd := exec.Command(binary)

	// Set environment
	envVars := os.Environ()
	for key, value := range env {
		if value != "" {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
	}
	cmd.Env = envVars

	// Redirect output to log file
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("⚠️  Warning: Could not open log file for %s: %v\n", name, err)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = logF
		cmd.Stderr = logF
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("❌ Failed to start %s: %v\n", name, err)
		return nil
	}

	// Write PID file for later cleanup
	if pidFile != "" {
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644); err != nil {
			fmt.Printf("⚠️  Warning: Could not write PID file for %s: %v\n", name, err)
		}
	}

	return cmd
}

// gatewayStop stops all NeuraTrade services by reading PID files and sending SIGTERM
func gatewayStop(cCtx *cli.Context) error {
	fmt.Println("🛑 Stopping NeuraTrade services...")
	fmt.Println()

	home := defaultNeuraTradeHome()
	pidsDir := filepath.Join(home, "pids")

	services := []struct {
		name    string
		pidFile string
	}{
		{"Backend API", "backend.pid"},
		{"CCXT Service", "ccxt.pid"},
		{"Telegram Service", "telegram.pid"},
	}

	stoppedCount := 0
	for _, svc := range services {
		pidFile := filepath.Join(pidsDir, svc.pidFile)
		if err := stopServiceByPIDFile(svc.name, pidFile); err != nil {
			fmt.Printf("⚠️  %s: %v\n", svc.name, err)
		} else {
			stoppedCount++
		}
	}

	if stoppedCount == 0 {
		fmt.Println()
		fmt.Println("No running services found.")
		fmt.Println("Services may have been stopped already, or were not started via 'gateway start'.")
		fmt.Println()
		fmt.Println("To force stop, you can manually kill the processes:")
		fmt.Println("  pkill -f neuratrade-server")
		fmt.Println("  pkill -f ccxt-service")
		fmt.Println("  pkill -f telegram-service")
		return fmt.Errorf("no services stopped")
	}

	fmt.Println()
	fmt.Printf("✅ Stopped %d service(s)\n", stoppedCount)
	return nil
}

// stopServiceByPIDFile reads a PID file and sends SIGTERM to the process
func stopServiceByPIDFile(name, pidFile string) error {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("not running (PID file not found)")
		}
		return fmt.Errorf("could not read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(bytes.TrimSpace(data)))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("process not found (removing stale PID file)")
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	fmt.Printf("✅ %s: Stopped (PID: %d)\n", name, pid)
	os.Remove(pidFile)
	return nil
}

// gatewayStatus shows the status of NeuraTrade services
func gatewayStatus(cCtx *cli.Context) error {
	fmt.Println("📊 NeuraTrade Service Status")
	fmt.Println("============================")
	fmt.Println()

	// Check if processes are running
	checkProcess("neuratrade-server", "Backend API")
	checkProcess("ccxt-service", "CCXT Service")
	checkProcess("telegram-service", "Telegram Service")

	fmt.Println()

	// Check health endpoint
	backendPort := getEnvOrDefault("BACKEND_HOST_PORT", "")
	if backendPort == "" {
		if cfg := getConfigValue(defaultNeuraTradeHome()); cfg != nil && cfg.Server.Port > 0 {
			backendPort = strconv.Itoa(cfg.Server.Port)
		} else {
			backendPort = "8080"
		}
	}
	fmt.Printf("🏥 Health Check: http://localhost:%s/health\n", backendPort)
	fmt.Println()

	// Try to get health
	baseURL := fmt.Sprintf("http://localhost:%s", backendPort)
	apiKey := getAPIKey()
	client := NewAPIClient(baseURL, apiKey)

	respBody, err := client.makeRequest("GET", "/health", nil)
	if err != nil {
		fmt.Printf("❌ Health check failed: %v\n", err)
		fmt.Println()
		fmt.Println("Make sure the backend is running:")
		fmt.Println("  neuratrade gateway start")
		return err
	}

	var healthResp map[string]interface{}
	if err := json.Unmarshal(respBody, &healthResp); err != nil {
		fmt.Printf("❌ Could not parse health response: %v\n", err)
		return err
	}

	status := "Unknown"
	if v, ok := healthResp["status"].(string); ok {
		status = v
	}

	fmt.Printf("✅ Backend Status: %s\n", status)

	if services, ok := healthResp["services"].(map[string]interface{}); ok {
		fmt.Println()
		fmt.Println("Service Health:")
		for name, svcStatus := range services {
			icon := "✓"
			if svcStatus != "healthy" && svcStatus != "ok" {
				icon = "⚠️ "
			}
			fmt.Printf("  %s %s: %v\n", icon, name, svcStatus)
		}
	}

	return nil
}

// checkProcess checks if a process is running
func checkProcess(processName, displayName string) {
	cmd := exec.Command("pgrep", "-f", processName)
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		fmt.Printf("❌ %s: Not running\n", displayName)
	} else {
		fmt.Printf("✅ %s: Running (PID: %s)\n", displayName, string(output[:len(output)-1]))
	}
}

// printServiceStatus prints service health status
func printServiceStatus(name, status string) {
	if status == "healthy" {
		fmt.Printf("  ✓ %s: %s\n", name, status)
	} else {
		fmt.Printf("  ⚠️  %s: %s\n", name, status)
	}
}

// getEnvOrDefault gets environment variable or returns default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
