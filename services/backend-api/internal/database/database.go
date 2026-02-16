package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/irfndi/neuratrade/internal/config"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	_ "github.com/mattn/go-sqlite3"
)

// Database abstracts both PostgreSQL and SQLite connections.
// Use this interface for all database operations to ensure driver agnosticism.
type Database interface {
	DBPool
	Close() error
	IsReady() bool
	HealthCheck(ctx context.Context) error
	BeginTx(ctx context.Context) (*sql.Tx, error)
}

// DBType enumerates supported database drivers.
type DBType string

const (
	DBTypeSQLite    DBType = "sqlite"
	DBTypePostgres  DBType = "postgres"
	DBTypePostgreSQL DBType = "postgresql"
)

// NewDatabaseConnection creates a database connection based on the driver configuration.
// It supports both SQLite and PostgreSQL drivers.
//
// Parameters:
//   cfg: Database configuration containing driver type and connection parameters.
//
// Returns:
//   Database: The initialized database connection.
//   error: Error if connection fails.
func NewDatabaseConnection(cfg *config.DatabaseConfig) (Database, error) {
	return NewDatabaseConnectionWithContext(context.Background(), cfg)
}

// NewDatabaseConnectionWithContext creates a database connection with a specified context.
//
// Parameters:
//   ctx: Context for the connection establishment.
//   cfg: Database configuration containing driver type and connection parameters.
//
// Returns:
//   Database: The initialized database connection.
//   error: Error if connection fails.
func NewDatabaseConnectionWithContext(ctx context.Context, cfg *config.DatabaseConfig) (Database, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "sqlite" // Default to SQLite
	}

	switch driver {
	case "sqlite":
		path := cfg.SQLitePath
		if path == "" {
			path = "neuratrade.db"
		}
		zaplogrus.Infof("Connecting to SQLite database: %s", path)
		return NewSQLiteConnectionWithExtension(path, cfg.SQLiteVectorExtensionPath)
	
	case "postgres", "postgresql":
		zaplogrus.Infof("Connecting to PostgreSQL database: %s@%s:%d/%s", cfg.User, cfg.Host, cfg.Port, cfg.DBName)
		return NewPostgresConnectionWithContext(ctx, cfg)
	
	default:
		return nil, fmt.Errorf("unsupported database driver: %s (supported: sqlite, postgres)", driver)
	}
}

// DetectDBType detects the database type from the driver string.
func DetectDBType(driver string) DBType {
	driver = strings.ToLower(strings.TrimSpace(driver))
	switch driver {
	case "sqlite", "sqlite3":
		return DBTypeSQLite
	case "postgres", "postgresql", "pgx":
		return DBTypePostgres
	default:
		return DBTypeSQLite // Default to SQLite for unknown drivers
	}
}

// NormalizeDriver normalizes the driver string to a canonical form.
func NormalizeDriver(driver string) string {
	return string(DetectDBType(driver))
}
