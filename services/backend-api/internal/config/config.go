package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config aggregates all configuration settings for the application.
type Config struct {
	// Environment indicates the running environment (e.g., "development", "production").
	Environment string `mapstructure:"environment"`
	// LogLevel sets the global logging verbosity.
	LogLevel string `mapstructure:"log_level"`
	// Server holds configuration for the HTTP server.
	Server ServerConfig `mapstructure:"server"`
	// Database holds configuration for the database connection.
	Database DatabaseConfig `mapstructure:"database"`
	// Redis holds configuration for the Redis connection.
	Redis RedisConfig `mapstructure:"redis"`
	// CCXT holds configuration for the CCXT service integration.
	CCXT CCXTConfig `mapstructure:"ccxt"`
	// Telegram holds configuration for Telegram bot notifications.
	Telegram TelegramConfig `mapstructure:"telegram"`
	// Telemetry holds configuration for OpenTelemetry integration.
	Telemetry TelemetryConfig `mapstructure:"telemetry"`
	// Sentry holds configuration for Sentry error tracking.
	Sentry SentryConfig `mapstructure:"sentry"`
	// Cleanup holds configuration for data retention and cleanup tasks.
	Cleanup CleanupConfig `mapstructure:"cleanup"`
	// Backfill holds configuration for historical data backfilling.
	Backfill BackfillConfig `mapstructure:"backfill"`
	// MarketData holds configuration for market data collection.
	MarketData MarketDataConfig `mapstructure:"market_data"`
	// Arbitrage holds configuration for arbitrage detection logic.
	Arbitrage ArbitrageConfig `mapstructure:"arbitrage"`
	// Blacklist holds configuration for the symbol blacklist mechanism.
	Blacklist BlacklistConfig `mapstructure:"blacklist"`
	// Auth holds configuration for authentication.
	Auth AuthConfig `mapstructure:"auth"`
	// Security holds configuration for security features like encryption.
	Security SecurityConfig `mapstructure:"security"`
	// Fees holds configuration for exchange fee defaults.
	Fees FeesConfig `mapstructure:"fees"`
	// Analytics holds configuration for analytics features.
	Analytics AnalyticsConfig `mapstructure:"analytics"`
	// Wallet holds configuration for wallet validation.
	Wallet     WalletValidatorConfig `mapstructure:"wallet"`
	Indicators IndicatorsConfig      `mapstructure:"indicators"`
	// AI holds configuration for AI-driven trading.
	AI AIConfig `mapstructure:"ai"`
	// Features holds feature flags.
	Features FeaturesConfig `mapstructure:"features"`
}

