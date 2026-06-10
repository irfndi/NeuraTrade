package handlers

import (
	"context"
	"fmt"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
)

type readOnlyDBAdapter struct {
	pool database.DatabasePool
}

func (a readOnlyDBAdapter) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	return a.pool.Query(ctx, query, args...)
}

func (a readOnlyDBAdapter) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return a.pool.QueryRow(ctx, query, args...)
}

func (a readOnlyDBAdapter) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	return a.pool.Exec(ctx, query, args...)
}

func (a readOnlyDBAdapter) Begin(context.Context) (database.Tx, error) {
	return nil, fmt.Errorf("begin transaction is not supported by this adapter")
}

// IsSQLitePool reports whether the wrapped database is the in-process
// *internaldb.SQLiteDB. The services-package scalping backtest engine
// needs this signal to decide between SQLite-flavored and PostgreSQL-
// flavored SQL without depending on this package.
func (a readOnlyDBAdapter) IsSQLitePool() bool {
	probe, ok := a.pool.(interface{ IsSQLiteBackend() bool })
	return ok && probe != nil && probe.IsSQLiteBackend()
}

func normalizeDBPool(db any) services.DBPool {
	switch typed := db.(type) {
	case nil:
		return nil
	case services.DBPool:
		return typed
	case database.LegacyDBPool:
		return database.WrapLegacyDBPool(typed)
	case database.DatabasePool:
		return readOnlyDBAdapter{pool: typed}
	case database.LegacyQuerier:
		return readOnlyDBAdapter{pool: database.WrapLegacyQuerier(typed)}
	default:
		return nil
	}
}
