package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

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

type gatewayServiceRuntime struct {
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

type gatewayRuntimeState struct {
	Mode                 string                           `json:"mode"`
	Supervised           bool                             `json:"supervised"`
	UpdatedAt            string                           `json:"updated_at"`
	HealthTimeoutSeconds int                              `json:"health_timeout_seconds"`
	Services             map[string]gatewayServiceRuntime `json:"services"`
}

type serviceProbeResult struct {
	healthy bool
	detail  string
}

var allowedGatewayServiceBinaries = newAllowedGatewayServiceBinaries()

func newAllowedGatewayServiceBinaries() map[string]struct{} {
	defaults := []string{"neuratrade-server", "telegram-service", "ccxt-service"}
	defaultSet := make(map[string]struct{}, len(defaults))
	for _, binary := range defaults {
		defaultSet[binary] = struct{}{}
	}

	raw := strings.TrimSpace(os.Getenv("GATEWAY_ALLOWED_BINARIES"))
	if raw == "" {
		return defaultSet
	}

	customSet := make(map[string]struct{})
	for _, entry := range strings.Split(raw, ",") {
		candidate := strings.TrimSpace(entry)
		if candidate == "" {
			continue
		}
		base := filepath.Base(candidate)
		if base == "" || base == "." || base == ".." {
			continue
		}
		customSet[base] = struct{}{}
	}
	if len(customSet) == 0 {
		return defaultSet
	}
	return customSet
}

// isNativeCCXTMode returns true if the gateway should skip launching the external
// ccxt-service process because CCXT is embedded natively in the backend server.
func isNativeCCXTMode() bool {
	// If CCXT_SERVICE_URL or CCXT_GRPC_ADDRESS is explicitly set, the user
	// wants external CCXT service mode.
	if os.Getenv("CCXT_SERVICE_URL") != "" {
		return false
	}
	if os.Getenv("CCXT_GRPC_ADDRESS") != "" {
		return false
	}
	return true
}

// gatewayStart starts all NeuraTrade services
func gatewayStart(cCtx *cli.Context) error {
	fmt.Println("🚀 Starting NeuraTrade Gateway...")
	fmt.Println()

	home := defaultNeuraTradeHome()
	cfg := getConfigValue(home)
	statePath := filepath.Join(home, "pids", "gateway-state.json")
	servicePIDFiles := []string{
		filepath.Join(home, "pids", "backend.pid"),
		filepath.Join(home, "pids", "ccxt.pid"),
		filepath.Join(home, "pids", "telegram.pid"),
	}

	backendPort := resolveBackendPort(cfg)

	ccxtPort := getEnvOrDefault("CCXT_PORT", "3001")
	telegramPort := getEnvOrDefault("TELEGRAM_PORT", "3002")
	bindHost := getEnvOrDefault("BIND_HOST", "127.0.0.1")
	supervised := cCtx.Bool("supervised") || getEnvBoolDefault("NEURATRADE_GATEWAY_SUPERVISED", false)
	healthTimeout := getEnvDurationSeconds("NEURATRADE_GATEWAY_HEALTH_TIMEOUT_SECONDS", 90)
	signalTimeout := getEnvDurationSeconds("NEURATRADE_GATEWAY_SIGNAL_TIMEOUT_SECONDS", 5)
	gracefulTimeout := getEnvDurationSeconds("NEURATRADE_GATEWAY_GRACEFUL_TIMEOUT_SECONDS", 10)
	adminAPIKey := normalizeAdminAPIKey(getEnvOrDefault("ADMIN_API_KEY", configAdminAPIKey(cfg)))
	jwtSecret := normalizeJWTSecret(getEnvOrDefault("JWT_SECRET", configJWTSecret(cfg)))

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
	fmt.Printf("🛡️  Supervised Mode: %t\n", supervised)
	fmt.Printf("⏱️  Health Timeout: %s\n", healthTimeout.Round(time.Second))
	fmt.Println()

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Join(home, "logs"), 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "pids"), 0755); err != nil {
		return fmt.Errorf("failed to create pids directory: %w", err)
	}
	writeGatewayState(statePath, gatewayRuntimeState{
		Mode:                 "starting",
		Supervised:           supervised,
		UpdatedAt:            time.Now().UTC().Format(time.RFC3339),
		HealthTimeoutSeconds: int(healthTimeout.Seconds()),
		Services: map[string]gatewayServiceRuntime{
			"backend":  {Status: "starting", Endpoint: fmt.Sprintf("http://%s:%s/health", bindHost, backendPort)},
			"telegram": {Status: "starting", Endpoint: fmt.Sprintf("http://%s:%s/health", bindHost, telegramPort)},
			"ccxt":     {Status: "starting", Endpoint: fmt.Sprintf("http://%s:%s/health", bindHost, ccxtPort)},
		},
	})

	// Get executable directory
	execDir, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execDir = filepath.Dir(execDir)

	// Start CCXT Service (skip if using native embedded CCXT mode)
	var ccxtCmd *exec.Cmd
	nativeCCXT := isNativeCCXTMode()
	if nativeCCXT {
		fmt.Println("📊 CCXT: Using native embedded mode (skipping external service)")
	} else {
		fmt.Println("📊 Starting CCXT Service...")
		var startErr error
		ccxtCmd, startErr = startService(
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
		if startErr != nil {
			writeGatewayStateMode(statePath, "down", "ccxt failed to start")
			cleanupGatewayRuntimeArtifacts(statePath, "ccxt failed to start", servicePIDFiles...)
			return fmt.Errorf("failed to start CCXT service: %w", startErr)
		}
		fmt.Println("✅ CCXT Service started")
	}

	// Build backend environment map. Only inject CCXT service URLs when using
	// the external CCXT service; in native mode these keys must be absent so
	// the backend detects embedded CCXT.
	backendEnv := map[string]string{
		"PORT":                  backendPort,
		"SERVER_PORT":           backendPort,
		"BACKEND_HOST_PORT":     backendPort,
		"HOST":                  "0.0.0.0", // Backend binds to all interfaces
		"DATABASE_DRIVER":       getEnvOrDefault("DATABASE_DRIVER", "sqlite"),
		"SQLITE_PATH":           sqlitePath,
		"SQLITE_DB_PATH":        sqlitePath,
		"REDIS_HOST":            getEnvOrDefault("REDIS_HOST", "localhost"),
		"REDIS_PORT":            getEnvOrDefault("REDIS_PORT", "6379"),
		"TELEGRAM_SERVICE_URL":  fmt.Sprintf("http://%s:%s", bindHost, telegramPort),
		"TELEGRAM_GRPC_ADDRESS": fmt.Sprintf("%s:%s", bindHost, getEnvOrDefault("TELEGRAM_GRPC_PORT", "50052")),
		"JWT_SECRET":            jwtSecret,
		"ADMIN_API_KEY":         adminAPIKey,
		"SENTRY_ENVIRONMENT":    getEnvOrDefault("SENTRY_ENVIRONMENT", "production"),
		"SENTRY_DSN":            getEnvOrDefault("SENTRY_DSN", ""),
		"AI_API_KEY":            aiAPIKey,
		"AI_BASE_URL":           aiBaseURL,
		"AI_PROVIDER":           aiProvider,
		"AI_MODEL":              aiModel,
	}
	if !nativeCCXT {
		if os.Getenv("CCXT_SERVICE_URL") == "" {
			backendEnv["CCXT_SERVICE_URL"] = fmt.Sprintf("http://%s:%s", bindHost, ccxtPort)
		}
		if os.Getenv("CCXT_GRPC_ADDRESS") == "" {
			backendEnv["CCXT_GRPC_ADDRESS"] = fmt.Sprintf("%s:%s", bindHost, getEnvOrDefault("CCXT_GRPC_PORT", "50051"))
		}
	}

	// Start Backend API
	fmt.Println("🔧 Starting Backend API...")
	backendCmd, err := startService(
		filepath.Join(execDir, "neuratrade-server"),
		"Backend API",
		filepath.Join(home, "logs", "backend.log"),
		backendEnv,
		filepath.Join(home, "pids", "backend.pid"),
	)
	if err != nil {
		signalAndWait(ccxtCmd, signalTimeout)
		cleanupGatewayRuntimeArtifacts(statePath, "backend failed to start", servicePIDFiles...)
		return fmt.Errorf("failed to start backend API: %w", err)
	}
	backendHealthURL := fmt.Sprintf("http://%s:%s/health", bindHost, backendPort)
	backendProbe := waitForServiceHealthy("Backend API", backendHealthURL, healthTimeout)
	if backendProbe.healthy {
		fmt.Println("✅ Backend API started")
		writeGatewayServiceState(statePath, "backend", "healthy", backendProbe.detail, backendHealthURL)
	} else if supervised {
		fmt.Printf("⚠️  Backend API still warming: %s\n", backendProbe.detail)
		writeGatewayServiceState(statePath, "backend", "warming", backendProbe.detail, backendHealthURL)
		writeGatewayStateMode(statePath, "warming", "backend warming up")
	} else {
		signalAndWait(backendCmd, signalTimeout)
		signalAndWait(ccxtCmd, signalTimeout)
		writeGatewayServiceState(statePath, "backend", "down", backendProbe.detail, backendHealthURL)
		cleanupGatewayRuntimeArtifacts(statePath, "backend health check failed", servicePIDFiles...)
		return fmt.Errorf("%s", backendProbe.detail)
	}

	// Start Telegram Service
	fmt.Println("📞 Starting Telegram Service...")
	telegramCmd, err := startService(
		filepath.Join(execDir, "telegram-service"),
		"Telegram Service",
		filepath.Join(home, "logs", "telegram.log"),
		map[string]string{
			"PORT":                  telegramPort,
			"BIND_HOST":             bindHost,
			"TELEGRAM_BOT_TOKEN":    telegramToken,
			"TELEGRAM_USE_POLLING":  getEnvOrDefault("TELEGRAM_USE_POLLING", "true"),
			"TELEGRAM_API_BASE_URL": fmt.Sprintf("http://%s:%s", bindHost, backendPort),
			"BACKEND_HOST_PORT":     backendPort,
			"NODE_ENV":              "production",
			"ADMIN_API_KEY":         adminAPIKey,
		},
		filepath.Join(home, "pids", "telegram.pid"),
	)
	if err != nil {
		signalAndWait(backendCmd, signalTimeout)
		signalAndWait(ccxtCmd, signalTimeout)
		cleanupGatewayRuntimeArtifacts(statePath, "telegram failed to start", servicePIDFiles...)
		return fmt.Errorf("failed to start Telegram service: %w", err)
	}
	telegramHealthURL := fmt.Sprintf("http://%s:%s/health", bindHost, telegramPort)
	telegramProbe := waitForServiceHealthy("Telegram Service", telegramHealthURL, healthTimeout)
	if telegramProbe.healthy {
		fmt.Println("✅ Telegram Service started")
		writeGatewayServiceState(statePath, "telegram", "healthy", telegramProbe.detail, telegramHealthURL)
	} else if supervised {
		fmt.Printf("⚠️  Telegram service still warming: %s\n", telegramProbe.detail)
		writeGatewayServiceState(statePath, "telegram", "warming", telegramProbe.detail, telegramHealthURL)
		writeGatewayStateMode(statePath, "warming", "telegram warming up")
	} else {
		signalAndWait(telegramCmd, signalTimeout)
		signalAndWait(backendCmd, signalTimeout)
		signalAndWait(ccxtCmd, signalTimeout)
		writeGatewayServiceState(statePath, "telegram", "down", telegramProbe.detail, telegramHealthURL)
		cleanupGatewayRuntimeArtifacts(statePath, "telegram health check failed", servicePIDFiles...)
		return fmt.Errorf("%s", telegramProbe.detail)
	}

	if ccxtCmd != nil {
		writeGatewayServiceState(statePath, "ccxt", "healthy", "process started", fmt.Sprintf("http://%s:%s/health", bindHost, ccxtPort))
	} else {
		writeGatewayServiceState(statePath, "ccxt", "embedded", "native mode (embedded in backend)", "")
	}

	initialMode := "healthy"
	if !backendProbe.healthy || !telegramProbe.healthy {
		initialMode = "warming"
	}
	writeGatewayStateMode(statePath, initialMode, "services started")
	fmt.Println()
	if initialMode == "healthy" {
		fmt.Println("🎉 All services started successfully!")
	} else {
		fmt.Println("⏳ Services started in warmup mode (supervised)")
	}
	fmt.Println()
	fmt.Printf("📡 Backend API: http://localhost:%s\n", backendPort)
	fmt.Printf("🏥 Health Check: http://localhost:%s/health\n", backendPort)
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop all services")

	monitorStop := make(chan struct{})
	go monitorGatewayHealth(statePath, bindHost, backendPort, telegramPort, backendCmd, telegramCmd, ccxtCmd, monitorStop)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	close(monitorStop)

	fmt.Println()
	fmt.Println("🛑 Shutting down services...")

	// Graceful shutdown: signal all processes in parallel, then wait sequentially
	if backendCmd != nil && backendCmd.Process != nil {
		backendCmd.Process.Signal(syscall.SIGTERM)
	}
	if telegramCmd != nil && telegramCmd.Process != nil {
		telegramCmd.Process.Signal(syscall.SIGTERM)
	}
	if ccxtCmd != nil && ccxtCmd.Process != nil {
		ccxtCmd.Process.Signal(syscall.SIGTERM)
	}
	waitForExit(backendCmd, gracefulTimeout)
	waitForExit(telegramCmd, gracefulTimeout)
	waitForExit(ccxtCmd, gracefulTimeout)
	cleanupGatewayRuntimeArtifacts(statePath, "gateway stopped", servicePIDFiles...)

	fmt.Println("✅ All services stopped")
	return nil
}

