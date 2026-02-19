package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSQLiteConnection tests SQLite connection creation
func TestSQLiteConnection(t *testing.T) {
	// Create a temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	// Verify database file was created
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}

// TestSQLiteConnection_WithExtension tests SQLite connection with extension
func TestSQLiteConnection_WithExtension(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Test with empty extension path (should work)
	db, err := NewSQLiteConnectionWithExtension(dbPath, "")
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	// Test with non-existent extension path (should fail)
	db2, err := NewSQLiteConnectionWithExtension(filepath.Join(tmpDir, "test2.db"), "/nonexistent/extension.so")
	assert.Error(t, err)
	assert.Nil(t, db2)
}

// TestSQLiteConnection_EmptyPath tests SQLite connection with empty path
func TestSQLiteConnection_EmptyPath(t *testing.T) {
	db, err := NewSQLiteConnection("")
	assert.Error(t, err)
	assert.Nil(t, db)
}

// TestSQLiteDB_Close tests SQLite database close
func TestSQLiteDB_Close(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)

	// Close should succeed
	err = db.Close()
	assert.NoError(t, err)

	// Close again should not error (idempotent)
	err = db.Close()
	assert.NoError(t, err)
}

// TestSQLiteDB_IsReady tests SQLite database readiness check
func TestSQLiteDB_IsReady(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	assert.True(t, db.IsReady())

	// Test nil database
	var nilDB *SQLiteDB
	assert.False(t, nilDB.IsReady())
}

// TestSQLiteDB_HealthCheck tests SQLite health check
func TestSQLiteDB_HealthCheck(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()
	err = db.HealthCheck(ctx)
	assert.NoError(t, err)
}

// TestSQLiteDB_Query tests SQLite query operations
func TestSQLiteDB_Query(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()

	// Create a test table
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Insert test data
	_, err = db.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "John Doe", "john@example.com")
	require.NoError(t, err)

	// Query the data
	rows, err := db.Query(ctx, "SELECT id, name, email FROM test_users WHERE name = ?", "John Doe")
	require.NoError(t, err)
	defer rows.Close()

	// Scan the results
	count := 0
	for rows.Next() {
		var id int64
		var name, email string
		err = rows.Scan(&id, &name, &email)
		require.NoError(t, err)
		assert.Equal(t, "John Doe", name)
		assert.Equal(t, "john@example.com", email)
		count++
	}
	assert.Equal(t, 1, count)

	// Check for iteration errors
	err = rows.Err()
	assert.NoError(t, err)
}

