package database

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/jackc/pgx/v5"

	// Package database provides PostgreSQL database connectivity and operations
	// for the NeuraTrade application including connection pooling and health checks
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

// PostgresDB wraps a PostgreSQL connection pool.
type PostgresDB struct {
	Pool *pgxpool.Pool
}

const maxAllowedPoolConns int32 = 10000

// NewPostgresConnection creates a new PostgreSQL connection with the default context.
//
// Parameters:
//
//	cfg: Database configuration.
//
// Returns:
//
//	*PostgresDB: The initialized connection.
//	error: Error if connection fails.
func NewPostgresConnection(cfg *config.DatabaseConfig) (*PostgresDB, error) {
	return NewPostgresConnectionWithContext(context.Background(), cfg)
}

// NewPostgresConnectionWithContext creates a new PostgreSQL connection with a specified context.
//
// Parameters:
//
//	ctx: Context for the connection establishment.
//	cfg: Database configuration.
//
// Returns:
//
//	*PostgresDB: The initialized connection.
//	error: Error if connection fails.
func NewPostgresConnectionWithContext(ctx context.Context, cfg *config.DatabaseConfig) (*PostgresDB, error) {
	poolConfig, err := buildPGXPoolConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Optimize for test environment
	if isTestEnvironment() {
		return createTestDatabaseConnection(poolConfig)
	}

	// Create connection pool with optimized retry logic and timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var pool *pgxpool.Pool
	for attempts := 0; attempts < 3; attempts++ {
		pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
		if err == nil {
			break
		}
		logrus.Warnf("Database connection attempt %d failed: %v", attempts+1, err)
		if attempts < 2 {
			// Exponential backoff with jitter
			backoffDuration := time.Duration(1<<uint(attempts)) * time.Second
			time.Sleep(backoffDuration)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool after retries: %w", err)
	}

	// Test the connection with timeout
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logrus.Info("Successfully connected to PostgreSQL")

	return &PostgresDB{Pool: pool}, nil
}

// isTestEnvironment detects if we're running in a test environment
func isTestEnvironment() bool {
	// Check environment variables that indicate test environment
	return os.Getenv("CI_ENVIRONMENT") == "test" ||
		os.Getenv("RUN_TESTS") == "true" ||
		strings.HasSuffix(os.Args[0], ".test") ||
		strings.Contains(os.Args[0], "test")
}

// createTestDatabaseConnection creates a connection optimized for testing
func createTestDatabaseConnection(poolConfig *pgxpool.Config) (*PostgresDB, error) {
	// Optimize pool settings for tests
	poolConfig.MaxConns = 5
	poolConfig.MinConns = 1
	poolConfig.MaxConnLifetime = 5 * time.Minute
	poolConfig.MaxConnIdleTime = 30 * time.Second
	poolConfig.HealthCheckPeriod = 30 * time.Second

	// Shorter timeout for tests
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create test database connection: %w", err)
	}

	// Quick ping with timeout
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping test database: %w", err)
	}

	return &PostgresDB{Pool: pool}, nil
}

// Close closes the database connection pool.
func (db *PostgresDB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
		logrus.Info("PostgreSQL connection closed")
	}
}

// HealthCheck verifies the database connection.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	error: Error if ping fails.
func (db *PostgresDB) HealthCheck(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}

// Query executes a query that returns rows.
//
// Parameters:
//
//	ctx: Context.
//	sql: SQL query.
//	args: Query arguments.
//
// Returns:
//
//	pgx.Rows: Result rows.
//	error: Error if query fails.
func (db *PostgresDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return db.Pool.Query(ctx, sql, args...)
}

// QueryRow executes a query that returns a single row.
//
// Parameters:
//
//	ctx: Context.
//	sql: SQL query.
//	args: Query arguments.
//
// Returns:
//
//	pgx.Row: Result row.
func (db *PostgresDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return db.Pool.QueryRow(ctx, sql, args...)
}

