package services

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
)

func applySQLiteMigrationBySuffix(t *testing.T, db *database.SQLiteDB, filenameSuffix string) {
	t.Helper()

	migrationPath := findSQLiteMigrationBySuffix(t, filenameSuffix)
	sqlBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("failed to read migration %s: %v", filepath.Base(migrationPath), err)
	}
	if _, err := db.Exec(context.Background(), string(sqlBytes)); err != nil {
		t.Fatalf("failed to apply migration %s: %v", filepath.Base(migrationPath), err)
	}
}

func findSQLiteMigrationBySuffix(t *testing.T, filenameSuffix string) string {
	t.Helper()

	dir := sqliteMigrationDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list sqlite migrations: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), filenameSuffix) {
			return filepath.Join(dir, entry.Name())
		}
	}
	t.Fatalf("sqlite migration with suffix %q not found", filenameSuffix)
	return ""
}

func sqliteMigrationDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve sqlite migration helper path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "database", "sqlite_migrations"))
}
