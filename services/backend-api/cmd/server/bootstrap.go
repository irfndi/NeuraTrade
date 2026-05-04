// Package main provides the NeuraTrade CLI bootstrap command.
// This command initializes the application environment and configuration.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ai"
)

// BootstrapCommand handles the bootstrap/setup process for NeuraTrade.
type BootstrapCommand struct {
	reader *bufio.Reader
}

// NewBootstrapCommand creates a new bootstrap command instance.
func NewBootstrapCommand() *BootstrapCommand {
	return &BootstrapCommand{
		reader: bufio.NewReader(os.Stdin),
	}
}

// Run executes the bootstrap process.
func (b *BootstrapCommand) Run() error {
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║         NeuraTrade CLI Bootstrap                         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("This command will help you set up NeuraTrade for first use.")
	fmt.Println()

	// Check if .env exists
	envPath := filepath.Join(".", ".env")
	if _, err := os.Stat(envPath); err == nil {
		fmt.Print(".env file already exists. Overwrite? (y/N): ")
		response, _ := b.reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(response)) != "y" {
			fmt.Println("Bootstrap cancelled.")
			return nil
		}
	}

	// Collect configuration
	config := b.collectConfiguration()

	// Generate .env file
	if err := b.generateEnvFile(envPath, config); err != nil {
		return fmt.Errorf("failed to generate .env file: %w", err)
	}

	// Create necessary directories
	if err := b.createDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	fmt.Println()
	fmt.Println("✅ Bootstrap completed successfully!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review and update the .env file with your settings")
	fmt.Println("  2. Run 'make dev-setup' to start dependencies")
	fmt.Println("  3. Run 'make migrate' to set up the database")
	fmt.Println("  4. Run 'make run' to start the application")
	fmt.Println()

	return nil
}

// Config holds the bootstrap configuration values.
type Config struct {
	Environment      string
	DatabaseHost     string
	DatabasePort     string
	DatabaseName     string
	DatabaseUser     string
	DatabasePassword string
	RedisHost        string
	RedisPort        string
	JWTSecret        string
	TelegramBotToken string
	SentryDSN        string
	AIProvider       string
	AIModel          string
	AIAPIKey         string
	AIBaseURL        string
}

func (b *BootstrapCommand) collectConfiguration() *Config {
	config := &Config{}

	fmt.Println("📋 Configuration Setup")
	fmt.Println("----------------------")
	fmt.Println("Press Enter to accept default values shown in brackets.")
	fmt.Println()

	// Environment
	fmt.Print("Environment [development]: ")
	config.Environment = b.readInput("development")

	// Database configuration
	fmt.Println()
	fmt.Println("🗄️  Database Configuration")
	fmt.Print("Database Host [localhost]: ")
	config.DatabaseHost = b.readInput("localhost")

	fmt.Print("Database Port [5432]: ")
	config.DatabasePort = b.readInput("5432")

	fmt.Print("Database Name [neuratrade]: ")
	config.DatabaseName = b.readInput("neuratrade")

	fmt.Print("Database User [postgres]: ")
	config.DatabaseUser = b.readInput("postgres")

	fmt.Print("Database Password [postgres]: ")
	config.DatabasePassword = b.readInput("postgres")

	// Redis configuration
	fmt.Println()
	fmt.Println("🔄 Redis Configuration")
	fmt.Print("Redis Host [localhost]: ")
	config.RedisHost = b.readInput("localhost")

	fmt.Print("Redis Port [6379]: ")
	config.RedisPort = b.readInput("6379")

	// Security
	fmt.Println()
	fmt.Println("🔐 Security Configuration")
	fmt.Print("JWT Secret (leave blank for auto-generated): ")
	config.JWTSecret = b.readInput(generateRandomSecret())

	// Optional integrations
	fmt.Println()
	fmt.Println("🔌 Optional Integrations (press Enter to skip)")
	fmt.Print("Telegram Bot Token: ")
	config.TelegramBotToken = b.readInput("")

	fmt.Print("Sentry DSN: ")
	config.SentryDSN = b.readInput("")

	// AI Model configuration — defaults are suggestions; runtime resolves from models.dev registry
	fmt.Println()
	fmt.Println("🤖 AI Model Configuration")
	fmt.Println("Configure one AI provider to get started. You can add more later.")
	fmt.Println("Providers: openai, anthropic, google, deepseek, minimax, zhipu")
	fmt.Println()

	fmt.Print("AI Provider [deepseek]: ")
	config.AIProvider = b.readInput("deepseek")

	for {
		defaults, ok := ai.ProviderDefaults(config.AIProvider)
		if !ok {
			config.AIModel = ""
			config.AIBaseURL = ""
			fmt.Printf("Unknown provider %q. Use: openai, anthropic, google, deepseek, minimax, zhipu\n", config.AIProvider)
			fmt.Print("AI Provider [deepseek]: ")
			config.AIProvider = b.readInput("deepseek")
			continue
		}
		config.AIModel = defaults.DefaultModel
		config.AIBaseURL = defaults.BaseURL
		break
	}

	fmt.Printf("AI Model [%s]: ", config.AIModel)
	config.AIModel = b.readInput(config.AIModel)

	fmt.Print("AI API Key: ")
	config.AIAPIKey = b.readInput("")

	return config
}

