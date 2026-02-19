package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type localConfig struct {
	TelegramTestChatID string `json:"telegram_test_chat_id"`
	Server struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"server"`
	Database struct {
		SQLitePath string `json:"sqlite_path"`
	} `json:"database"`
	CCXT struct {
		AdminAPIKey string `json:"admin_api_key"`
		ServiceURL  string `json:"service_url"`
		GrpcAddress string `json:"grpc_address"`
	} `json:"ccxt"`
	Telegram struct {
		BotToken   string `json:"bot_token"`
		ApiBaseURL string `json:"api_base_url"`
		ServiceURL string `json:"service_url"`
		ChatID     string `json:"chat_id"`
	} `json:"telegram"`
	Services struct {
		Telegram struct {
			ChatID string `json:"chat_id"`
		} `json:"telegram"`
	} `json:"services"`
	Security struct {
		AdminAPIKey string `json:"admin_api_key"`
	} `json:"security"`
	AI struct {
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"ai"`
}

func defaultNeuraTradeHome() string {
	if v := os.Getenv("NEURATRADE_HOME"); v != "" {
		return v
	}
	return os.ExpandEnv("${HOME}/.neuratrade")
}

func loadLocalConfig(home string) (*localConfig, error) {
	configPath := filepath.Join(home, "config.json")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg localConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

func getConfigValue(home string) *localConfig {
	cfg, err := loadLocalConfig(home)
	if err != nil {
		return nil
	}
	return cfg
}

func configAdminAPIKey(cfg *localConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.Security.AdminAPIKey != "" {
		return cfg.Security.AdminAPIKey
	}
	return cfg.CCXT.AdminAPIKey
}

func configChatID(cfg *localConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.Telegram.ChatID != "" {
		return cfg.Telegram.ChatID
	}
	if cfg.Services.Telegram.ChatID != "" {
		return cfg.Services.Telegram.ChatID
	}
	return cfg.TelegramTestChatID
}
