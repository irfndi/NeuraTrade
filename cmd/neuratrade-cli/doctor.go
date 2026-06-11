package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"
)

type doctorReport struct {
	OK       []string
	Warnings []string
	Errors   []string
}

func cliDoctor(cCtx *cli.Context) error {
	home := defaultNeuraTradeHome()
	report := collectDoctorReport(home)
	printDoctorReport(home, report)
	if len(report.Errors) > 0 {
		return cli.Exit("doctor found blocking configuration errors", 1)
	}
	return nil
}

func collectDoctorReport(home string) doctorReport {
	var report doctorReport

	if strings.TrimSpace(home) == "" {
		report.Errors = append(report.Errors, "NEURATRADE_HOME resolved to an empty path")
		return report
	}
	if info, err := os.Stat(home); err != nil {
		if os.IsNotExist(err) {
			report.Warnings = append(report.Warnings, fmt.Sprintf("home directory does not exist yet: %s", home))
		} else {
			report.Errors = append(report.Errors, fmt.Sprintf("cannot inspect home directory: %v", err))
		}
	} else if !info.IsDir() {
		report.Errors = append(report.Errors, fmt.Sprintf("NEURATRADE_HOME is not a directory: %s", home))
	} else {
		report.OK = append(report.OK, "home directory exists")
	}

	runtimeCfg := validateRuntimeConfig(home, &report)
	secretsCfg := validateSecretsConfig(home, runtimeCfg, &report)
	validateRuntimeArtifacts(home, &report)

	if runtimeCfg != nil && secretsCfg != nil {
		report.OK = append(report.OK, "runtime and secret config files are both readable")
	}
	return report
}

func validateRuntimeConfig(home string, report *doctorReport) *runtimeConfig {
	path := runtimeConfigPath(home)
	cfg, err := loadRuntimeConfig(home)
	if err != nil {
		if os.IsNotExist(err) {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s is missing; run `neuratrade config init` to create deterministic runtime defaults", path))
			return nil
		}
		report.Errors = append(report.Errors, err.Error())
		return nil
	}
	report.OK = append(report.OK, "runtime config is readable")

	if !isValidPort(fmt.Sprintf("%d", cfg.Server.Port)) {
		report.Errors = append(report.Errors, "runtime server.port must be between 1 and 65535")
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Database.Driver))
	if driver != "sqlite" && driver != "postgres" {
		report.Errors = append(report.Errors, fmt.Sprintf("runtime database.driver must be sqlite or postgres, got %q", cfg.Database.Driver))
	}
	if driver == "sqlite" {
		if strings.TrimSpace(cfg.Database.SQLitePath) == "" {
			report.Errors = append(report.Errors, "runtime database.sqlite_path is required when database.driver=sqlite")
		} else if parent := filepath.Dir(cfg.Database.SQLitePath); parent != "." {
			if info, err := os.Stat(parent); err == nil && !info.IsDir() {
				report.Errors = append(report.Errors, fmt.Sprintf("runtime sqlite parent is not a directory: %s", parent))
			} else if os.IsNotExist(err) {
				report.Warnings = append(report.Warnings, fmt.Sprintf("runtime sqlite parent will be created on gateway start: %s", parent))
			}
		}
	}
	if cfg.Redis.Port != 0 && !isValidPort(fmt.Sprintf("%d", cfg.Redis.Port)) {
		report.Errors = append(report.Errors, "runtime redis.port must be between 1 and 65535")
	}
	if cfg.Gateway.CCXTPort != 0 && !isValidPort(fmt.Sprintf("%d", cfg.Gateway.CCXTPort)) {
		report.Errors = append(report.Errors, "runtime gateway.ccxt_port must be between 1 and 65535")
	}
	if cfg.Gateway.TelegramPort != 0 && !isValidPort(fmt.Sprintf("%d", cfg.Gateway.TelegramPort)) {
		report.Errors = append(report.Errors, "runtime gateway.telegram_port must be between 1 and 65535")
	}
	if cfg.Gateway.TelegramGRPCPort != 0 && !isValidPort(fmt.Sprintf("%d", cfg.Gateway.TelegramGRPCPort)) {
		report.Errors = append(report.Errors, "runtime gateway.telegram_grpc_port must be between 1 and 65535")
	}
	if cfg.Gateway.HealthTimeoutSeconds <= 0 {
		report.Errors = append(report.Errors, "runtime gateway.health_timeout_seconds must be positive")
	}
	if cfg.Gateway.SignalTimeoutSeconds <= 0 {
		report.Errors = append(report.Errors, "runtime gateway.signal_timeout_seconds must be positive")
	}
	if cfg.Gateway.GracefulTimeoutSeconds <= 0 {
		report.Errors = append(report.Errors, "runtime gateway.graceful_timeout_seconds must be positive")
	}
	if cfg.Features.EnableAI {
		if strings.TrimSpace(cfg.AI.Provider) == "" {
			report.Errors = append(report.Errors, "runtime ai.provider is required when features.enable_ai=true")
		}
		if strings.TrimSpace(cfg.AI.Model) == "" {
			report.Errors = append(report.Errors, "runtime ai.model is required when features.enable_ai=true")
		}
	}
	if cfg.AI.MaxTokens < 0 {
		report.Errors = append(report.Errors, "runtime ai.max_tokens cannot be negative")
	}
	if cfg.AI.MinConfidence < 0 || cfg.AI.MinConfidence > 1 {
		report.Errors = append(report.Errors, "runtime ai.min_confidence must be between 0 and 1")
	}
	return cfg
}