// startService starts a service process and writes its PID to a file.
func startService(binary, name, logFile string, env map[string]string, pidFile string) (*exec.Cmd, error) {
	resolvedBinary, err := resolveServiceBinary(binary)
	if err != nil {
		return nil, fmt.Errorf("resolve %s executable: %w", name, err)
	}
	cmd, err := commandForAllowedBinary(resolvedBinary)
	if err != nil {
		return nil, fmt.Errorf("build %s command: %w", name, err)
	}

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
		return nil, fmt.Errorf("start %s process: %w", name, err)
	}

	// Write PID file for later cleanup
	if pidFile != "" {
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644); err != nil {
			fmt.Printf("⚠️  Warning: Could not write PID file for %s: %v\n", name, err)
		}
	}

	return cmd, nil
}

func commandForAllowedBinary(resolvedBinary string) (*exec.Cmd, error) {
	base := filepath.Base(strings.TrimSpace(resolvedBinary))
	switch base {
	case "neuratrade-server":
		cmd := exec.Command("neuratrade-server")
		cmd.Path = resolvedBinary
		return cmd, nil
	case "telegram-service":
		cmd := exec.Command("telegram-service")
		cmd.Path = resolvedBinary
		return cmd, nil
	case "ccxt-service":
		cmd := exec.Command("ccxt-service")
		cmd.Path = resolvedBinary
		return cmd, nil
	default:
		return nil, fmt.Errorf("binary %q is not allowlisted", base)
	}
}

