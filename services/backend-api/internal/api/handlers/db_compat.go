package handlers

import (
	"context"
	"fmt"

	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type legacyDBQuerier interface {
	Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error)
}

type legacyDBQuerierAdapter struct {
	legacy legacyDBQuerier
}

func (a legacyDBQuerierAdapter) Query(ctx context.Context, query string, args ...interface{}) (database.Rows, error) {
	rows, err := a.legacy.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return database.PgxRows{Rows: rows}, nil
}

func (a legacyDBQuerierAdapter) QueryRow(ctx context.Context, query string, args ...interface{}) database.Row {
	return database.PgxRow{Row: a.legacy.QueryRow(ctx, query, args...)}
}

func (a legacyDBQuerierAdapter) Exec(ctx context.Context, query string, args ...interface{}) (database.Result, error) {
	tag, err := a.legacy.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return database.PgxResult{CommandTag: tag}, nil
}

func resolveDBQuerier(input any) (DBQuerier, error) {
	if input == nil {
		return nil, fmt.Errorf("nil database querier")
	}

	if q, ok := input.(DBQuerier); ok {
		return q, nil
	}

	if legacy, ok := input.(legacyDBQuerier); ok {
		return legacyDBQuerierAdapter{legacy: legacy}, nil
	}

	return nil, fmt.Errorf("unsupported db querier type %T", input)
}
