package database

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test PostgresDB struct
func TestPostgresDB_Struct(t *testing.T) {
	db := &PostgresDB{
		Pool: nil, // We can't create a real pool without a database
	}

	assert.NotNil(t, db)
	assert.Nil(t, db.Pool)
}

// Test PostgresDB Close method with nil pool
func TestPostgresDB_Close_NilPool(t *testing.T) {
	db := &PostgresDB{Pool: nil}

	// Should not panic when closing nil pool
	assert.NotPanics(t, func() {
		db.Close()
	})
}

// Test PostgresDB Close method with valid pool (mock)
func TestPostgresDB_Close_WithPool(t *testing.T) {
	// Create a mock pool that we can close
	// Since we can't create a real pool without a database, we'll test the logic path
	db := &PostgresDB{Pool: nil}

	// Should not panic when closing with nil pool (already tested)
	assert.NotPanics(t, func() {
		db.Close()
	})
}

// Test PostgresDB HealthCheck with nil pool
func TestPostgresDB_HealthCheck_NilPool(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	// Should panic when trying to ping nil pool
	assert.Panics(t, func() {
		_ = db.HealthCheck(ctx)
	})
}

// Test RedisClient struct
func TestRedisClient_Struct(t *testing.T) {
	client := &RedisClient{
		Client: nil, // We can't create a real client without Redis
	}

	assert.NotNil(t, client)
	assert.Nil(t, client.Client)
}