func resolveServiceBinary(binary string) (string, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return "", fmt.Errorf("binary path is empty")
	}

	base := filepath.Base(binary)
	if _, ok := allowedGatewayServiceBinaries[base]; !ok {
		return "", fmt.Errorf("binary %q is not allowlisted", base)
	}

	resolved, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("look up binary %q: %w", binary, err)
	}
	return resolved, nil
}

// gatewayStop stops all NeuraTrade services by reading PID files and sending SIGTERM
func gatewayStop(cCtx *cli.Context) error {
	fmt.Println("🛑 Stopping NeuraTrade services...")
	fmt.Println()

	home := defaultNeuraTradeHome()
	pidsDir := filepath.Join(home, "pids")

	services := []struct {
		name            string
		pidFile         string
		processPatterns []string
	}{
		{"Backend API", "backend.pid", []string{"neuratrade-server"}},
		{"CCXT Service", "ccxt.pid", []string{"ccxt-service"}},
		{"Telegram Service", "telegram.pid", []string{"telegram-service", "bun run index.ts"}},
	}

	stoppedCount := 0
	for _, svc := range services {
		pidFile := filepath.Join(pidsDir, svc.pidFile)
		if err := stopServiceByPIDFile(svc.name, pidFile, svc.processPatterns...); err != nil {
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
		fmt.Println("  pkill -f 'bun run index.ts'")
		return fmt.Errorf("no services stopped")
	}

	fmt.Println()
	fmt.Printf("✅ Stopped %d service(s)\n", stoppedCount)
	return nil
}

// stopServiceByPIDFile reads a PID file and sends SIGTERM to the process
func stopServiceByPIDFile(name, pidFile string, expectedPatterns ...string) error {
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

	if len(expectedPatterns) > 0 {
		matches, matchErr := processMatchesAnyPattern(pid, expectedPatterns...)
		if matchErr != nil {
			return fmt.Errorf("failed to validate process pattern for PID %d: %w", pid, matchErr)
		}
		if !matches {
			_ = os.Remove(pidFile)
			return fmt.Errorf("stale PID file (PID %d does not match any expected process pattern, removed)", pid)
		}
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	fmt.Printf("✅ %s: Stopped (PID: %d)\n", name, pid)
	os.Remove(pidFile)
	return nil
}

func processMatchesAnyPattern(pid int, expectedPatterns ...string) (bool, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("ps command failed for PID %d: %w", pid, err)
	}
	command := strings.ToLower(strings.TrimSpace(string(output)))
	for _, pattern := range expectedPatterns {
		if strings.Contains(command, strings.ToLower(pattern)) {
			return true, nil
		}
	}
	return false, nil
}

// gatewayStatus shows the status of NeuraTrade services
func gatewayStatus(cCtx *cli.Context) error {
	home := defaultNeuraTradeHome()
	statePath := filepath.Join(home, "pids", "gateway-state.json")

	fmt.Println("📊 NeuraTrade Service Status")
	fmt.Println("============================")
	fmt.Println()

	if state, ok := readGatewayState(statePath); ok {
		fmt.Printf("Runtime Mode: %s\n", strings.ToUpper(state.Mode))
		if state.UpdatedAt != "" {
			fmt.Printf("Last Update: %s\n", state.UpdatedAt)
		}
		if state.Supervised {
			fmt.Println("Supervision: ENABLED")
		}
		fmt.Println()
	}

	// Check if processes are running
	checkProcess("Backend API", "neuratrade-server")
	checkProcess("CCXT Service", "ccxt-service")
	checkProcess("Telegram Service", "telegram-service", "bun run index.ts")

	fmt.Println()

	// Check health endpoint
	backendPort := resolveBackendPort(getConfigValue(defaultNeuraTradeHome()))
	bindHost := getEnvOrDefault("BIND_HOST", "127.0.0.1")
	probeHost := bindHost
	if probeHost == "0.0.0.0" || probeHost == "::" {
		probeHost = "127.0.0.1"
	}
	fmt.Printf("🚪 Health Check: http://%s:%s/health\n", probeHost, backendPort)
	fmt.Println()

	// Try to get health
	baseURL := fmt.Sprintf("http://%s:%s", probeHost, backendPort)
	apiKey := getAPIKey()
	client := NewAPIClient(baseURL, apiKey)

	respBody, err := client.makeRequest("GET", "/health", nil)
	if err != nil {
		fmt.Printf("⚠️  Backend health probe failed: %v\n", err)
		if state, ok := readGatewayState(statePath); ok {
			if strings.EqualFold(state.Mode, "warming") || strings.EqualFold(state.Mode, "degraded") {
				fmt.Printf("Gateway runtime mode is %s (services may still be warming).\n", strings.ToUpper(state.Mode))
				return nil
			}
		}
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
func checkProcess(displayName string, processPatterns ...string) {
	seen := make(map[string]struct{})

	for _, pattern := range processPatterns {
		cmd := exec.Command("pgrep", "-f", pattern)
		output, err := cmd.Output()
		if err != nil || len(output) == 0 {
			continue
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			pidField := strings.TrimSpace(strings.Fields(line)[0])
			if pidField == "" {
				continue
			}

			if _, exists := seen[pidField]; exists {
				continue
			}

			cmdline, readErr := readCommandLineForPID(pidField)
			if readErr != nil || strings.TrimSpace(cmdline) == "" {
				continue
			}
			cmdlineLower := strings.ToLower(strings.TrimSpace(cmdline))
			if strings.Contains(cmdlineLower, "pgrep -f") ||
				strings.Contains(cmdlineLower, "pkill -f") ||
				strings.Contains(cmdlineLower, "gateway status") {
				continue
			}
			if !strings.Contains(cmdlineLower, strings.ToLower(pattern)) {
				continue
			}

			seen[pidField] = struct{}{}
			fmt.Printf("✅ %s: Running (PID: %s)\n", displayName, pidField)
			return
		}
	}
	fmt.Printf("❌ %s: Not running\n", displayName)
}

func readCommandLineForPID(pid string) (string, error) {
	if strings.TrimSpace(pid) == "" {
		return "", fmt.Errorf("empty pid")
	}
	cmd := exec.Command("ps", "-p", pid, "-o", "command=")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ps command failed for PID %s: %w", pid, err)
	}
	return strings.TrimSpace(string(output)), nil
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

func getEnvBoolDefault(key string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return defaultValue
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func getEnvDurationSeconds(key string, defaultSeconds int) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return time.Duration(defaultSeconds) * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return time.Duration(defaultSeconds) * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func resolveBackendPort(cfg *localConfig) string {
	candidates := []string{
		os.Getenv("SERVER_PORT"),
		os.Getenv("PORT"),
		os.Getenv("BACKEND_HOST_PORT"),
	}
	for _, candidate := range candidates {
		if isValidPort(candidate) {
			return candidate
		}
	}
	if cfg != nil && cfg.Server.Port > 0 {
		return strconv.Itoa(cfg.Server.Port)
	}
	return "8080"
}

func isValidPort(port string) bool {
	if strings.TrimSpace(port) == "" {
		return false
	}
	num, err := strconv.Atoi(port)
	return err == nil && num > 0 && num < 65536
}

func normalizeAdminAPIKey(adminAPIKey string) string {
	key := strings.TrimSpace(adminAPIKey)
	if len(key) >= 32 {
		return key
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		fmt.Println("⚠️  Warning: Failed to generate secure ADMIN_API_KEY, using deterministic fallback")
		return "neuratrade-generated-admin-key-32chars"
	}
	generated := hex.EncodeToString(buf)
	fmt.Println("⚠️  Warning: ADMIN_API_KEY was missing/too short; generated an ephemeral secure key for this session")
	return generated
}

func normalizeJWTSecret(jwtSecret string) string {
	key := strings.TrimSpace(jwtSecret)
	if len(key) >= 32 {
		return key
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		fmt.Println("⚠️  Warning: Failed to generate secure JWT_SECRET, using deterministic fallback")
		return "neuratrade-generated-jwt-secret-min-32"
	}
	generated := hex.EncodeToString(buf)
	fmt.Println("⚠️  Warning: JWT_SECRET was missing/too short; generated an ephemeral secure key for this session")
	return generated
}

func waitForServiceHealthy(name, url string, timeout time.Duration) serviceProbeResult {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	lastDetail := "no response yet"
	for time.Now().Before(deadline) {
		resp, err := client.Get(url) // #nosec G107 -- URL is constructed from local fixed host/port.
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return serviceProbeResult{
					healthy: true,
					detail:  fmt.Sprintf("%s reachable (%d)", name, resp.StatusCode),
				}
			}
			lastDetail = fmt.Sprintf("%s returned %d", name, resp.StatusCode)
		} else {
			lastDetail = err.Error()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return serviceProbeResult{
		healthy: false,
		detail:  fmt.Sprintf("%s failed health check at %s within %s (%s)", name, url, timeout.String(), lastDetail),
	}
}

func monitorGatewayHealth(
	statePath, bindHost, backendPort, telegramPort string,
	backendCmd, telegramCmd, ccxtCmd *exec.Cmd,
	stop <-chan struct{},
) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	backendURL := fmt.Sprintf("http://%s:%s/health", bindHost, backendPort)
	telegramURL := fmt.Sprintf("http://%s:%s/health", bindHost, telegramPort)
	httpClient := &http.Client{Timeout: 2 * time.Second}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			backendUp := processRunning(backendCmd)
			telegramUp := processRunning(telegramCmd)
			embeddedCCXT := ccxtCmd == nil
			var ccxtUp bool

			backendHealthy := probeHTTPHealthy(httpClient, backendURL)
			telegramHealthy := probeHTTPHealthy(httpClient, telegramURL)

			writeGatewayServiceState(statePath, "backend", serviceRuntimeState(backendUp, backendHealthy), "", backendURL)
			writeGatewayServiceState(statePath, "telegram", serviceRuntimeState(telegramUp, telegramHealthy), "", telegramURL)

			if embeddedCCXT {
				ccxtUp = backendUp
				writeGatewayServiceState(statePath, "ccxt", serviceRuntimeState(ccxtUp, backendHealthy), "embedded", "")
			} else {
				ccxtUp = processRunning(ccxtCmd)
				writeGatewayServiceState(statePath, "ccxt", serviceRuntimeState(ccxtUp, true), "", "")
			}

			mode := deriveGatewayMode(backendUp, telegramUp, ccxtUp, backendHealthy, telegramHealthy)
			writeGatewayStateMode(statePath, mode, "runtime monitor")
		}
	}
}

func deriveGatewayMode(backendUp, telegramUp, ccxtUp, backendHealthy, telegramHealthy bool) string {
	if !backendUp && !telegramUp && !ccxtUp {
		return "down"
	}
	if backendUp && telegramUp && backendHealthy && telegramHealthy {
		return "healthy"
	}
	// Services are up but probes are not yet passing: treat as startup warming.
	if (backendUp || telegramUp) && !backendHealthy && !telegramHealthy {
		return "warming"
	}
	return "degraded"
}

func serviceRuntimeState(processUp, healthy bool) string {
	switch {
	case !processUp:
		return "down"
	case processUp && healthy:
		return "healthy"
	default:
		return "warming"
	}
}

// signalAndWait sends SIGTERM to a process and waits up to shutdownTimeout
// for it to exit. If the process is nil, it is a no-op.
func signalAndWait(cmd *exec.Cmd, shutdownTimeout time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(shutdownTimeout):
		cmd.Process.Kill()
		<-done
	}
}

