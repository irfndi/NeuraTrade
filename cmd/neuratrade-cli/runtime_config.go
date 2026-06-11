package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shopspring/decimal"
)

const runtimeConfigFileName = "runtime.json"

type runtimeConfig struct {
	Server struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"server"`
	Database struct {
		Driver     string `json:"driver"`
		SQLitePath string `json:"sqlite_path"`
	} `json:"database"`
	Redis struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"redis"`
	CCXT struct {
		ServiceURL  string `json:"service_url"`
		GrpcAddress string `json:"grpc_address"`
	} `json:"ccxt"`
	Telegram struct {
		ServiceURL  string `json:"service_url"`
		GrpcAddress string `json:"grpc_address"`
		UsePolling  bool   `json:"use_polling"`
		ApiBaseURL  string `json:"api_base_url"`
	} `json:"telegram"`
	AI struct {
		Provider      string          `json:"provider"`
		Model         string          `json:"model"`
		BaseURL       string          `json:"base_url"`
		Temperature   float64         `json:"temperature"`
		MaxTokens     int             `json:"max_tokens"`
		MinConfidence float64         `json:"min_confidence"`
		DailyBudget   decimal.Decimal `json:"daily_budget"`
		RoutingMode   string          `json:"routing_mode"`
	} `json:"ai"`
	Features struct {
		EnableAI          bool `json:"enable_ai"`
		EnableAIScalping  bool `json:"enable_ai_scalping"`
		EnableAISignals   bool `json:"enable_ai_signals"`
		EnableAIArbitrage bool `json:"enable_ai_arbitrage"`
		PaperTrading      bool `json:"paper_trading"`
		RealTrading       bool `json:"real_trading"`
	} `json:"features"`
	Gateway struct {
		BindHost               string `json:"bind_host"`
		CCXTPort               int    `json:"ccxt_port"`
		TelegramPort           int    `json:"telegram_port"`
		TelegramGRPCPort       int    `json:"telegram_grpc_port"`
		Supervised             bool   `json:"supervised"`
		HealthTimeoutSeconds   int    `json:"health_timeout_seconds"`
		SignalTimeoutSeconds   int    `json:"signal_timeout_seconds"`
		GracefulTimeoutSeconds int    `json:"graceful_timeout_seconds"`
		SkipTelegram           bool   `json:"skip_telegram"`
	} `json:"gateway"`
}

func runtimeConfigPath(home string) string {
	return filepath.Join(home, runtimeConfigFileName)
}

func defaultRuntimeConfig(home string) runtimeConfig {
	defaultAI := defaultCLIAIProviderConfig()
	var cfg runtimeConfig
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8080
	cfg.Database.Driver = "sqlite"
	cfg.Database.SQLitePath = filepath.Join(home, "data", "neuratrade.db")
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 6379
	cfg.CCXT.ServiceURL = "http://localhost:3001"
	cfg.CCXT.GrpcAddress = "127.0.0.1:50051"
	cfg.Telegram.ServiceURL = "http://localhost:3002"
	cfg.Telegram.GrpcAddress = "127.0.0.1:50052"
	cfg.Telegram.UsePolling = true
	cfg.Telegram.ApiBaseURL = "http://localhost:8080"
	cfg.AI.Provider = defaultAI.Provider
	cfg.AI.Model = defaultAI.Model
	cfg.AI.BaseURL = defaultAI.BaseURL
	cfg.AI.Temperature = 0.7
	cfg.AI.MaxTokens = 4096
	cfg.AI.MinConfidence = 0.7
	cfg.AI.DailyBudget = decimal.NewFromInt(10)
	cfg.AI.RoutingMode = "primary"
	cfg.Features.EnableAI = true
	cfg.Features.EnableAIScalping = true
	cfg.Features.EnableAISignals = false
	cfg.Features.EnableAIArbitrage = false
	cfg.Features.PaperTrading = true
	cfg.Features.RealTrading = false
	cfg.Gateway.BindHost = "127.0.0.1"
	cfg.Gateway.CCXTPort = 3001
	cfg.Gateway.TelegramPort = 3002
	cfg.Gateway.TelegramGRPCPort = 50052
	cfg.Gateway.Supervised = false
	cfg.Gateway.HealthTimeoutSeconds = gatewayDefaultHealthTimeoutSeconds
	cfg.Gateway.SignalTimeoutSeconds = 5
	cfg.Gateway.GracefulTimeoutSeconds = 10
	cfg.Gateway.SkipTelegram = false
	return cfg
}

func runtimeConfigFromLocalConfig(home string, local *localConfig) runtimeConfig {
	cfg := defaultRuntimeConfig(home)
	if local == nil {
		return cfg
	}
	if strings.TrimSpace(local.Server.Host) != "" {
		cfg.Server.Host = strings.TrimSpace(local.Server.Host)
	}
	if local.Server.Port > 0 {
		cfg.Server.Port = local.Server.Port
	}
	if strings.TrimSpace(local.Database.Driver) != "" {
		cfg.Database.Driver = strings.TrimSpace(local.Database.Driver)
	}
	if strings.TrimSpace(local.Database.SQLitePath) != "" {
		cfg.Database.SQLitePath = strings.TrimSpace(local.Database.SQLitePath)
	}
	if strings.TrimSpace(local.CCXT.ServiceURL) != "" {
		cfg.CCXT.ServiceURL = strings.TrimSpace(local.CCXT.ServiceURL)
	}
	if strings.TrimSpace(local.CCXT.GrpcAddress) != "" {
		cfg.CCXT.GrpcAddress = strings.TrimSpace(local.CCXT.GrpcAddress)
	}
	if strings.TrimSpace(local.Telegram.ServiceURL) != "" {
		cfg.Telegram.ServiceURL = strings.TrimSpace(local.Telegram.ServiceURL)
	}
	if strings.TrimSpace(local.Telegram.GrpcAddress) != "" {
		cfg.Telegram.GrpcAddress = strings.TrimSpace(local.Telegram.GrpcAddress)
	}
	if strings.TrimSpace(local.Telegram.ApiBaseURL) != "" {
		cfg.Telegram.ApiBaseURL = strings.TrimSpace(local.Telegram.ApiBaseURL)
	}
	if strings.TrimSpace(local.AI.Provider) != "" {
		cfg.AI.Provider = strings.TrimSpace(local.AI.Provider)
	}
	if strings.TrimSpace(local.AI.Model) != "" {
		cfg.AI.Model = strings.TrimSpace(local.AI.Model)
	}
	if strings.TrimSpace(local.AI.BaseURL) != "" {
		cfg.AI.BaseURL = strings.TrimSpace(local.AI.BaseURL)
	}
	return cfg
}

func loadRuntimeConfig(home string) (*runtimeConfig, error) {
	content, err := os.ReadFile(runtimeConfigPath(home))
	if err != nil {
		return nil, fmt.Errorf("read runtime config: %w", err)
	}
	var cfg runtimeConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("parse runtime config: %w", err)
	}
	return &cfg, nil
}

func getRuntimeConfigValue(home string) *runtimeConfig {
	cfg, err := loadRuntimeConfig(home)
	if err != nil {
		return nil
	}
	return cfg
}

func writeRuntimeConfig(home string, cfg runtimeConfig) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal runtime config: %w", err)
	}
	if err := os.WriteFile(runtimeConfigPath(home), data, 0o644); err != nil {
		return fmt.Errorf("write runtime config: %w", err)
	}
	return nil
}

func runtimeString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func runtimePort(value int, fallback string) string {
	if value > 0 && value < 65536 {
		return fmt.Sprintf("%d", value)
	}
	return fallback
}
