package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteDB wraps a SQLite database connection.
type SQLiteDB struct {
	DB *sql.DB
}

func NewSQLiteConnection(path string) (*SQLiteDB, error) {
	return NewSQLiteConnectionWithExtension(path, "")
}

func NewSQLiteConnectionWithExtension(path, extensionPath string) (*SQLiteDB, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite database path is required")
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -64000",
	}
	for _, pragma := range pragmas {
		if _, err = db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to apply sqlite pragma %q: %w", pragma, err)
		}
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	if strings.TrimSpace(extensionPath) != "" {
		if _, err = db.Exec("SELECT load_extension(?)", strings.TrimSpace(extensionPath)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to load sqlite extension %q: %w", extensionPath, err)
		}
	}

	return &SQLiteDB{DB: db}, nil
}

// Close implements the services.DBPool interface.
func (db *SQLiteDB) Close() error {
	if db == nil || db.DB == nil {
		return nil
	}
	return db.DB.Close()
}

// Query implements the services.DBPool interface.
func (db *SQLiteDB) Query(ctx context.Context, sql string, args ...any) (*sql.Rows, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}
	return db.DB.QueryContext(ctx, sql, args...)
}

// QueryRow implements the services.DBPool interface.
func (db *SQLiteDB) QueryRow(ctx context.Context, sql string, args ...any) *sql.Row {
	if db == nil || db.DB == nil {
		return nil
	}
	return db.DB.QueryRowContext(ctx, sql, args...)
}

// Exec implements the services.DBPool interface.
func (db *SQLiteDB) Exec(ctx context.Context, sql string, args ...any) (sql.Result, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}
	return db.DB.ExecContext(ctx, sql, args...)
}

// BeginTx implements the services.DBPool interface.
func (db *SQLiteDB) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}
	return db.DB.BeginTx(ctx, nil)
}

func (db *SQLiteDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if db == nil || db.DB == nil {
		return nil, fmt.Errorf("sqlite database is not initialized")
	}

	return nil, fmt.Errorf("sqlite pgx transactions are not supported")
}

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