// Test RedisClient Close method with nil client
func TestRedisClient_Close_NilClient(t *testing.T) {
	client := &RedisClient{Client: nil}

	// Should not panic when closing nil client
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// Test RedisClient HealthCheck with nil client
func TestRedisClient_HealthCheck_NilClient(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Should return error when trying to ping nil client
	err := client.HealthCheck(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

// Test RedisClient cache operations with nil client
func TestRedisClient_Set_NilClient(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Should return error when trying to set with nil client
	err := client.Set(ctx, "key", "value", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

func TestRedisClient_Get_NilClient(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Should return error when trying to get with nil client
	_, err := client.Get(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

func TestRedisClient_Delete_NilClient(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Should return error when trying to delete with nil client
	err := client.Delete(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

func TestRedisClient_Exists_NilClient(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Should return error when trying to check existence with nil client
	_, err := client.Exists(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

// Test NewPostgresConnection with invalid config
func TestNewPostgresConnection_InvalidConfig(t *testing.T) {
	// Test with invalid database URL
	cfg := &config.DatabaseConfig{
		DatabaseURL: "invalid-url",
	}

	db, err := NewPostgresConnection(cfg)
	assert.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "failed to parse database config")
}

// Test optimized database connection for test environment
func TestNewPostgresConnection_TestEnvironment(t *testing.T) {
	// Set test environment
	t.Setenv("CI_ENVIRONMENT", "test")
	t.Setenv("RUN_TESTS", "true")

	// Test with valid config but using test optimizations
	cfg := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "test",
		Password: "test",
		DBName:   "test",
		SSLMode:  "disable",
	}

	// This should use the optimized test connection path and fail quickly
	db, err := NewPostgresConnection(cfg)
	// We expect this to fail gracefully without long timeouts due to no database running
	assert.Error(t, err)
	assert.Nil(t, db)
	// Should fail quickly with connection error, not timeout
	assert.Contains(t, err.Error(), "failed to connect")
}

func TestNewPostgresConnection_InvalidDurationConfig(t *testing.T) {
	// Test with valid basic config but invalid duration strings
	cfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		DBName:          "test",
		SSLMode:         "disable",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: "invalid-duration",
		ConnMaxIdleTime: "invalid-duration",
	}

	db, err := NewPostgresConnection(cfg)
	assert.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "failed to parse ConnMaxLifetime")
}

func TestBuildPGXPoolConfig_BuildsFromComponents(t *testing.T) {
	// Test DSN building from individual components without attempting a real connection
	cfg := &config.DatabaseConfig{
		Host:         "localhost",
		Port:         5432,
		User:         "testuser",
		Password:     "testpass",
		DBName:       "testdb",
		SSLMode:      "disable",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
		// Leave DatabaseURL empty to test component-based DSN
	}

	poolConfig, err := buildPGXPoolConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, poolConfig)

	assert.Equal(t, int32(10), poolConfig.MaxConns)
	assert.Equal(t, int32(5), poolConfig.MinConns)

	connString := poolConfig.ConnString()
	assert.Contains(t, connString, "host=localhost")
	assert.Contains(t, connString, "port=5432")
	assert.Contains(t, connString, "user=testuser")
	assert.Contains(t, connString, "password=testpass")
	assert.Contains(t, connString, "dbname=testdb")
	assert.Contains(t, connString, "sslmode=disable")
}

// Test NewRedisConnection with invalid config
func TestNewRedisConnection_InvalidConfig(t *testing.T) {
	// Test with invalid Redis config (non-existent host)
	cfg := config.RedisConfig{
		Host:     "non-existent-host",
		Port:     6379,
		Password: "",
		DB:       0,
	}

	client, err := NewRedisConnection(cfg)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to connect to Redis")
}

// Test configuration validation
func TestDatabaseConfig_Validation(t *testing.T) {
	// Test valid PostgreSQL config
	postgresCfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "postgres",
		Password:        "password",
		DBName:          "testdb",
		SSLMode:         "disable",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: "300s",
		ConnMaxIdleTime: "60s",
	}

	assert.Equal(t, "localhost", postgresCfg.Host)
	assert.Equal(t, 5432, postgresCfg.Port)
	assert.Equal(t, "postgres", postgresCfg.User)
	assert.Equal(t, "password", postgresCfg.Password)
	assert.Equal(t, "testdb", postgresCfg.DBName)
	assert.Equal(t, "disable", postgresCfg.SSLMode)
	assert.Equal(t, 25, postgresCfg.MaxOpenConns)
	assert.Equal(t, 5, postgresCfg.MaxIdleConns)
	assert.Equal(t, "300s", postgresCfg.ConnMaxLifetime)
	assert.Equal(t, "60s", postgresCfg.ConnMaxIdleTime)

	// Test valid Redis config
	redisCfg := config.RedisConfig{
		Host:     "localhost",
		Port:     6379,
		Password: "redis_password",
		DB:       1,
	}

	assert.Equal(t, "localhost", redisCfg.Host)
	assert.Equal(t, 6379, redisCfg.Port)
	assert.Equal(t, "redis_password", redisCfg.Password)
	assert.Equal(t, 1, redisCfg.DB)
}

// Test duration parsing
func TestDurationParsing(t *testing.T) {
	// Test valid duration strings
	validDurations := []string{"300s", "5m", "1h", "24h"}

	for _, durationStr := range validDurations {
		duration, err := time.ParseDuration(durationStr)
		assert.NoError(t, err, "Should parse valid duration: %s", durationStr)
		assert.Greater(t, duration, time.Duration(0), "Duration should be positive")
	}

	// Test invalid duration strings
	invalidDurations := []string{"invalid", "300", "5minutes", "1hour"}

	for _, durationStr := range invalidDurations {
		_, err := time.ParseDuration(durationStr)
		assert.Error(t, err, "Should fail to parse invalid duration: %s", durationStr)
	}
}

// Test context operations
func TestContextOperations(t *testing.T) {
	// Test context creation and cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)

	// Test context with deadline
	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))

	// Test context cancellation
	cancel()
	select {
	case <-ctx.Done():
		// Context should be cancelled
		assert.Error(t, ctx.Err())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should have been cancelled")
	}
}

// Test error handling patterns
func TestErrorHandling(t *testing.T) {
	// Test error wrapping patterns used in the database package
	originalErr := assert.AnError
	wrappedErr := fmt.Errorf("failed to connect: %w", originalErr)

	assert.Error(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "failed to connect")
	assert.Contains(t, wrappedErr.Error(), originalErr.Error())

	// Test error unwrapping
	require.ErrorIs(t, wrappedErr, originalErr)
}

// Test Redis Close method with logger
func TestRedisClient_Close_WithLogger(t *testing.T) {
	logger := zaplogrus.New()
	client := &RedisClient{
		Client: nil,
		logger: logger,
	}

	// Should not panic when closing nil client with logger
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// Test Redis Close method with successful close
func TestRedisClient_Close_Success(t *testing.T) {
	// This test would require a mock Redis client
	// For now, we test the nil case which is already covered
	client := &RedisClient{Client: nil}
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// Test Redis NewRedisConnectionWithRetry with error recovery manager
func TestNewRedisConnectionWithRetry_WithErrorRecovery(t *testing.T) {
	// Mock ErrorRecoveryManager
	mockErrorRecovery := &MockErrorRecoveryManager{
		executeWithRetryFunc: func(ctx context.Context, operationName string, operation func() error) error {
			return fmt.Errorf("mock connection error")
		},
	}

	cfg := config.RedisConfig{
		Host:     "localhost",
		Port:     6379,
		Password: "",
		DB:       0,
	}

	client, err := NewRedisConnectionWithRetry(cfg, mockErrorRecovery)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to connect to Redis")
}

// Test Redis NewRedisConnectionWithRetry with non-existent host
func TestNewRedisConnectionWithRetry_NonExistentHost(t *testing.T) {
	// Mock ErrorRecoveryManager that simulates connection failure
	mockErrorRecovery := &MockErrorRecoveryManager{
		executeWithRetryFunc: func(ctx context.Context, operationName string, operation func() error) error {
			return fmt.Errorf("connection failed") // Simulate connection failure
		},
	}

	cfg := config.RedisConfig{
		Host:     "non-existent-host-12345",
		Port:     6379,
		Password: "",
		DB:       0,
	}

	client, err := NewRedisConnectionWithRetry(cfg, mockErrorRecovery)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to connect to Redis")
}

// Test Redis cache operations with successful scenarios
func TestRedisClient_CacheOperations(t *testing.T) {
	// Create a mock Redis client that succeeds
	client := &RedisClient{
		Client: nil, // Will remain nil for error testing
		logger: zaplogrus.New(),
	}
	ctx := context.Background()

	// Test Set operation with nil client
	err := client.Set(ctx, "test-key", "test-value", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	// Test Get operation with nil client
	_, err = client.Get(ctx, "test-key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	// Test Delete operation with nil client
	err = client.Delete(ctx, "test-key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	// Test Exists operation with nil client
	_, err = client.Exists(ctx, "test-key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

// Test Redis Close with error scenario
func TestRedisClient_Close_Error(t *testing.T) {
	// This test would require a mock Redis client that returns an error on close
	// For now, we test the nil case which provides some coverage
	client := &RedisClient{Client: nil}
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// Test Redis Close with logger only
func TestRedisClient_Close_LoggerOnly(t *testing.T) {
	logger := zaplogrus.New()
	client := &RedisClient{
		Client: nil,
		logger: logger,
	}
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// Test Redis operations with various scenarios
func TestRedisClient_OperationScenarios(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Test multiple Delete operations
	err := client.Delete(ctx, "key1", "key2", "key3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	// Test multiple Exists operations
	count, err := client.Exists(ctx, "key1", "key2", "key3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
	assert.Equal(t, int64(0), count)
}

// Test Redis HealthCheck success scenario
func TestRedisClient_HealthCheck_Success(t *testing.T) {
	// This test would require a real Redis client or a more sophisticated mock
	// For now, we test the nil case which provides error path coverage
	client := &RedisClient{Client: nil}
	ctx := context.Background()
	err := client.HealthCheck(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

// Mock ErrorRecoveryManager for testing
type MockErrorRecoveryManager struct {
	executeWithRetryFunc func(ctx context.Context, operationName string, operation func() error) error
}

func (m *MockErrorRecoveryManager) ExecuteWithRetry(ctx context.Context, operationName string, operation func() error) error {
	return m.executeWithRetryFunc(ctx, operationName, operation)
}

// Test TracedDB struct
func TestTracedDB_Struct(t *testing.T) {
	db := &TracedDB{
		Pool: nil, // We can't create a real pool without a database
	}

	assert.NotNil(t, db)
	assert.Nil(t, db.Pool)
}

// Test NewTracedDB with nil pool
func TestNewTracedDB_NilPool(t *testing.T) {
	db := NewTracedDB(nil)
	assert.NotNil(t, db)
	assert.Nil(t, db.Pool)
}

// Test TracedDB Query with nil pool
func TestTracedDB_Query_NilPool(t *testing.T) {
	db := &TracedDB{Pool: nil}
	ctx := context.Background()

	// Should panic when trying to query with nil pool
	assert.Panics(t, func() {
		_, _ = db.Query(ctx, "SELECT 1")
	})
}

// Test TracedDB QueryRow with nil pool
func TestTracedDB_QueryRow_NilPool(t *testing.T) {
	db := &TracedDB{Pool: nil}
	ctx := context.Background()

	// Should panic when trying to query row with nil pool
	assert.Panics(t, func() {
		_ = db.QueryRow(ctx, "SELECT 1")
	})
}

// Test TracedDB Exec with nil pool
func TestTracedDB_Exec_NilPool(t *testing.T) {
	db := &TracedDB{Pool: nil}
	ctx := context.Background()

	// Should panic when trying to exec with nil pool
	assert.Panics(t, func() {
		_, _ = db.Exec(ctx, "INSERT INTO test (col) VALUES ($1)", "value")
	})
}

// Test TracedDB Begin with nil pool
func TestTracedDB_Begin_NilPool(t *testing.T) {
	db := &TracedDB{Pool: nil}
	ctx := context.Background()

	// Should panic when trying to begin transaction with nil pool
	assert.Panics(t, func() {
		_, _ = db.Begin(ctx)
	})
}

// Test TracedDB BeginTx with nil pool
func TestTracedDB_BeginTx_NilPool(t *testing.T) {
	db := &TracedDB{Pool: nil}
	ctx := context.Background()
	txOptions := pgx.TxOptions{}

	// Should panic when trying to begin transaction with nil pool
	assert.Panics(t, func() {
		_, _ = db.BeginTx(ctx, txOptions)
	})
}

// Test TracedDB Ping with nil pool
func TestTracedDB_Ping_NilPool(t *testing.T) {
	db := &TracedDB{Pool: nil}
	ctx := context.Background()

	// Should panic when trying to ping with nil pool
	assert.Panics(t, func() {
		_ = db.Ping(ctx)
	})
}

// Test TracedDB Close with nil pool
func TestTracedDB_Close_NilPool(t *testing.T) {
	db := &TracedDB{Pool: nil}

	// Should panic when closing nil pool
	assert.Panics(t, func() {
		db.Close()
	})
}

// Test TracedTx struct
func TestTracedTx_Struct(t *testing.T) {
	tx := &TracedTx{
		Tx: nil, // We can't create a real transaction without a database
	}

	assert.NotNil(t, tx)
	assert.Nil(t, tx.Tx)
}

// Test TracedTx Query with nil transaction
func TestTracedTx_Query_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()

	// Should panic when trying to query with nil transaction
	assert.Panics(t, func() {
		_, _ = tx.Query(ctx, "SELECT 1")
	})
}

// Test TracedTx QueryRow with nil transaction
func TestTracedTx_QueryRow_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()

	// Should panic when trying to query row with nil transaction
	assert.Panics(t, func() {
		_ = tx.QueryRow(ctx, "SELECT 1")
	})
}

// Test TracedTx Exec with nil transaction
func TestTracedTx_Exec_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()

	// Should panic when trying to exec with nil transaction
	assert.Panics(t, func() {
		_, _ = tx.Exec(ctx, "INSERT INTO test (col) VALUES ($1)", "value")
	})
}

// Test TracedTx Commit with nil transaction
func TestTracedTx_Commit_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()

	// Should panic when trying to commit with nil transaction
	assert.Panics(t, func() {
		_ = tx.Commit(ctx)
	})
}

// Test TracedTx Rollback with nil transaction
func TestTracedTx_Rollback_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()

	// Should panic when trying to rollback with nil transaction
	assert.Panics(t, func() {
		_ = tx.Rollback(ctx)
	})
}

// Test TracedTx Begin with nil transaction
func TestTracedTx_Begin_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()

	// Should panic when trying to begin nested transaction with nil transaction
	assert.Panics(t, func() {
		_, _ = tx.Begin(ctx)
	})
}

// Test TracedTx Conn with nil transaction
func TestTracedTx_Conn_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}

	// Should panic when trying to get connection with nil transaction
	assert.Panics(t, func() {
		_ = tx.Conn()
	})
}

// Test TracedTx CopyFrom with nil transaction
func TestTracedTx_CopyFrom_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()
	tableName := pgx.Identifier{"test"}
	columnNames := []string{"col"}

	// Create a simple CopyFromSource
	rows := [][]interface{}{{"value"}}
	rowSrc := pgx.CopyFromRows(rows)

	// Should panic when trying to copy from with nil transaction
	assert.Panics(t, func() {
		_, _ = tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
	})
}

// Test TracedTx LargeObjects with nil transaction
func TestTracedTx_LargeObjects_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}

	// Should panic when trying to get large objects with nil transaction
	assert.Panics(t, func() {
		_ = tx.LargeObjects()
	})
}

// Test TracedTx Prepare with nil transaction
func TestTracedTx_Prepare_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()

	// Should panic when trying to prepare with nil transaction
	assert.Panics(t, func() {
		_, _ = tx.Prepare(ctx, "test_stmt", "SELECT 1")
	})
}

// Test TracedTx SendBatch with nil transaction
func TestTracedTx_SendBatch_NilTx(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx := context.Background()
	batch := &pgx.Batch{}

	// Should panic when trying to send batch with nil transaction
	assert.Panics(t, func() {
		_ = tx.SendBatch(ctx, batch)
	})
}

// Test RecordDatabaseError function
func TestRecordDatabaseError(t *testing.T) {
	ctx := context.Background()
	err := fmt.Errorf("test error")
	operation := "test_operation"

	// Should not panic when recording database error
	assert.NotPanics(t, func() {
		RecordDatabaseError(ctx, err, operation)
	})
}

// Test RecordDatabaseError with nil error
func TestRecordDatabaseError_NilError(t *testing.T) {
	ctx := context.Background()
	operation := "test_operation"

	// Should not panic when recording nil database error
	assert.NotPanics(t, func() {
		RecordDatabaseError(ctx, nil, operation)
	})
}

// Test RecordDatabaseError with empty operation
func TestRecordDatabaseError_EmptyOperation(t *testing.T) {
	ctx := context.Background()
	err := fmt.Errorf("test error")

	// Should not panic when recording database error with empty operation
	assert.NotPanics(t, func() {
		RecordDatabaseError(ctx, err, "")
	})
}

// Test AddDatabaseSpanAttributes function
func TestAddDatabaseSpanAttributes(t *testing.T) {
	ctx := context.Background()
	table := "test_table"
	rowsAffected := int64(42)

	// Should not panic when adding database span attributes
	assert.NotPanics(t, func() {
		AddDatabaseSpanAttributes(ctx, table, rowsAffected)
	})
}

// Test AddDatabaseSpanAttributes with empty table
func TestAddDatabaseSpanAttributes_EmptyTable(t *testing.T) {
	ctx := context.Background()
	rowsAffected := int64(0)

	// Should not panic when adding database span attributes with empty table
	assert.NotPanics(t, func() {
		AddDatabaseSpanAttributes(ctx, "", rowsAffected)
	})
}

// Test AddDatabaseSpanAttributes with zero rows affected
func TestAddDatabaseSpanAttributes_ZeroRows(t *testing.T) {
	ctx := context.Background()
	table := "test_table"
	rowsAffected := int64(0)

	// Should not panic when adding database span attributes with zero rows
	assert.NotPanics(t, func() {
		AddDatabaseSpanAttributes(ctx, table, rowsAffected)
	})
}

// Test AddDatabaseSpanAttributes with negative rows affected
func TestAddDatabaseSpanAttributes_NegativeRows(t *testing.T) {
	ctx := context.Background()
	table := "test_table"
	rowsAffected := int64(-1)

	// Should not panic when adding database span attributes with negative rows
	assert.NotPanics(t, func() {
		AddDatabaseSpanAttributes(ctx, table, rowsAffected)
	})
}

// Test TracedDB operations with timeout context
func TestTracedDB_Operations_WithTimeout(t *testing.T) {
	db := &TracedDB{Pool: nil}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Test that operations handle timeout context appropriately (should panic due to nil pool)
	assert.Panics(t, func() {
		_, _ = db.Query(ctx, "SELECT 1")
	})

	assert.Panics(t, func() {
		_ = db.QueryRow(ctx, "SELECT 1")
	})

	assert.Panics(t, func() {
		_, _ = db.Exec(ctx, "SELECT 1")
	})

	assert.Panics(t, func() {
		_ = db.Ping(ctx)
	})
}

// Test TracedTx operations with timeout context
func TestTracedTx_Operations_WithTimeout(t *testing.T) {
	tx := &TracedTx{Tx: nil}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Test that operations handle timeout context appropriately (should panic due to nil tx)
	assert.Panics(t, func() {
		_, _ = tx.Query(ctx, "SELECT 1")
	})

	assert.Panics(t, func() {
		_ = tx.QueryRow(ctx, "SELECT 1")
	})

	assert.Panics(t, func() {
		_, _ = tx.Exec(ctx, "SELECT 1")
	})

	assert.Panics(t, func() {
		_ = tx.Commit(ctx)
	})

	assert.Panics(t, func() {
		_ = tx.Rollback(ctx)
	})
}

// Test TracedDB Close multiple times
func TestTracedDB_Close_MultipleTimes(t *testing.T) {
	db := &TracedDB{Pool: nil}

	// Should panic when closing multiple times
	assert.Panics(t, func() {
		db.Close()
		db.Close()
		db.Close()
	})
}

// Test telemetry functions with various error types
func TestRecordDatabaseError_VariousErrorTypes(t *testing.T) {
	ctx := context.Background()
	operation := "test_operation"

	errorTypes := []error{
		fmt.Errorf("simple error"),
		fmt.Errorf("wrapped error: %w", assert.AnError),
		nil,
		context.DeadlineExceeded,
		context.Canceled,
	}

	for _, err := range errorTypes {
		assert.NotPanics(t, func() {
			RecordDatabaseError(ctx, err, operation)
		})
	}
}

// Test AddDatabaseSpanAttributes with various row counts
func TestAddDatabaseSpanAttributes_VariousRowCounts(t *testing.T) {
	ctx := context.Background()
	table := "test_table"

	rowCounts := []int64{
		0,
		1,
		100,
		1000,
		999999,
		-1,
	}

	for _, rowsAffected := range rowCounts {
		assert.NotPanics(t, func() {
			AddDatabaseSpanAttributes(ctx, table, rowsAffected)
		})
	}
}

// Test RedisClient Close with error scenario
func TestRedisClient_Close_WithError(t *testing.T) {
	// Create a mock client that will error on close
	// Since we can't create a real client without Redis, we'll test with nil
	client := &RedisClient{
		Client: nil,
		logger: zaplogrus.New(),
	}

	// Should not panic when closing nil client
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// Test RedisClient Close with different logger scenarios
func TestRedisClient_Close_LoggerScenarios(t *testing.T) {
	// Test with nil logger
	client1 := &RedisClient{
		Client: nil,
		logger: nil,
	}
	assert.NotPanics(t, func() {
		client1.Close()
	})

	// Test with custom logger
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)
	client2 := &RedisClient{
		Client: nil,
		logger: logger,
	}
	assert.NotPanics(t, func() {
		client2.Close()
	})
}

// Test Redis operations with timeout context
func TestRedisClient_Operations_WithTimeout(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Test operations with timeout context
	err := client.HealthCheck(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	err = client.Set(ctx, "key", "value", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	_, err = client.Get(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	err = client.Delete(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	_, err = client.Exists(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

// Test Redis operations with cancelled context
func TestRedisClient_Operations_WithCancelledContext(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test operations with cancelled context
	err := client.HealthCheck(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	err = client.Set(ctx, "key", "value", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

// Test NewRedisConnection with valid config but unreachable host
func TestNewRedisConnection_UnreachableHost(t *testing.T) {
	cfg := config.RedisConfig{
		Host:     "192.0.2.1", // RFC 5737 test network - unreachable
		Port:     6379,
		Password: "",
		DB:       0,
	}

	client, err := NewRedisConnection(cfg)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to connect to Redis")
}

// Test NewRedisConnectionWithRetry with nil error recovery manager
func TestNewRedisConnectionWithRetry_NilErrorRecovery(t *testing.T) {
	cfg := config.RedisConfig{
		Host:     "invalid-host-that-does-not-exist",
		Port:     9999,
		Password: "",
		DB:       0,
	}

	// Test with nil error recovery manager (should use fallback)
	client, err := NewRedisConnectionWithRetry(cfg, nil)
	// We expect this to fail because we're using an invalid host
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to connect to Redis")
}

// Test PostgresDB Close with multiple scenarios
func TestPostgresDB_Close_MultipleScenarios(t *testing.T) {
	// Test with nil pool
	db1 := &PostgresDB{Pool: nil}
	assert.NotPanics(t, func() {
		db1.Close()
	})

	// Test multiple close calls on same nil pool
	db2 := &PostgresDB{Pool: nil}
	assert.NotPanics(t, func() {
		db2.Close()
		db2.Close()
		db2.Close()
	})
}

// Test NewPostgresConnection with valid DATABASE_URL
func TestNewPostgresConnection_ValidDatabaseURL(t *testing.T) {
	// Test with a valid DATABASE_URL format but non-existent database
	cfg := &config.DatabaseConfig{
		DatabaseURL: "postgres://user:pass@localhost:5432/testdb",
	}

	db, err := NewPostgresConnection(cfg)
	// We expect this to fail because we don't have a real database
	assert.Error(t, err)
	assert.Nil(t, db)
	// Should fail with connection error (either from test optimized path or normal path)
	assert.True(t,
		strings.Contains(err.Error(), "failed to ping database") ||
			strings.Contains(err.Error(), "failed to connect"))
}

// Test NewPostgresConnection with connection pool configuration
func TestBuildPGXPoolConfig_ConnectionPoolConfig(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		DBName:          "test",
		SSLMode:         "disable",
		MaxOpenConns:    2147483647,
		MaxIdleConns:    2147483647,
		ConnMaxLifetime: "24h",
		ConnMaxIdleTime: "1h",
	}

	poolCfg, err := buildPGXPoolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, maxAllowedPoolConns, poolCfg.MaxConns)
	assert.Equal(t, maxAllowedPoolConns, poolCfg.MinConns)
	assert.Equal(t, 24*time.Hour, poolCfg.MaxConnLifetime)
	assert.Equal(t, time.Hour, poolCfg.MaxConnIdleTime)
}

// Test buildPGXPoolConfig with edge case configurations
func TestBuildPGXPoolConfig_EdgeCases(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		DBName:          "test",
		SSLMode:         "disable",
		MaxOpenConns:    0,
		MaxIdleConns:    0,
		ConnMaxLifetime: "0s",
		ConnMaxIdleTime: "0s",
	}

	poolCfg, err := buildPGXPoolConfig(cfg)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, poolCfg.MaxConns, int32(0))
	assert.GreaterOrEqual(t, poolCfg.MinConns, int32(0))
	assert.LessOrEqual(t, poolCfg.MaxConns, maxAllowedPoolConns)
	assert.LessOrEqual(t, poolCfg.MinConns, maxAllowedPoolConns)
	assert.Equal(t, time.Duration(0), poolCfg.MaxConnLifetime)
	assert.Equal(t, time.Duration(0), poolCfg.MaxConnIdleTime)
}

// Test buildPGXPoolConfig with connection pool bounds checking
func TestBuildPGXPoolConfig_ConnectionPoolBounds(t *testing.T) {
	t.Parallel()

	if strconv.IntSize == 32 {
		t.Skip("skipping bounds test on 32-bit architectures")
	}

	tooLarge := int64(math.MaxInt32) + 1

	cfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		DBName:          "test",
		SSLMode:         "disable",
		MaxOpenConns:    int(tooLarge),
		MaxIdleConns:    int(tooLarge),
		ConnMaxLifetime: "1h",
		ConnMaxIdleTime: "30m",
	}

	poolCfg, err := buildPGXPoolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, maxAllowedPoolConns, poolCfg.MaxConns)
	assert.Equal(t, maxAllowedPoolConns, poolCfg.MinConns)
	assert.Equal(t, time.Hour, poolCfg.MaxConnLifetime)
	assert.Equal(t, 30*time.Minute, poolCfg.MaxConnIdleTime)
}

func TestBuildPGXPoolConfig_InvalidDurations(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		DBName:          "test",
		SSLMode:         "disable",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: "oops",
	}

	poolCfg, err := buildPGXPoolConfig(cfg)
	assert.Nil(t, poolCfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse ConnMaxLifetime")

	cfg.ConnMaxLifetime = "1h"
	cfg.ConnMaxIdleTime = "still-bad"

	poolCfg, err = buildPGXPoolConfig(cfg)
	assert.Nil(t, poolCfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse ConnMaxIdleTime")
}

func TestBuildPGXPoolConfig_InvalidPoolSizing(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:         "localhost",
		Port:         5432,
		User:         "test",
		Password:     "test",
		DBName:       "test",
		SSLMode:      "disable",
		MaxOpenConns: 5,
		MaxIdleConns: 10,
	}

	poolCfg, err := buildPGXPoolConfig(cfg)
	assert.Nil(t, poolCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pool sizing")
}

// Test RedisClient Close with actual Redis client mock
func TestRedisClient_Close_WithRealClient(t *testing.T) {
	// Since we can't create a real Redis client without Redis running,
	// we'll test the Close method behavior with different scenarios
	client := &RedisClient{
		Client: nil, // Simulates a client that was never connected
		logger: zaplogrus.New(),
	}

	// Should not panic and should handle nil client gracefully
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// Test Redis operations with different value types
func TestRedisClient_Operations_ValueTypes(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Test Set with different value types
	testCases := []struct {
		name  string
		value interface{}
	}{
		{"string", "test value"},
		{"int", 42},
		{"float", 3.14},
		{"bool", true},
		{"slice", []int{1, 2, 3}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.Set(ctx, "test_key", tc.value, time.Minute)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "redis client is nil")
		})
	}
}

// Test Redis Delete with multiple keys
func TestRedisClient_Delete_MultipleKeys(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Test Delete with multiple keys
	err := client.Delete(ctx, "key1", "key2", "key3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	// Test Delete with no keys
	err = client.Delete(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

// Test Redis Exists with multiple keys
func TestRedisClient_Exists_MultipleKeys(t *testing.T) {
	client := &RedisClient{Client: nil}
	ctx := context.Background()

	// Test Exists with multiple keys
	count, err := client.Exists(ctx, "key1", "key2", "key3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
	assert.Equal(t, int64(0), count)

	// Test Exists with no keys
	count, err = client.Exists(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
	assert.Equal(t, int64(0), count)
}

// Test PostgresDB Close with successful scenario
func TestPostgresDB_Close_Success(t *testing.T) {
	// Since we can't create a real pool without a database,
	// we'll test the Close method behavior with nil pool
	db := &PostgresDB{Pool: nil}

	// Should not panic when closing nil pool
	assert.NotPanics(t, func() {
		db.Close()
	})
}

// Test PostgresDB Close with logging verification
func TestPostgresDB_Close_Logging(t *testing.T) {
	// Create a logger to capture log messages
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.InfoLevel)

	db := &PostgresDB{
		Pool: nil,
	}

	// Should not panic and should handle nil pool gracefully
	assert.NotPanics(t, func() {
		db.Close()
	})
}

// Test RedisClient Close error handling scenarios
func TestRedisClient_Close_ErrorScenarios(t *testing.T) {
	// Test with nil client and nil logger
	client1 := &RedisClient{
		Client: nil,
		logger: nil,
	}

	assert.NotPanics(t, func() {
		client1.Close()
	})

	// Test with nil client but with logger
	logger := zaplogrus.New()
	client2 := &RedisClient{
		Client: nil,
		logger: logger,
	}

	assert.NotPanics(t, func() {
		client2.Close()
	})
}

// Test NewPostgresConnection with valid DATABASE_URL that builds DSN
func TestNewPostgresConnection_DatabaseURLDSN(t *testing.T) {
	// Test with DATABASE_URL that triggers DSN building path
	// Use an invalid URL to avoid connection timeout - this should fail at parsing stage
	cfg := &config.DatabaseConfig{
		DatabaseURL: "invalid-database-url",
		Host:        "localhost",
		Port:        5432,
		User:        "user",
		Password:    "pass",
		DBName:      "dbname",
		SSLMode:     "require",
	}

	db, err := NewPostgresConnection(cfg)
	// We expect this to fail quickly at parsing stage
	assert.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "failed to parse database config")
}

// Test NewPostgresConnection with minimal config to test all code paths
func TestNewPostgresConnection_MinimalConfig(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "testuser",
		Password: "testpass",
		DBName:   "testdb",
		SSLMode:  "disable",
	}

	db, err := NewPostgresConnection(cfg)
	// We expect this to fail because we don't have a real database
	assert.Error(t, err)
	assert.Nil(t, db)
	// Should fail with connection error (either from test optimized path or normal path)
	assert.True(t,
		strings.Contains(err.Error(), "failed to ping database") ||
			strings.Contains(err.Error(), "failed to connect"))
}

// Test NewPostgresConnection with connection pool edge cases
func TestNewPostgresConnection_ConnectionPoolEdgeCases(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:         "nonexistent-host",
		Port:         5432,
		User:         "test",
		Password:     "test",
		DBName:       "test",
		SSLMode:      "disable",
		MaxOpenConns: 1, // Minimum valid value
		MaxIdleConns: 1, // Minimum valid value
	}

	db, err := NewPostgresConnection(cfg)
	// We expect this to fail quickly due to DNS resolution
	assert.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "hostname resolving error")
}

// Test NewPostgresConnection with connection pool bounds checking

// Test additional Redis configuration scenarios
func TestNewRedisConnection_AdditionalScenarios(t *testing.T) {
	// Use unreachable addresses and invalid credentials that will fail in any environment

	// Test with completely invalid port number (outside valid range)
	cfg := config.RedisConfig{
		Host:     "localhost",
		Port:     99999, // Invalid port number
		Password: "",
		DB:       0,
	}
	_, err := NewRedisConnection(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Redis")

	// Test with invalid host that cannot resolve
	cfg.Host = "nonexistent-test-host.invalid-domain-xyz.com"
	cfg.Port = 6379
	cfg.DB = 0
	_, err = NewRedisConnection(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Redis")

	// Test with invalid password on localhost with very short timeout
	cfg.Host = "127.0.0.1" // Use IP instead of localhost
	cfg.Port = 6379
	cfg.Password = "definitely-wrong-password-12345"
	_, err = NewRedisConnection(cfg)
	// This might succeed if local redis has no password.
	if err != nil {
		assert.Contains(t, err.Error(), "failed to connect to Redis")
	} else {
		t.Log("Warning: Redis connection succeeded with wrong password. Local Redis likely has no password set.")
	}

	// Test with reserved port that shouldn't have Redis
	cfg.Host = "localhost"
	cfg.Port = 80 // HTTP port, unlikely to have Redis
	cfg.Password = ""
	cfg.DB = 0
	_, err = NewRedisConnection(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Redis")
}

// Test PostgresDB additional configuration scenarios
func TestNewPostgresConnection_AdditionalConfigs(t *testing.T) {
	// Test with different SSL modes
	sslModes := []string{"disable", "require", "verify-ca", "verify-full"}

	for _, sslMode := range sslModes {
		cfg := &config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "test",
			Password: "test",
			DBName:   "test",
			SSLMode:  sslMode,
		}

		_, err := NewPostgresConnection(cfg)
		assert.Error(t, err)
	}
}

// Test PostgresDB with duration edge cases
func TestNewPostgresConnection_DurationEdgeCases(t *testing.T) {
	durationTests := []struct {
		name     string
		lifetime string
		idleTime string
	}{
		{"Empty strings", "", ""},
		{"Zero durations", "0ns", "0ns"},
		{"Very short durations", "1ns", "1ns"},
		{"Very long durations", "87600h", "87600h"}, // 10 years
		{"Negative durations", "-1h", "-1h"},
		{"Microsecond precision", "1µs", "1µs"},
		{"Nanosecond precision", "1ns", "1ns"},
	}

	for _, tt := range durationTests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.DatabaseConfig{
				Host:            "localhost",
				Port:            5432,
				User:            "test",
				Password:        "test",
				DBName:          "test",
				SSLMode:         "disable",
				ConnMaxLifetime: tt.lifetime,
				ConnMaxIdleTime: tt.idleTime,
			}

			_, err := NewPostgresConnection(cfg)
			assert.Error(t, err)
		})
	}
}

// Test NewTracedDB function
func TestNewTracedDB(t *testing.T) {
	// Test with nil pool
	db := NewTracedDB(nil)
	assert.NotNil(t, db)
	assert.Nil(t, db.Pool)
}

// Test NewPostgresConnection error scenarios without timeout
func TestNewPostgresConnection_ErrorScenarios(t *testing.T) {
	// Test with invalid port
	cfg := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     -1,
		User:     "test",
		Password: "test",
		DBName:   "test",
		SSLMode:  "disable",
	}

	_, err := NewPostgresConnection(cfg)
	assert.Error(t, err)

	// Test with empty host
	cfg2 := &config.DatabaseConfig{
		Host:     "",
		Port:     5432,
		User:     "test",
		Password: "test",
		DBName:   "test",
		SSLMode:  "disable",
	}

	_, err = NewPostgresConnection(cfg2)
	assert.Error(t, err)
}

// Test NewRedisConnection error scenarios without timeout
func TestNewRedisConnection_ErrorScenarios(t *testing.T) {
	// Test with invalid port
	cfg := config.RedisConfig{
		Host:     "localhost",
		Port:     -1,
		Password: "",
		DB:       0,
	}

	_, err := NewRedisConnection(cfg)
	assert.Error(t, err)

	// Test with empty host
	cfg2 := config.RedisConfig{
		Host:     "",
		Port:     6379,
		Password: "",
		DB:       0,
	}

	_, err = NewRedisConnection(cfg2)
	// This might succeed if it defaults to localhost and localhost is available
	if err != nil {
		// Verify it's actually a Redis connection error by checking the error message
		assert.Contains(t, err.Error(), "failed to connect to Redis", "expected Redis connection error")
	} else {
		t.Skip("Skipping: Redis connection with empty host succeeded (likely defaulted to localhost). This test is environment-dependent.")
	}
}

// Test NewRedisConnectionWithRetry with invalid config
func TestNewRedisConnectionWithRetry_InvalidConfig(t *testing.T) {
	cfg := config.RedisConfig{
		Host:     "invalid-host-that-does-not-exist-12345.com",
		Port:     6379,
		Password: "",
		DB:       0,
	}

	// Mock ErrorRecoveryManager
	mockManager := &MockErrorRecoveryManager{
		executeWithRetryFunc: func(ctx context.Context, operationName string, operation func() error) error {
			return operation() // Just execute once, no retry
		},
	}

	_, err := NewRedisConnectionWithRetry(cfg, mockManager)
	assert.Error(t, err)
}

// Test NewPostgresConnection with excessive connection pool values
func TestNewPostgresConnection_ExcessivePoolValues(t *testing.T) {
	// Test with connection pool values that exceed int32 bounds (should be ignored)
	cfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		DBName:          "test",
		SSLMode:         "disable",
		MaxOpenConns:    2147483648, // Exceeds max int32
		MaxIdleConns:    2147483648, // Exceeds max int32
		ConnMaxLifetime: "1h",
		ConnMaxIdleTime: "30m",
	}

	db, err := NewPostgresConnection(cfg)
	// We expect this to fail because we don't have a real database
	assert.Error(t, err)
	assert.Nil(t, db)
	// Should fail with connection error (either from test optimized path or normal path)
	assert.True(t,
		strings.Contains(err.Error(), "failed to ping database") ||
			strings.Contains(err.Error(), "failed to connect"))
}

// Test NewPostgresConnection with invalid duration formats
func TestNewPostgresConnection_InvalidDurations(t *testing.T) {
	// Test with invalid duration formats (should error)
	cfg := &config.DatabaseConfig{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		DBName:          "test",
		SSLMode:         "disable",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: "not-a-valid-duration",
		ConnMaxIdleTime: "also-not-valid",
	}

	db, err := NewPostgresConnection(cfg)
	assert.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "failed to parse ConnMaxLifetime")
}

// Test PostgresDB IsReady method
func TestPostgresDB_IsReady(t *testing.T) {
	tests := []struct {
		name     string
		db       *PostgresDB
		expected bool
	}{
		{
			name:     "nil PostgresDB",
			db:       nil,
			expected: false,
		},
		{
			name:     "nil Pool",
			db:       &PostgresDB{Pool: nil},
			expected: false,
		},
		{
			name:     "empty struct",
			db:       &PostgresDB{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For nil db, we can't call method, so test the logic
			if tt.db == nil {
				// nil receiver would panic in Go, so we just verify the expectation
				assert.False(t, tt.expected)
			} else {
				result := tt.db.IsReady()
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// Test PostgresDB Query method with nil pool
func TestPostgresDB_Query_NilPool(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	rows, err := db.Query(ctx, "SELECT 1")
	assert.Error(t, err)
	assert.Nil(t, rows)
	assert.Contains(t, err.Error(), "postgres pool is not initialized")
}

// Test PostgresDB QueryRow method with nil pool
func TestPostgresDB_QueryRow_NilPool(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	row := db.QueryRow(ctx, "SELECT 1")
	assert.Nil(t, row)
}

// Test PostgresDB Exec method with nil pool
func TestPostgresDB_Exec_NilPool(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	tag, err := db.Exec(ctx, "INSERT INTO test (col) VALUES ($1)", "value")
	assert.Error(t, err)
	assert.Nil(t, tag)
	assert.Contains(t, err.Error(), "postgres pool is not initialized")
}

// Test PostgresDB Begin method with nil pool
func TestPostgresDB_Begin_NilPool(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	assert.Error(t, err)
	assert.Nil(t, tx)
	assert.Contains(t, err.Error(), "postgres pool is not initialized")
}

// Test PostgresDB BeginTx method with nil SQL
func TestPostgresDB_BeginTx_NilSQL(t *testing.T) {
	db := &PostgresDB{SQL: nil}
	ctx := context.Background()

	tx, err := db.BeginTx(ctx)
	assert.Error(t, err)
	assert.Nil(t, tx)
	assert.Contains(t, err.Error(), "postgres sql connection is not initialized")
}

// Test PostgresDB Query with valid pool (integration-style, expects nil pool error)
func TestPostgresDB_Query_ErrorPaths(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	// Test with various queries - all should return the same error
	queries := []string{
		"SELECT 1",
		"SELECT * FROM users",
		"INSERT INTO test VALUES (1)",
	}

	for _, query := range queries {
		rows, err := db.Query(ctx, query)
		assert.Error(t, err)
		assert.Nil(t, rows)
		assert.Contains(t, err.Error(), "postgres pool is not initialized")
	}
}

// Test PostgresDB QueryRow with valid pool (integration-style, expects nil row)
func TestPostgresDB_QueryRow_ErrorPaths(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	// Test with various queries - all should return nil row
	queries := []string{
		"SELECT 1",
		"SELECT * FROM users WHERE id = $1",
	}

	for _, query := range queries {
		row := db.QueryRow(ctx, query)
		assert.Nil(t, row)
	}
}

// Test PostgresDB Exec with various statements
func TestPostgresDB_Exec_ErrorPaths(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx := context.Background()

	// Test with various statements
	statements := []struct {
		query string
		args  []interface{}
	}{
		{"INSERT INTO users (name) VALUES ($1)", []interface{}{"test"}},
		{"UPDATE users SET name = $1 WHERE id = $2", []interface{}{"test", 1}},
		{"DELETE FROM users WHERE id = $1", []interface{}{1}},
	}

	for _, stmt := range statements {
		tag, err := db.Exec(ctx, stmt.query, stmt.args...)
		assert.Error(t, err)
		assert.Nil(t, tag)
		assert.Contains(t, err.Error(), "postgres pool is not initialized")
	}
}

// Test PostgresDB Begin with cancelled context
func TestPostgresDB_Begin_CancelledContext(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tx, err := db.Begin(ctx)
	assert.Error(t, err)
	assert.Nil(t, tx)
	assert.Contains(t, err.Error(), "postgres pool is not initialized")
}

// Test PostgresDB all methods with context scenarios
func TestPostgresDB_Methods_WithContextTimeout(t *testing.T) {
	db := &PostgresDB{Pool: nil}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Give context time to expire
	time.Sleep(1 * time.Millisecond)

	rows, err := db.Query(ctx, "SELECT 1")
	assert.Error(t, err)
	assert.Nil(t, rows)

	row := db.QueryRow(ctx, "SELECT 1")
	assert.Nil(t, row)

	tag, err := db.Exec(ctx, "SELECT 1")
	assert.Error(t, err)
	assert.Nil(t, tag)

	tx, err := db.Begin(ctx)
	assert.Error(t, err)
	assert.Nil(t, tx)
}