// ServerConfig defines the HTTP server settings.
type ServerConfig struct {
	// Port is the TCP port the server listens on.
	Port int `mapstructure:"port"`
	// AllowedOrigins is a list of CORS allowed origins.
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// DatabaseConfig defines the PostgreSQL database connection settings.
type DatabaseConfig struct {
	Driver string `mapstructure:"driver"`
	// Host is the database server hostname or IP.
	Host string `mapstructure:"host"`
	// Port is the database server port.
	Port int `mapstructure:"port"`
	// User is the database username.
	User string `mapstructure:"user"`
	// Password is the database password.
	Password string `mapstructure:"password"`
	// DBName is the name of the database to connect to.
	DBName string `mapstructure:"dbname"`
	// SSLMode defines the SSL connection mode.
	SSLMode string `mapstructure:"sslmode"`
	// DatabaseURL is a connection string that can override individual fields.
	DatabaseURL string `mapstructure:"database_url"`
	// MaxOpenConns is the maximum number of open connections.
	MaxOpenConns int `mapstructure:"max_open_conns"`
	// MaxIdleConns is the maximum number of idle connections.
	MaxIdleConns int `mapstructure:"max_idle_conns"`
	// ConnMaxLifetime is the maximum connection lifetime.
	ConnMaxLifetime string `mapstructure:"conn_max_lifetime"`
	// ConnMaxIdleTime is the maximum idle connection lifetime.
	ConnMaxIdleTime string `mapstructure:"conn_max_idle_time"`
	// PostgreSQL 18 specific optimizations
	ApplicationName           string `mapstructure:"application_name"`
	ConnectTimeout            int    `mapstructure:"connect_timeout"`
	StatementTimeout          int    `mapstructure:"statement_timeout"`
	QueryTimeout              int    `mapstructure:"query_timeout"` // Alias for StatementTimeout for compatibility
	PoolTimeout               int    `mapstructure:"pool_timeout"`
	PoolHealthCheckPeriod     int    `mapstructure:"pool_health_check_period"`
	PoolMaxLifetime           int    `mapstructure:"pool_max_lifetime"`
	PoolIdleTimeout           int    `mapstructure:"pool_idle_timeout"`
	EnableAsync               bool   `mapstructure:"enable_async"`
	AsyncBatchSize            int    `mapstructure:"async_batch_size"`
	AsyncConcurrency          int    `mapstructure:"async_concurrency"`
	SQLitePath                string `mapstructure:"sqlite_path"`
	SQLiteVectorExtensionPath string `mapstructure:"sqlite_vector_extension_path"`
}

// RedisConfig defines the Redis connection settings.
type RedisConfig struct {
	// Host is the Redis server hostname.
	Host string `mapstructure:"host"`
	// Port is the Redis server port.
	Port int `mapstructure:"port"`
	// Password is the Redis authentication password.
	Password string `mapstructure:"password"`
	// DB is the Redis database index to use.
	DB int `mapstructure:"db"`
	// PoolSize is the maximum number of connections in the connection pool.
	PoolSize int `mapstructure:"pool_size"`
	// MinIdleConns is the minimum number of idle connections.
	MinIdleConns int `mapstructure:"min_idle_conns"`
	// DialTimeout is the timeout for establishing new connections.
	DialTimeout int `mapstructure:"dial_timeout"`
	// ReadTimeout is the timeout for read operations.
	ReadTimeout int `mapstructure:"read_timeout"`
	// WriteTimeout is the timeout for write operations.
	WriteTimeout int `mapstructure:"write_timeout"`
	// PoolTimeout is the timeout for getting a connection from the pool.
	PoolTimeout int `mapstructure:"pool_timeout"`
	// DefaultTTL is the default TTL for cached items in seconds.
	DefaultTTL int `mapstructure:"default_ttl"`
}

// CCXTConfig defines settings for interacting with the CCXT microservice.
type CCXTConfig struct {
	// ServiceURL is the base URL of the CCXT service.
	ServiceURL string `mapstructure:"service_url"`
	// GrpcAddress is the gRPC address of the CCXT service.
	GrpcAddress string `mapstructure:"grpc_address"`
	// Timeout is the request timeout in seconds.
	Timeout int `mapstructure:"timeout"`
	// AdminAPIKey is the API key for authenticating with admin endpoints.
	AdminAPIKey string `mapstructure:"admin_api_key"`
}

// TelegramConfig defines settings for the Telegram notification bot.
type TelegramConfig struct {
	// ServiceURL is the base URL of the Telegram service.
	ServiceURL string `mapstructure:"service_url"`
	// GrpcAddress is the gRPC address of the Telegram service.
	GrpcAddress string `mapstructure:"grpc_address"`
	// AdminAPIKey is the API key for authenticating with the Telegram service.
	AdminAPIKey string `mapstructure:"admin_api_key"`
	// BotToken is the authentication token for the Telegram bot.
	BotToken string `mapstructure:"bot_token"`
	// WebhookURL is the URL where Telegram should send updates.
	WebhookURL string `mapstructure:"webhook_url"`
	// UsePolling indicates whether to use long polling instead of webhooks.
	UsePolling bool `mapstructure:"use_polling"`
	// PollingOffset is the offset for the next update in polling mode.
	PollingOffset int `mapstructure:"polling_offset"`
	// PollingLimit is the maximum number of updates to retrieve per request.
	PollingLimit int `mapstructure:"polling_limit"`
	// PollingTimeout is the timeout in seconds for long polling.
	PollingTimeout int `mapstructure:"polling_timeout"`
}

// TelemetryConfig defines settings for OpenTelemetry.
type TelemetryConfig struct {
	// Enabled controls whether telemetry is active.
	Enabled bool `mapstructure:"enabled"`
	// ServiceName is the name of the service for tracing.
	ServiceName string `mapstructure:"service_name"`
	// ServiceVersion is the version of the service.
	ServiceVersion string `mapstructure:"service_version"`
	// LogLevel sets the log level for telemetry components.
	LogLevel string `mapstructure:"log_level"`
}

// SentryConfig defines settings for Sentry error reporting.
type SentryConfig struct {
	// Enabled controls whether Sentry reporting is active.
	Enabled bool `mapstructure:"enabled"`
	// DSN is the Data Source Name for the Sentry project.
	DSN string `mapstructure:"dsn"`
	// Environment is the environment tag sent to Sentry.
	Environment string `mapstructure:"environment"`
	// Release is the release version tag.
	Release string `mapstructure:"release"`
	// TracesSampleRate is the percentage of transactions to trace.
	TracesSampleRate float64 `mapstructure:"traces_sample_rate"`
	// ProfilesSampleRate is the percentage of profiles to capture.
	ProfilesSampleRate float64 `mapstructure:"profiles_sample_rate"`
}

// CleanupConfig defines policies for data retention and cleanup.
type CleanupConfig struct {
	// MarketData configures retention for market data.
	MarketData CleanupDataConfig `mapstructure:"market_data"`
	// FundingRates configures retention for funding rate data.
	FundingRates CleanupDataConfig `mapstructure:"funding_rates"`
	// ArbitrageOpportunities configures retention for arbitrage opportunity records.
	ArbitrageOpportunities CleanupArbitrageConfig `mapstructure:"arbitrage_opportunities"`
	// IntervalMinutes is the frequency of cleanup job execution.
	IntervalMinutes int `mapstructure:"interval"`
	// EnableSmartCleanup enables more intelligent cleanup strategies.
	EnableSmartCleanup bool `mapstructure:"enable_smart_cleanup"`
}

// CleanupDataConfig defines retention policies for general data.
type CleanupDataConfig struct {
	// RetentionHours is the number of hours to keep data.
	RetentionHours int `mapstructure:"retention_hours"`
	// DeletionHours defines a secondary deletion threshold.
	DeletionHours int `mapstructure:"deletion_hours"`
}

// CleanupArbitrageConfig defines retention policies for arbitrage data.
type CleanupArbitrageConfig struct {
	// RetentionHours is the number of hours to keep arbitrage records.
	RetentionHours int `mapstructure:"retention_hours"`
}

// BackfillConfig defines settings for historical data backfilling.
type BackfillConfig struct {
	// Enabled controls whether backfilling is active.
	Enabled bool `mapstructure:"enabled"`
	// BackfillHours is the duration of history to backfill.
	BackfillHours int `mapstructure:"backfill_hours"`
	// MinDataThresholdHours is the minimum amount of data required to skip backfill.
	MinDataThresholdHours int `mapstructure:"min_data_threshold_hours"`
	// BatchSize is the number of items to process in one backfill batch.
	BatchSize int `mapstructure:"batch_size"`
	// DelayBetweenBatches is the pause time in milliseconds between batches.
	DelayBetweenBatches int `mapstructure:"delay_between_batches"`
}

// MarketDataConfig defines settings for market data collection.
type MarketDataConfig struct {
	// CollectionInterval is the time string for how often to collect data.
	CollectionInterval string `mapstructure:"collection_interval"`
	// BatchSize is the number of symbols/requests to process at once.
	BatchSize int `mapstructure:"batch_size"`
	// MaxRetries is the number of times to retry failed requests.
	MaxRetries int `mapstructure:"max_retries"`
	// Timeout is the request timeout string.
	Timeout string `mapstructure:"timeout"`
	// Exchanges is a list of exchange names to collect data from.
	Exchanges []string `mapstructure:"exchanges"`
}

// ArbitrageConfig defines settings for arbitrage detection.
type ArbitrageConfig struct {
	// MinProfitThreshold is the minimum profit percentage required.
	MinProfitThreshold float64 `mapstructure:"min_profit_threshold"`
	// MaxTradeAmount is the maximum capital to allocate for a trade.
	MaxTradeAmount float64 `mapstructure:"max_trade_amount"`
	// CheckInterval is the string duration between checks.
	CheckInterval string `mapstructure:"check_interval"`
	// EnabledPairs is a list of trading pairs to monitor.
	EnabledPairs []string `mapstructure:"enabled_pairs"`
	// Enabled controls whether the arbitrage service is running.
	Enabled bool `mapstructure:"enabled"`
	// IntervalSeconds is the interval in seconds between checks.
	IntervalSeconds int `mapstructure:"interval_seconds"`
	// MaxAgeMinutes is the maximum age of data to consider valid.
	MaxAgeMinutes int `mapstructure:"max_age_minutes"`
	// BatchSize is the processing batch size.
	BatchSize int `mapstructure:"batch_size"`
}

// BlacklistConfig defines settings for the symbol blacklist.
type BlacklistConfig struct {
	// TTL is the default Time To Live for blacklisted symbols.
	TTL string `mapstructure:"ttl"`
	// ShortTTL is a shorter TTL for temporary issues.
	ShortTTL string `mapstructure:"short_ttl"`
	// LongTTL is a longer TTL for persistent issues.
	LongTTL string `mapstructure:"long_ttl"`
	// UseRedis controls whether to persist the blacklist in Redis.
	UseRedis bool `mapstructure:"use_redis"`
	// RetryAfterClear controls whether to immediately retry symbols after they expire.
	RetryAfterClear bool `mapstructure:"retry_after_clear"`
}

// AuthConfig defines authentication settings.
type AuthConfig struct {
	// JWTSecret is the secret key used for signing JWT tokens.
	JWTSecret string `mapstructure:"jwt_secret"`
}

// SecurityConfig defines security-related settings.
type SecurityConfig struct {
	// EncryptionKey is the base64-encoded 32-byte key for AES-256-GCM encryption.
	// Used for encrypting sensitive data like exchange API keys.
	EncryptionKey string `mapstructure:"encryption_key"`
}

// FeesConfig defines default fees used when exchange-specific data is missing.
type FeesConfig struct {
	// DefaultTakerFee is the fallback taker fee (decimal percent, e.g., 0.001 = 0.1%).
	DefaultTakerFee float64 `mapstructure:"default_taker_fee"`
	// DefaultMakerFee is the fallback maker fee (decimal percent).
	DefaultMakerFee float64 `mapstructure:"default_maker_fee"`
}

// AnalyticsConfig defines settings for analytics features.
type AnalyticsConfig struct {
	EnableForecasting       bool    `mapstructure:"enable_forecasting"`
	EnableCorrelation       bool    `mapstructure:"enable_correlation"`
	EnableRegimeDetection   bool    `mapstructure:"enable_regime_detection"`
	ForecastLookback        int     `mapstructure:"forecast_lookback"`
	ForecastHorizon         int     `mapstructure:"forecast_horizon"`
	CorrelationWindow       int     `mapstructure:"correlation_window"`
	CorrelationMinPoints    int     `mapstructure:"correlation_min_points"`
	RegimeShortWindow       int     `mapstructure:"regime_short_window"`
	RegimeLongWindow        int     `mapstructure:"regime_long_window"`
	VolatilityHighThreshold float64 `mapstructure:"volatility_high_threshold"`
	VolatilityLowThreshold  float64 `mapstructure:"volatility_low_threshold"`
}

type WalletValidatorConfig struct {
	MinimumUSDCBalance         float64 `mapstructure:"minimum_usdc_balance"`
	MinimumPortfolioValue      float64 `mapstructure:"minimum_portfolio_value"`
	MinimumExchangeConnections int     `mapstructure:"minimum_exchange_connections"`
}

type IndicatorsConfig struct {
	Provider string `mapstructure:"provider"`
}

// Load reads the configuration from the config file and environment variables.
//
// The config is loaded from the following locations (in order of precedence):
//  1. ~/.neuratrade/config.json (local user config - highest priority for user overrides)
//  2. ./config.yml (project config)
//  3. Environment variables (highest priority)
//
// Returns:
//
//	*Config: The loaded configuration structure.
//	error: An error if the configuration could not be parsed.

// AIConfig holds AI-driven trading configuration.
type AIConfig struct {
	Provider      string  `mapstructure:"provider"`
	Model         string  `mapstructure:"model"`
	APIKey        string  `mapstructure:"api_key"`
	BaseURL       string  `mapstructure:"base_url"`
	Temperature   float64 `mapstructure:"temperature"`
	MaxTokens     int     `mapstructure:"max_tokens"`
	MinConfidence float64 `mapstructure:"min_confidence"`
	DailyBudget   float64 `mapstructure:"daily_budget"`
}

// FeaturesConfig holds feature flags.
type FeaturesConfig struct {
	EnableAI          bool `mapstructure:"enable_ai"`
	EnableAIScalping  bool `mapstructure:"enable_ai_scalping"`
	EnableAISignals   bool `mapstructure:"enable_ai_signals"`
	EnableAIArbitrage bool `mapstructure:"enable_ai_arbitrage"`
}

func Load() (*Config, error) {
	// First: Add ~/.neuratrade/ config path (user local config)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		viper.AddConfigPath(filepath.Join(homeDir, ".neuratrade"))
	}

	// Second: Add project config path
	viper.SetConfigName("config")
	// Don't set explicit config type - let viper auto-detect from file extension
	viper.AddConfigPath(".")

	// Also support JSON config from ~/.neuratrade/ (only if homeDir is available)
	// Note: viper.AddConfigPath is cumulative, so this adds to the search paths
	if err == nil {
		viper.AddConfigPath(filepath.Join(homeDir, ".neuratrade"))
	}

	// Set default values
	setDefaults()

	// Enable environment variable support
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Map legacy environment variables for JWT secret
	_ = viper.BindEnv("auth.jwt_secret", "JWT_SECRET")
	_ = viper.BindEnv("security.jwt_secret", "JWT_SECRET")

	// Bind encryption key for API key storage
	_ = viper.BindEnv("security.encryption_key", "ENCRYPTION_KEY")

	// Bind standard DATABASE_URL
	_ = viper.BindEnv("database.database_url", "DATABASE_URL")
	_ = viper.BindEnv("database.driver", "DATABASE_DRIVER")
	_ = viper.BindEnv("database.sqlite_path", "SQLITE_PATH")
	_ = viper.BindEnv("database.sqlite_vector_extension_path", "SQLITE_VEC_EXTENSION_PATH")

	// Bind database environment variables explicitly
	_ = viper.BindEnv("database.host", "DATABASE_HOST")
	_ = viper.BindEnv("database.port", "DATABASE_PORT")
	_ = viper.BindEnv("database.user", "DATABASE_USER")
	_ = viper.BindEnv("database.password", "DATABASE_PASSWORD")
	_ = viper.BindEnv("database.dbname", "DATABASE_DBNAME")
	_ = viper.BindEnv("database.sslmode", "DATABASE_SSLMODE")

	// Bind CCXT service environment variables
	_ = viper.BindEnv("ccxt.service_url", "CCXT_SERVICE_URL")
	_ = viper.BindEnv("ccxt.grpc_address", "CCXT_GRPC_ADDRESS")
	_ = viper.BindEnv("ccxt.admin_api_key", "ADMIN_API_KEY")

	// Bind Telegram service environment variables
	_ = viper.BindEnv("telegram.service_url", "TELEGRAM_SERVICE_URL")
	_ = viper.BindEnv("telegram.grpc_address", "TELEGRAM_GRPC_ADDRESS")

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		// Config file not found, use defaults and environment variables
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	// Sanitize Sentry DSN (remove surrounding spaces)
	if config.Sentry.DSN != "" {
		config.Sentry.DSN = strings.TrimSpace(config.Sentry.DSN)
	}

	// Backfill JWT secret from legacy security configuration if needed
	if config.Auth.JWTSecret == "" {
		config.Auth.JWTSecret = strings.TrimSpace(viper.GetString("auth.jwt_secret"))
	}
	if config.Auth.JWTSecret == "" {
		config.Auth.JWTSecret = strings.TrimSpace(viper.GetString("security.jwt_secret"))
	}

	// Validate critical security settings
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// setDefaults initializes the default configuration values in Viper.
func setDefaults() {
	// Environment
	viper.SetDefault("environment", "development")
	viper.SetDefault("log_level", "info")

	// Server
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.allowed_origins", []string{"http://localhost:3000"})

	// Bind PORT env to server.port
	_ = viper.BindEnv("server.port", "PORT")

	// Set database defaults - SQLite by default
	viper.SetDefault("database.driver", "sqlite")
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.user", "postgres")
	// Set default password for testing (must be overridden in production)
	viper.SetDefault("database.password", "change-me-in-production")
	viper.SetDefault("database.dbname", "neuratrade")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("database.database_url", "")
	viper.SetDefault("database.max_open_conns", 25)
	viper.SetDefault("database.max_idle_conns", 5)
	viper.SetDefault("database.conn_max_lifetime", "300s")
	viper.SetDefault("database.conn_max_idle_time", "60s")
	viper.SetDefault("database.sqlite_path", "neuratrade.db")
	viper.SetDefault("database.sqlite_vector_extension_path", "")

	// Redis
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)

	// CCXT - Use Docker service names when running in Docker/Coolify
	// Note: These defaults can be overridden by explicit env vars (CCXT_SERVICE_URL, CCXT_GRPC_ADDRESS)
	// which are bound via viper.BindEnv() in Load(). Viper prioritizes env vars over defaults.
	//
	// Detect Coolify environment by checking for COOLIFY_* variables
	// Coolify sets COOLIFY_CONTAINER_NAME, COOLIFY_RESOURCE_UUID, etc., not just COOLIFY=true
	isCoolify := os.Getenv("COOLIFY_CONTAINER_NAME") != "" ||
		os.Getenv("COOLIFY_RESOURCE_UUID") != "" ||
		os.Getenv("COOLIFY") == "true"
	isDocker := os.Getenv("DOCKER_ENVIRONMENT") == "true" || isCoolify

	// Log detection result for debugging (only if log package is available)
	if isDocker {
		log.Printf("INFO: Docker/Coolify environment detected (COOLIFY=%v, DOCKER_ENVIRONMENT=%s)",
			isCoolify, os.Getenv("DOCKER_ENVIRONMENT"))
		viper.SetDefault("ccxt.service_url", "http://ccxt-service:3001")
		viper.SetDefault("ccxt.grpc_address", "ccxt-service:50051")
	} else {
		viper.SetDefault("ccxt.service_url", "http://localhost:3001")
		// Use explicit IPv4 address to avoid IPv6 resolution issues on some systems
		viper.SetDefault("ccxt.grpc_address", "127.0.0.1:50051")
	}
	viper.SetDefault("ccxt.timeout", 30)

	// Telegram - Use Docker service names when running in Docker/Coolify
	// Note: These defaults can be overridden by explicit env vars (TELEGRAM_SERVICE_URL, TELEGRAM_GRPC_ADDRESS)
	if isDocker {
		viper.SetDefault("telegram.service_url", "http://telegram-service:3002")
		viper.SetDefault("telegram.grpc_address", "telegram-service:50052")
	} else {
		viper.SetDefault("telegram.service_url", "http://localhost:3002")
		// Use explicit IPv4 address to avoid IPv6 resolution issues on some systems
		viper.SetDefault("telegram.grpc_address", "127.0.0.1:50052")
	}
	viper.SetDefault("telegram.admin_api_key", "")
	viper.SetDefault("telegram.bot_token", "")
	viper.SetDefault("telegram.webhook_url", "")
	viper.SetDefault("telegram.use_polling", false)
	viper.SetDefault("telegram.polling_offset", 0)
	viper.SetDefault("telegram.polling_limit", 100)
	viper.SetDefault("telegram.polling_timeout", 60)

	// Telemetry
	viper.SetDefault("telemetry.enabled", true)
	viper.SetDefault("telemetry.service_name", "github.com/irfndi/neuratrade")
	viper.SetDefault("telemetry.service_version", "1.0.0")
	viper.SetDefault("telemetry.log_level", "info")

	// Sentry
	viper.SetDefault("sentry.enabled", false)
	viper.SetDefault("sentry.dsn", "")
	viper.SetDefault("sentry.environment", viper.GetString("environment"))
	viper.SetDefault("sentry.release", "")
	viper.SetDefault("sentry.traces_sample_rate", 0.2)
	viper.SetDefault("sentry.profiles_sample_rate", 0.0)

	// Cleanup - Enhanced configuration with smart cleanup
	viper.SetDefault("cleanup.market_data.retention_hours", 36)
	viper.SetDefault("cleanup.market_data.deletion_hours", 12)
	viper.SetDefault("cleanup.funding_rates.retention_hours", 36)
	viper.SetDefault("cleanup.funding_rates.deletion_hours", 12)
	viper.SetDefault("cleanup.arbitrage_opportunities.retention_hours", 72)
	viper.SetDefault("cleanup.interval", 60)
	viper.SetDefault("cleanup.enable_smart_cleanup", true)

	// Backfill
	viper.SetDefault("backfill.enabled", false)
	viper.SetDefault("backfill.backfill_hours", 6)
	viper.SetDefault("backfill.min_data_threshold_hours", 4)
	viper.SetDefault("backfill.batch_size", 5)
	viper.SetDefault("backfill.delay_between_batches", 500)

	// Market Data
	viper.SetDefault("market_data.collection_interval", "5m")
	viper.SetDefault("market_data.batch_size", 100)
	viper.SetDefault("market_data.max_retries", 3)
	viper.SetDefault("market_data.timeout", "15s")
	viper.SetDefault("market_data.exchanges", []string{"binance", "coinbase", "kraken", "bitfinex", "huobi"})

	// Arbitrage
	viper.SetDefault("arbitrage.enabled", true)
	viper.SetDefault("arbitrage.interval_seconds", 60)
	viper.SetDefault("arbitrage.min_profit_threshold", 0.5)
	viper.SetDefault("arbitrage.max_age_minutes", 30)
	viper.SetDefault("arbitrage.batch_size", 100)
	viper.SetDefault("arbitrage.max_trade_amount", 1000.0)
	viper.SetDefault("arbitrage.check_interval", "2m")
	viper.SetDefault("arbitrage.enabled_pairs", []string{"BTC/USDT", "ETH/USDT", "BNB/USDT", "ADA/USDT"})

	// Blacklist
	viper.SetDefault("blacklist.ttl", "24h")
	viper.SetDefault("blacklist.short_ttl", "1h")
	viper.SetDefault("blacklist.long_ttl", "72h")
	viper.SetDefault("blacklist.use_redis", true)
	viper.SetDefault("blacklist.retry_after_clear", true)

	// Auth
	viper.SetDefault("auth.jwt_secret", "")

	// Security
	viper.SetDefault("security.encryption_key", "")

	// Fees
	viper.SetDefault("fees.default_taker_fee", 0.001)
	viper.SetDefault("fees.default_maker_fee", 0.001)

	// Analytics
	viper.SetDefault("analytics.enable_forecasting", true)
	viper.SetDefault("analytics.enable_correlation", true)
	viper.SetDefault("analytics.enable_regime_detection", true)
	viper.SetDefault("analytics.forecast_lookback", 120)
	viper.SetDefault("analytics.forecast_horizon", 8)
	viper.SetDefault("analytics.correlation_window", 200)
	viper.SetDefault("analytics.correlation_min_points", 30)
	viper.SetDefault("analytics.regime_short_window", 20)
	viper.SetDefault("analytics.regime_long_window", 60)
	viper.SetDefault("analytics.volatility_high_threshold", 0.03)
	viper.SetDefault("analytics.volatility_low_threshold", 0.005)

	// Wallet validation
	viper.SetDefault("wallet.minimum_usdc_balance", 100.0)
	viper.SetDefault("wallet.minimum_portfolio_value", 500.0)
	viper.SetDefault("wallet.minimum_exchange_connections", 1)

	viper.SetDefault("indicators.provider", "talib")

	// AI config defaults
	viper.SetDefault("ai.provider", "minimax")
	viper.SetDefault("ai.model", "")
	viper.SetDefault("ai.api_key", "")
	viper.SetDefault("ai.base_url", "https://api.minimax.chat/v1")
	viper.SetDefault("ai.temperature", 0.7)
	viper.SetDefault("ai.max_tokens", 4096)
	viper.SetDefault("ai.min_confidence", 0.7)
	viper.SetDefault("ai.daily_budget", 10.0)

	// Features config defaults
	viper.SetDefault("features.enable_ai", true)
	viper.SetDefault("features.enable_ai_scalping", true)
	viper.SetDefault("features.enable_ai_signals", true)
	viper.SetDefault("features.enable_ai_arbitrage", false)
}