func waitForExit(cmd *exec.Cmd, timeout time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		cmd.Process.Kill()
		<-done
	}
}

func processRunning(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	return syscall.Kill(cmd.Process.Pid, 0) == nil
}

func probeHTTPHealthy(client *http.Client, url string) bool {
	resp, err := client.Get(url) // #nosec G107 -- internal local URL probe.
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func writeGatewayState(path string, state gatewayRuntimeState) {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, payload, 0644)
}

func readGatewayState(path string) (gatewayRuntimeState, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return gatewayRuntimeState{}, false
	}
	var state gatewayRuntimeState
	if err := json.Unmarshal(content, &state); err != nil {
		return gatewayRuntimeState{}, false
	}
	if state.Services == nil {
		state.Services = make(map[string]gatewayServiceRuntime)
	}
	return state, true
}

func writeGatewayStateMode(path, mode, detail string) {
	state, ok := readGatewayState(path)
	if !ok {
		state = gatewayRuntimeState{
			Services: make(map[string]gatewayServiceRuntime),
		}
	}
	state.Mode = mode
	if detail != "" {
		if state.Services == nil {
			state.Services = make(map[string]gatewayServiceRuntime)
		}
		state.Services["gateway"] = gatewayServiceRuntime{
			Status: mode,
			Detail: detail,
		}
	}
	writeGatewayState(path, state)
}