func (b *BootstrapCommand) readInput(defaultValue string) string {
	input, _ := b.reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue
	}
	return input
}

func (b *BootstrapCommand) generateEnvFile(path string, config *Config) error {
	content := fmt.Sprintf(`# NeuraTrade Environment Configuration
# Generated by 'neuratrade bootstrap' on %s

# Application
APP_NAME=neuratrade
ENVIRONMENT=%s
LOG_LEVEL=info

# Database
DATABASE_DRIVER=postgres
DATABASE_HOST=%s
DATABASE_PORT=%s
DATABASE_NAME=%s
DATABASE_USER=%s
DATABASE_PASSWORD=%s
DATABASE_SSL_MODE=disable
DATABASE_MAX_CONNS=10
DATABASE_MAX_IDLE_CONNS=5

# Redis
REDIS_HOST=%s
REDIS_PORT=%s
REDIS_PASSWORD=
REDIS_DB=0

# Security
JWT_SECRET=%s
JWT_EXPIRATION_HOURS=24

# CCXT Service
CCXT_SERVICE_URL=http://localhost:3001
CCXT_ADMIN_API_KEY=change-me-in-production

# Telegram (optional)
TELEGRAM_BOT_TOKEN=%s
TELEGRAM_WEBHOOK_URL=
TELEGRAM_WEBHOOK_SECRET=

# Sentry (optional)
SENTRY_DSN=%s
SENTRY_ENVIRONMENT=%s

# Trading
MINIMUM_PROFIT_THRESHOLD=0.1
MAX_TRADE_AMOUNT_USD=1000
ENABLE_PAPER_TRADING=true

# AI Configuration (single-model-first)
AI_PROVIDER=%s
AI_MODEL=%s
AI_API_KEY=%s
AI_BASE_URL=%s
AI_ROUTING_MODE=primary
`, timeNow(), config.Environment, config.DatabaseHost, config.DatabasePort,
		config.DatabaseName, config.DatabaseUser, config.DatabasePassword,
		config.RedisHost, config.RedisPort, config.JWTSecret,
		config.TelegramBotToken, config.SentryDSN, config.Environment,
		config.AIProvider, config.AIModel, config.AIAPIKey, config.AIBaseURL)

	return os.WriteFile(path, []byte(content), 0600)
}

func (b *BootstrapCommand) createDirectories() error {
	dirs := []string{
		"logs",
		"data",
		"tmp",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		fmt.Printf("  📁 Created directory: %s/\n", dir)
	}

	return nil
}

func timeNow() string {
	return time.Now().UTC().Format("2006-01-02")
}

func generateRandomSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}