func validateSecretsConfig(home string, runtimeCfg *runtimeConfig, report *doctorReport) *localConfig {
	path := filepath.Join(home, "config.json")
	cfg, err := loadLocalConfig(home)
	if err != nil {
		if os.IsNotExist(err) {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s is missing; keep secrets in env or run `neuratrade config init`", path))
			return nil
		}
		report.Errors = append(report.Errors, err.Error())
		return nil
	}
	report.OK = append(report.OK, "secret config is readable")

	adminKey := configAdminAPIKey(cfg)
	if strings.TrimSpace(os.Getenv("ADMIN_API_KEY")) == "" && len(strings.TrimSpace(adminKey)) < 32 {
		report.Warnings = append(report.Warnings, "ADMIN_API_KEY is missing or shorter than 32 chars; gateway will generate an ephemeral key")
	}
	jwtSecret := configJWTSecret(cfg)
	if strings.TrimSpace(os.Getenv("JWT_SECRET")) == "" && len(strings.TrimSpace(jwtSecret)) < 32 {
		report.Warnings = append(report.Warnings, "JWT_SECRET is missing or shorter than 32 chars; gateway will generate an ephemeral secret")
	}
	aiEnabled := runtimeCfg == nil || runtimeCfg.Features.EnableAI
	if aiEnabled && strings.TrimSpace(os.Getenv("AI_API_KEY")) == "" && strings.TrimSpace(cfg.AI.APIKey) == "" {
		report.Warnings = append(report.Warnings, "AI_API_KEY is not configured; AI features may degrade to deterministic fallback")
	}
	if strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")) == "" && strings.TrimSpace(cfg.Telegram.BotToken) == "" {
		report.Warnings = append(report.Warnings, "TELEGRAM_BOT_TOKEN is not configured; Telegram service may be disabled in paper-only mode")
	}
	return cfg
}

func validateRuntimeArtifacts(home string, report *doctorReport) {
	statePath := filepath.Join(home, "pids", "gateway-state.json")
	content, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			report.Warnings = append(report.Warnings, "gateway state is missing; run `neuratrade gateway start` after configuration")
			return
		}
		report.Warnings = append(report.Warnings, fmt.Sprintf("cannot read gateway state: %v", err))
		return
	}
	var state gatewayRuntimeState
	if err := json.Unmarshal(content, &state); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("gateway state is not valid JSON: %v", err))
		return
	}
	if strings.TrimSpace(state.Mode) == "" {
		report.Warnings = append(report.Warnings, "gateway state has no mode")
		return
	}
	report.OK = append(report.OK, fmt.Sprintf("gateway state mode is %s", state.Mode))
}

func printDoctorReport(home string, report doctorReport) {
	fmt.Println("NeuraTrade Doctor")
	fmt.Println("=================")
	fmt.Printf("Home: %s\n", home)
	fmt.Printf("Runtime config: %s\n", runtimeConfigPath(home))
	fmt.Printf("Secret config: %s\n", filepath.Join(home, "config.json"))
	fmt.Println()

	for _, item := range report.OK {
		fmt.Printf("OK    %s\n", item)
	}
	for _, item := range report.Warnings {
		fmt.Printf("WARN  %s\n", item)
	}
	for _, item := range report.Errors {
		fmt.Printf("ERROR %s\n", item)
	}
	if len(report.Errors) == 0 {
		fmt.Println()
		fmt.Println("Doctor completed without blocking errors.")
	}
}