// TestSQLiteDB_QueryRow tests SQLite query row operations
func TestSQLiteDB_QueryRow(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()

	// Create test table
	_, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS test_config (key TEXT PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	// Insert test data
	_, err = db.Exec(ctx, "INSERT INTO test_config (key, value) VALUES (?, ?)", "test_key", "test_value")
	require.NoError(t, err)

	// Query single row
	row := db.QueryRow(ctx, "SELECT value FROM test_config WHERE key = ?", "test_key")
	var value string
	err = row.Scan(&value)
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

// TestSQLiteDB_Exec tests SQLite exec operations
func TestSQLiteDB_Exec(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()

	// Create test table
	result, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_exec (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			data TEXT
		)
	`)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Insert data
	insertResult, err := db.Exec(ctx, "INSERT INTO test_exec (data) VALUES (?)", "test data")
	require.NoError(t, err)

	rowsAffected, err := insertResult.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), rowsAffected)
}

// TestSQLiteDB_Begin tests SQLite transaction begin
func TestSQLiteDB_Begin(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()

	// Create test table
	_, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS test_tx (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Insert data in transaction
	_, err = tx.Exec(ctx, "INSERT INTO test_tx (value) VALUES (?)", "tx_value")
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit(ctx)
	require.NoError(t, err)

	// Verify data was committed
	row := db.QueryRow(ctx, "SELECT value FROM test_tx WHERE value = ?", "tx_value")
	var value string
	err = row.Scan(&value)
	require.NoError(t, err)
	assert.Equal(t, "tx_value", value)
}

// TestSQLiteDB_BeginTx tests SQLite transaction with options
func TestSQLiteDB_BeginTx(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()

	// Create test table
	_, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS test_txtx (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	// Begin transaction with options
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Insert data
	_, err = tx.Exec(ctx, "INSERT INTO test_txtx (value) VALUES (?)", "txtx_value")
	require.NoError(t, err)

	// Rollback transaction
	err = tx.Rollback(ctx)
	require.NoError(t, err)

	// Verify data was rolled back
	row := db.QueryRow(ctx, "SELECT COUNT(*) FROM test_txtx WHERE value = ?", "txtx_value")
	var count int
	err = row.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestSQLiteDB_TransactionRollback tests transaction rollback
func TestSQLiteDB_TransactionRollback(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()

	// Create test table
	_, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS test_rollback (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	// Insert initial data
	_, err = db.Exec(ctx, "INSERT INTO test_rollback (value) VALUES (?)", "initial")
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.Begin(ctx)
	require.NoError(t, err)

	// Insert more data
	_, err = tx.Exec(ctx, "INSERT INTO test_rollback (value) VALUES (?)", "rolled_back")
	require.NoError(t, err)

	// Rollback
	err = tx.Rollback(ctx)
	require.NoError(t, err)

	// Verify only initial data exists
	rows, err := db.Query(ctx, "SELECT value FROM test_rollback")
	require.NoError(t, err)
	defer rows.Close()

	values := []string{}
	for rows.Next() {
		var value string
		err = rows.Scan(&value)
		require.NoError(t, err)
		values = append(values, value)
	}

	assert.Equal(t, []string{"initial"}, values)
}

// TestSQLiteDB_NilDatabase tests operations with nil database
func TestSQLiteDB_NilDatabase(t *testing.T) {
	var db *SQLiteDB
	ctx := context.Background()

	// Query should return error
	_, err := db.Query(ctx, "SELECT 1")
	assert.Error(t, err)

	// Exec should return error
	_, err = db.Exec(ctx, "SELECT 1")
	assert.Error(t, err)

	// Begin should return error
	_, err = db.Begin(ctx)
	assert.Error(t, err)

	// BeginTx should return error
	_, err = db.BeginTx(ctx)
	assert.Error(t, err)

	// HealthCheck should return error
	err = db.HealthCheck(ctx)
	assert.Error(t, err)

	// IsReady should return false
	assert.False(t, db.IsReady())

	// Close should not panic
	assert.NotPanics(t, func() {
		err := db.Close()
		assert.NoError(t, err)
	})
}

// TestNewDatabaseConnection tests the unified database connection factory
func TestNewDatabaseConnection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Test SQLite connection
	cfg := &config.DatabaseConfig{
		Driver:     "sqlite",
		SQLitePath: dbPath,
	}

	db, err := NewDatabaseConnection(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	assert.True(t, db.IsReady())
	assert.IsType(t, &SQLiteDB{}, db)
}

// TestNewDatabaseConnection_Postgres tests PostgreSQL connection via factory
func TestNewDatabaseConnection_Postgres(t *testing.T) {
	// Test with PostgreSQL config (will fail without actual database)
	cfg := &config.DatabaseConfig{
		Driver:   "postgres",
		Host:     "localhost",
		Port:     5432,
		User:     "test",
		Password: "test",
		DBName:   "test",
		SSLMode:  "disable",
	}

	db, err := NewDatabaseConnection(cfg)
	assert.Error(t, err)
	assert.Nil(t, db)
}

// TestNewDatabaseConnection_DefaultDriver tests default driver selection
func TestNewDatabaseConnection_DefaultDriver(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Test with empty driver (should default to SQLite)
	cfg := &config.DatabaseConfig{
		SQLitePath: dbPath,
	}

	db, err := NewDatabaseConnection(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	assert.IsType(t, &SQLiteDB{}, db)
}

// TestNewDatabaseConnection_UnknownDriver tests unknown driver handling
func TestNewDatabaseConnection_UnknownDriver(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Driver: "unknown_driver",
	}

	db, err := NewDatabaseConnection(cfg)
	assert.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "unsupported database driver")
}

// TestDetectDBType tests database type detection
func TestDetectDBType(t *testing.T) {
	tests := []struct {
		driver   string
		expected DBType
	}{
		{"sqlite", DBTypeSQLite},
		{"sqlite3", DBTypeSQLite},
		{"postgres", DBTypePostgres},
		{"postgresql", DBTypePostgres},
		{"pgx", DBTypePostgres},
		{"", DBTypeSQLite},
		{"unknown", DBTypeSQLite},
		{" SQLITE ", DBTypeSQLite},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			result := DetectDBType(tt.driver)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeDriver tests driver normalization
func TestNormalizeDriver(t *testing.T) {
	tests := []struct {
		driver   string
		expected string
	}{
		{"sqlite", "sqlite"},
		{"sqlite3", "sqlite"},
		{"postgres", "postgres"},
		{"postgresql", "postgres"},
		{"pgx", "postgres"},
		{"", "sqlite"},
		{"unknown", "sqlite"},
		{" SQLITE ", "sqlite"},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			result := NormalizeDriver(tt.driver)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSQLiteDB_QueryWithTimeout tests query with timeout context
func TestSQLiteDB_QueryWithTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Simple query should succeed
	rows, err := db.Query(ctx, "SELECT 1")
	require.NoError(t, err)
	defer rows.Close()
}

// TestSQLiteDB_ConcurrentAccess tests concurrent database access
func TestSQLiteDB_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	ctx := context.Background()

	// Create test table
	_, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS concurrent_test (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	// Run concurrent inserts
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(val int) {
			_, err := db.Exec(ctx, "INSERT INTO concurrent_test (value) VALUES (?)", val)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all inserts succeeded
	row := db.QueryRow(ctx, "SELECT COUNT(*) FROM concurrent_test")
	var count int
	err = row.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}
