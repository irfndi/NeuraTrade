package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteDB wraps a SQLite connection.
type SQLiteDB struct {
	DB *sql.DB
}

// Ensure SQLiteDB implements Database interface.
var _ Database = (*SQLiteDB)(nil)

// NewSQLiteConnection creates a new SQLite connection with default settings.
func NewSQLiteConnection(path string) (*SQLiteDB, error) {
	return NewSQLiteConnectionWithExtension(path, "")
}

// NewSQLiteConnectionWithExtension creates a new SQLite connection with optional vector extension.
func NewSQLiteConnectionWithExtension(path, extensionPath string) (*SQLiteDB, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite database path is required")
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Configure connection pool for optimal SQLite performance
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Apply SQLite pragmas for performance and safety
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -64000", // 64MB cache
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 268435456", // 256MB memory-mapped I/O
	}
	for _, pragma := range pragmas {
		if _, err = db.Exec(pragma); err != nil {
			zaplogrus.Warnf("SQLite pragma %q failed: %v", pragma, err)
			// Don't fail on pragma errors, just warn
		}
	}

	// Test connectivity
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	// Load optional vector extension
	if strings.TrimSpace(extensionPath) != "" {
		if _, err = db.Exec("SELECT load_extension(?)", strings.TrimSpace(extensionPath)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to load sqlite extension %q: %w", extensionPath, err)
		}
		zaplogrus.Infof("Loaded SQLite extension: %s", extensionPath)
	}

	zaplogrus.Infof("Successfully connected to SQLite database: %s", path)
	return &SQLiteDB{DB: db}, nil
}

// Close closes the SQLite database connection.
func (db *SQLiteDB) Close() error {
	if db == nil || db.DB == nil {
		return nil
	}
	if err := db.DB.Close(); err != nil {
		zaplogrus.WithError(err).Warn("Failed to close SQLite database")
		return err
	}
	zaplogrus.Info("SQLite database connection closed")
	return nil
}

// Query executes a query that returns rows.
func (db *SQLiteDB) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}
	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return SQLRows{Rows: rows}, nil
}

// QueryRow executes a query that returns a single row.
func (db *SQLiteDB) QueryRow(ctx context.Context, query string, args ...any) Row {
	if db == nil || db.DB == nil {
		return SQLRow{}
	}
	return SQLRow{Row: db.DB.QueryRowContext(ctx, query, args...)}
}

// Exec executes a query without returning rows.
func (db *SQLiteDB) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}
	res, err := db.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return SQLResult{Result: res}, nil
}

// BeginTx starts a transaction with context.
func (db *SQLiteDB) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}
	return db.DB.BeginTx(ctx, nil)
}

// Begin starts a transaction.
func (db *SQLiteDB) Begin(ctx context.Context) (Tx, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}

	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return SQLTx{Tx: tx}, nil
}

// IsReady checks if the database connection is ready.
func (db *SQLiteDB) IsReady() bool {
	return db != nil && db.DB != nil
}

// HealthCheck performs a simple connectivity check.
func (db *SQLiteDB) HealthCheck(ctx context.Context) error {
	if db == nil || db.DB == nil {
		return fmt.Errorf("sqlite database is not initialized")
	}
	return db.DB.PingContext(ctx)
}
