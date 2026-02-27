// Package main is the entry point for the Agent Control Plane service.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentcontrol "github.com/irfndi/neuratrade/services/agent-control"
	
	
	
	
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Agent service failed: %v\n", err)
		os.Exit(1)
	}
}

// run orchestrates the agent service lifecycle.
func run() error {
	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize components
	auditLogger := agentcontrol.NewLogger(agentcontrol.AuditConfig{
		Level: agentcontrol.LevelInfo,
	})

	backendClient := agentcontrol.NewBackendClient(agentcontrol.ClientConfig{
		BaseURL:    getEnv("BACKEND_API_URL", "http://localhost:8080"),
		Timeout:    30 * time.Second,
		MaxRetries: 3,
	})

	eventIngestor := agentcontrol.NewIngestor(agentcontrol.IngestConfig{
		BackendEventURL: getEnv("BACKEND_EVENT_URL", "ws://localhost:8080/events"),
		BufferSize:      1024,
		ReconnectDelay:  5 * time.Second,
	})

	policyEngine := agentcontrol.NewEngine(agentcontrol.PolicyConfig{
		MaxOrderSize:     getEnvFloat("POLICY_MAX_ORDER_SIZE", 1.0),
		MaxLeverage:      getEnvFloat("POLICY_MAX_LEVERAGE", 5.0),
		MaxDailyLoss:     getEnvFloat("POLICY_MAX_DAILY_LOSS", 1000.0),
		AllowedExchanges: getEnvList("POLICY_ALLOWED_EXCHANGES", []string{"binance", "okx"}),
		SafeModeEnabled:  getEnvBool("POLICY_SAFE_MODE", false),
	})

	playbookRegistry := agentcontrol.NewRegistry()
	registerDefaultPlaybooks(playbookRegistry, backendClient, auditLogger)

	// Create agent runtime
	agent := agentcontrol.NewAgentRuntime(agentcontrol.AgentRuntimeConfig{
		AuditLogger:      auditLogger,
		BackendClient:    backendClient,
		EventIngestor:    eventIngestor,
		PolicyEngine:     policyEngine,
		PlaybookRegistry: playbookRegistry,
	})

	// Start agent
	if err := agent.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	// Wait for shutdown signal
	sig := <-sigChan
	fmt.Printf("Received signal %v, shutting down...\n", sig)

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := agent.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}

	fmt.Println("Agent service stopped gracefully")
	return nil
}

func registerDefaultPlaybooks(registry *agentcontrol.Registry, backendClient *agentcontrol.BackendClient, auditLogger *agentcontrol.Logger) {
	// Playbook: Pause exchange on error spikes
	registry.Register("pause_exchange_on_errors", agentcontrol.Playbook{
		Name:        "Pause Exchange on Error Spikes",
		Description: "Pause market data collection when error rate exceeds threshold",
		Execute: func(ctx context.Context, event any) error {
			exchangeID, ok := event.(string)
			if !ok {
				return fmt.Errorf("invalid event type")
			}
			auditLogger.Log(ctx, agentcontrol.ActionPlaybookExecuted, "pause_exchange_on_errors", map[string]any{
				"exchange_id": exchangeID,
				"reason":      "error_spike_detected",
			})
			return backendClient.PauseExchange(ctx, exchangeID)
		},
	})

	// Playbook: Enable safe mode on drawdown
	registry.Register("enable_safe_mode_on_drawdown", agentcontrol.Playbook{
		Name:        "Enable Safe Mode on Drawdown",
		Description: "Enable safe mode when daily drawdown exceeds limit",
		Execute: func(ctx context.Context, event any) error {
			auditLogger.Log(ctx, agentcontrol.ActionPlaybookExecuted, "enable_safe_mode_on_drawdown", map[string]any{
				"reason": "drawdown_limit_breached",
			})
			return backendClient.EnableSafeMode(ctx)
		},
	})

	// Playbook: Kill switch on critical failure
	registry.Register("kill_switch_on_critical", agentcontrol.Playbook{
		Name:        "Kill Switch on Critical Failure",
		Description: "Engage kill switch on critical system failure",
		Execute: func(ctx context.Context, event any) error {
			auditLogger.Log(ctx, agentcontrol.ActionPlaybookExecuted, "kill_switch_on_critical", map[string]any{
				"reason": "critical_failure_detected",
			})
			return backendClient.EngageKillSwitch(ctx)
		},
	})
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := getEnv(key, "")
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1"
}

func getEnvFloat(key string, defaultValue float64) float64 {
	value := getEnv(key, "")
	if value == "" {
		return defaultValue
	}
	var result float64
	fmt.Sscanf(value, "%f", &result)
	return result
}

func getEnvList(key string, defaultValue []string) []string {
	value := getEnv(key, "")
	if value == "" {
		return defaultValue
	}
	return splitCSV(value)
}

func splitCSV(s string) []string {
	result := []string{}
	for _, item := range split(s, ',') {
		trimmed := trim(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func split(s string, sep rune) []string {
	result := []string{}
	current := ""
	for _, r := range s {
		if r == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	result = append(result, current)
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
