package config

import (
	"fmt"
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
	ApplicationName       string `mapstructure:"application_name"`
	ConnectTimeout        int    `mapstructure:"connect_timeout"`
	StatementTimeout      int    `mapstructure:"statement_timeout"`
	QueryTimeout          int    `mapstructure:"query_timeout"` // Alias for StatementTimeout for compatibility
	PoolTimeout           int    `mapstructure:"pool_timeout"`
	PoolHealthCheckPeriod int    `mapstructure:"pool_health_check_period"`
	PoolMaxLifetime       int    `mapstructure:"pool_max_lifetime"`
	PoolIdleTimeout       int    `mapstructure:"pool_idle_timeout"`
	EnableAsync           bool   `mapstructure:"enable_async"`
	AsyncBatchSize        int    `mapstructure:"async_batch_size"`
	AsyncConcurrency      int    `mapstructure:"async_concurrency"`
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
}

// CCXTConfig defines settings for interacting with the CCXT microservice.
type CCXTConfig struct {
	// ServiceURL is the base URL of the CCXT service.
	ServiceURL string `mapstructure:"service_url"`
	// GrpcAddress is the gRPC address of the CCXT service.
	GrpcAddress string `mapstructure:"grpc_address"`
	// Timeout is the request timeout in seconds.
	Timeout int `mapstructure:"timeout"`
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

// Load reads the configuration from the config file and environment variables.
//
// Returns:
//
//	*Config: The loaded configuration structure.
//	error: An error if the configuration could not be parsed.
func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	// Set default values
	setDefaults()

	// Enable environment variable support
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Map legacy environment variables for JWT secret
	_ = viper.BindEnv("auth.jwt_secret", "JWT_SECRET")
	_ = viper.BindEnv("security.jwt_secret", "JWT_SECRET")

	// Bind standard DATABASE_URL
	_ = viper.BindEnv("database.database_url", "DATABASE_URL")

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

	// Set database defaults
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

	// Redis
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)

	// CCXT
	viper.SetDefault("ccxt.service_url", "http://localhost:3001")
	viper.SetDefault("ccxt.grpc_address", "localhost:50051")
	viper.SetDefault("ccxt.timeout", 30)

	// Telegram
	viper.SetDefault("telegram.service_url", "http://localhost:3002")
	viper.SetDefault("telegram.grpc_address", "localhost:50052")
	viper.SetDefault("telegram.admin_api_key", "")
	viper.SetDefault("telegram.bot_token", "")
	viper.SetDefault("telegram.webhook_url", "")
	viper.SetDefault("telegram.use_polling", false)
	viper.SetDefault("telegram.polling_offset", 0)
	viper.SetDefault("telegram.polling_limit", 100)
	viper.SetDefault("telegram.polling_timeout", 60)

	// Telemetry
	viper.SetDefault("telemetry.enabled", true)
	viper.SetDefault("telemetry.service_name", "github.com/irfandi/celebrum-ai-go")
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
	viper.SetDefault("backfill.enabled", true)
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
	}

	return nil
}