// Exec executes a query without returning rows.
//
// Parameters:
//
//	ctx: Context.
//	sql: SQL query.
//	args: Query arguments.
//
// Returns:
//
//	pgconn.CommandTag: Command tag.
//	error: Error if execution fails.
func (db *PostgresDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return db.Pool.Exec(ctx, sql, args...)
}

func buildPGXPoolConfig(cfg *config.DatabaseConfig) (*pgxpool.Config, error) {
	var dsn string

	// Check if Host contains a connection string (common misconfiguration or flexible usage)
	if strings.HasPrefix(cfg.Host, "postgres://") || strings.HasPrefix(cfg.Host, "postgresql://") {
		dsn = cfg.Host
	} else if cfg.DatabaseURL != "" {
		dsn = cfg.DatabaseURL
	} else {
		// Build DSN with PostgreSQL 18 optimizations
		dsn = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s application_name=%s connect_timeout=%d",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
			cfg.ApplicationName, cfg.ConnectTimeout)
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Basic connection pool configuration
	if cfg.MaxOpenConns > 0 {
		poolConfig.MaxConns = clampToSafePoolSize(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		poolConfig.MinConns = clampToSafePoolSize(cfg.MaxIdleConns)
	}

	if poolConfig.MinConns > 0 && poolConfig.MaxConns > 0 && poolConfig.MinConns > poolConfig.MaxConns {
		return nil, fmt.Errorf("invalid pool sizing: min_conns (%d) > max_conns (%d)", poolConfig.MinConns, poolConfig.MaxConns)
	}

	// Parse duration-based configurations
	if cfg.ConnMaxLifetime != "" {
		duration, err := time.ParseDuration(cfg.ConnMaxLifetime)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ConnMaxLifetime: %w", err)
		}
		poolConfig.MaxConnLifetime = duration
	}

	if cfg.ConnMaxIdleTime != "" {
		duration, err := time.ParseDuration(cfg.ConnMaxIdleTime)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ConnMaxIdleTime: %w", err)
		}
		poolConfig.MaxConnIdleTime = duration
	}

	// PostgreSQL 18 specific optimizations
	if cfg.PoolHealthCheckPeriod > 0 {
		poolConfig.HealthCheckPeriod = time.Duration(cfg.PoolHealthCheckPeriod) * time.Second
	}

	if cfg.PoolMaxLifetime > 0 {
		poolConfig.MaxConnLifetime = time.Duration(cfg.PoolMaxLifetime) * time.Second
	}

	if cfg.PoolIdleTimeout > 0 {
		poolConfig.MaxConnIdleTime = time.Duration(cfg.PoolIdleTimeout) * time.Second
	}

	// Configure pgx config for PostgreSQL 18 async features
	poolConfig.ConnConfig.RuntimeParams["application_name"] = cfg.ApplicationName

	// Set statement_timeout (PostgreSQL standard parameter)
	// Note: Use the larger of StatementTimeout or QueryTimeout since they serve similar purposes
	timeout := cfg.StatementTimeout
	if cfg.QueryTimeout > 0 && cfg.QueryTimeout > timeout {
		timeout = cfg.QueryTimeout
	}
	if timeout > 0 {
		poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", timeout)
	}

	// Enable PostgreSQL 18 async features if requested
	if cfg.EnableAsync {
		poolConfig.ConnConfig.RuntimeParams["jit"] = "off" // Disable JIT for better async performance
		poolConfig.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_generic_plan"
	}

	// Add Sentry tracer for error tracking
	poolConfig.ConnConfig.Tracer = &PostgresSentryTracer{}

	return poolConfig, nil
}

func clampToSafePoolSize(value int) int32 {
	requested := int64(value)
	if requested <= 0 {
		return 0
	}

	if requested > int64(math.MaxInt32) {
		logrus.Warnf("Configured pool size %d exceeds int32 limit; clamping to %d", value, maxAllowedPoolConns)
		return maxAllowedPoolConns
	}

	if requested > int64(maxAllowedPoolConns) {
		logrus.Warnf("Configured pool size %d exceeds safe limit %d; clamping", value, maxAllowedPoolConns)
		return maxAllowedPoolConns
	}

	return int32(requested)
}