// GetServiceURL returns the CCXT service URL.
//
// Returns:
//
//	string: The service URL.
func (c *CCXTConfig) GetServiceURL() string {
	return c.ServiceURL
}

// GetTimeout returns the CCXT service timeout in seconds.
//
// Returns:
//
//	int: The timeout duration.
func (c *CCXTConfig) GetTimeout() int {
	return c.Timeout
}

// validateConfig validates critical security and operational settings.
func validateConfig(config *Config) error {
	driver := strings.ToLower(strings.TrimSpace(config.Database.Driver))
	if driver == "" {
		driver = "postgres"
	}

	if driver != "postgres" && driver != "sqlite" {
		return fmt.Errorf("database.driver must be one of [postgres, sqlite], got %q", config.Database.Driver)
	}

	if driver == "sqlite" && strings.TrimSpace(config.Database.SQLitePath) == "" {
		return fmt.Errorf("database.sqlite_path is required when database.driver=sqlite")
	}

	// Validate JWT secret for production environments
	if config.Environment == "production" || config.Environment == "staging" {
		if config.Auth.JWTSecret == "" {
			return fmt.Errorf("JWT_SECRET cannot be empty in %s environment. Please set a secure JWT secret", config.Environment)
		}

		// Validate JWT secret complexity (minimum 32 characters for security)
		if len(config.Auth.JWTSecret) < 32 {
			return fmt.Errorf("JWT_SECRET must be at least 32 characters long in %s environment for security", config.Environment)
		}

		// Check for common insecure JWT secrets
		insecureSecrets := []string{
			"test-jwt-secret",
			"secret",
			"jwt-secret",
			"default-secret",
			"test-jwt-secret-for-ci",
			"test-jwt-secret-for-ci-only",
			"changeme",
			"password",
			"123456",
		}

		for _, insecure := range insecureSecrets {
			if config.Auth.JWTSecret == insecure {
				return fmt.Errorf("JWT_SECRET '%s' is insecure and not allowed in %s environment. Please use a secure, randomly generated secret", insecure, config.Environment)
			}
		}

		// Validate Service URLs for production/staging
		// If running in a containerized environment (common for production/staging),
		// localhost/127.0.0.1 is almost always wrong for service-to-service communication.
		if config.CCXT.ServiceURL != "" {
			if strings.Contains(config.CCXT.ServiceURL, "localhost") || strings.Contains(config.CCXT.ServiceURL, "127.0.0.1") {
				log.Printf("WARNING: CCXT_SERVICE_URL '%s' contains localhost/127.0.0.1 in %s environment. This may cause connectivity issues between containers.", config.CCXT.ServiceURL, config.Environment)
			}
		}

		if config.Telegram.ServiceURL != "" {
			if strings.Contains(config.Telegram.ServiceURL, "localhost") || strings.Contains(config.Telegram.ServiceURL, "127.0.0.1") {
				log.Printf("WARNING: TELEGRAM_SERVICE_URL '%s' contains localhost/127.0.0.1 in %s environment. This may cause connectivity issues between containers.", config.Telegram.ServiceURL, config.Environment)
			}
		}
	}

	return nil
}