func writeGatewayServiceState(path, serviceName, status, detail, endpoint string) {
	state, ok := readGatewayState(path)
	if !ok {
		state = gatewayRuntimeState{
			Services: make(map[string]gatewayServiceRuntime),
		}
	}
	if state.Services == nil {
		state.Services = make(map[string]gatewayServiceRuntime)
	}
	state.Services[serviceName] = gatewayServiceRuntime{
		Status:   status,
		Detail:   detail,
		Endpoint: endpoint,
	}
	writeGatewayState(path, state)
}

func cleanupGatewayRuntimeArtifacts(statePath, detail string, pidFiles ...string) {
	markGatewayStopped(statePath, detail)
	for _, pidFile := range pidFiles {
		if strings.TrimSpace(pidFile) == "" {
			continue
		}
		_ = os.Remove(pidFile)
	}
}

func markGatewayStopped(statePath, detail string) {
	state, ok := readGatewayState(statePath)
	if !ok {
		state = gatewayRuntimeState{Services: make(map[string]gatewayServiceRuntime)}
	}
	if state.Services == nil {
		state.Services = make(map[string]gatewayServiceRuntime)
	}

	state.Mode = "down"
	state.Services["gateway"] = gatewayServiceRuntime{Status: "down", Detail: detail}
	for _, serviceName := range []string{"backend", "ccxt", "telegram"} {
		service := state.Services[serviceName]
		service.Status = "down"
		service.Detail = detail
		state.Services[serviceName] = service
	}

	writeGatewayState(statePath, state)
}
