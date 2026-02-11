package services

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBPool defines the driver-agnostic interface for database operations.
// This interface allows switching between PostgreSQL and SQLite without
// changing service layer code.
type DBPool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SignalAggregatorInterface defines the interface for signal aggregation
type SignalAggregatorInterface interface {
	AggregateArbitrageSignals(ctx context.Context, input ArbitrageSignalInput) ([]*AggregatedSignal, error)
	AggregateTechnicalSignals(ctx context.Context, input TechnicalSignalInput) ([]*AggregatedSignal, error)
	DeduplicateSignals(ctx context.Context, signals []*AggregatedSignal) ([]*AggregatedSignal, error)
}

func isNilDBPool(db DBPool) bool {
	if db == nil {
		return true
	}

	v := reflect.ValueOf(db)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return true
	}

	if checker, ok := db.(interface{ IsReady() bool }); ok {
		return !checker.IsReady()
	}

	return false
}
