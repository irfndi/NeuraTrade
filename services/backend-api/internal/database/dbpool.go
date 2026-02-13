package database

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

type Row interface {
	Scan(dest ...any) error
}

type Result interface {
	RowsAffected() (int64, error)
}

type Tx interface {
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type DBPool interface {
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	Begin(ctx context.Context) (Tx, error)
}

type PgxRows struct{ pgx.Rows }

func (r PgxRows) Scan(dest ...any) error {
	return r.Rows.Scan(dest...)
}

func (r PgxRows) Close() {
	r.Rows.Close()
}

func (r PgxRows) Err() error {
	return r.Rows.Err()
}

func (r PgxRows) Next() bool {
	return r.Rows.Next()
}

type PgxRow struct{ pgx.Row }

func (r PgxRow) Scan(dest ...any) error {
	return r.Row.Scan(dest...)
}

type PgxResult struct{ pgconn.CommandTag }

func (r PgxResult) RowsAffected() (int64, error) {
	return r.CommandTag.RowsAffected(), nil
}

type PgxTx struct{ pgx.Tx }

func (t PgxTx) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := t.Tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxRows{Rows: rows}, nil
}

func (t PgxTx) QueryRow(ctx context.Context, query string, args ...any) Row {
	return PgxRow{Row: t.Tx.QueryRow(ctx, query, args...)}
}

func (t PgxTx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	tag, err := t.Tx.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxResult{CommandTag: tag}, nil
}

func (t PgxTx) Commit(ctx context.Context) error {
	return t.Tx.Commit(ctx)
}

func (t PgxTx) Rollback(ctx context.Context) error {
	return t.Tx.Rollback(ctx)
}

type SQLRows struct{ *sql.Rows }

func (r SQLRows) Scan(dest ...any) error {
	return r.Rows.Scan(dest...)
}

func (r SQLRows) Close() {
	_ = r.Rows.Close()
}

func (r SQLRows) Err() error {
	return r.Rows.Err()
}

func (r SQLRows) Next() bool {
	return r.Rows.Next()
}

type SQLRow struct{ *sql.Row }

func (r SQLRow) Scan(dest ...any) error {
	return r.Row.Scan(dest...)
}

type SQLResult struct{ sql.Result }

func (r SQLResult) RowsAffected() (int64, error) {
	return r.Result.RowsAffected()
}

type SQLTx struct{ *sql.Tx }

func (t SQLTx) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := t.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return SQLRows{Rows: rows}, nil
}

func (t SQLTx) QueryRow(ctx context.Context, query string, args ...any) Row {
	return SQLRow{Row: t.QueryRowContext(ctx, query, args...)}
}

func (t SQLTx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	res, err := t.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return SQLResult{Result: res}, nil
}

func (t SQLTx) Commit(ctx context.Context) error {
	return t.Tx.Commit()
}

func (t SQLTx) Rollback(ctx context.Context) error {
	return t.Tx.Rollback()
}

type LegacyQuerier interface {
	Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error)
}

type LegacyDBPool interface {
	LegacyQuerier
	Begin(ctx context.Context) (pgx.Tx, error)
}

type wrappedLegacyQuerier struct {
	legacy LegacyQuerier
}

func (w wrappedLegacyQuerier) Query(ctx context.Context, query string, args ...interface{}) (Rows, error) {
	rows, err := w.legacy.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxRows{Rows: rows}, nil
}

func (w wrappedLegacyQuerier) QueryRow(ctx context.Context, query string, args ...interface{}) Row {
	return PgxRow{Row: w.legacy.QueryRow(ctx, query, args...)}
}

func (w wrappedLegacyQuerier) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	tag, err := w.legacy.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxResult{CommandTag: tag}, nil
}

type wrappedLegacyDBPool struct {
	wrappedLegacyQuerier
	legacy LegacyDBPool
}

func (w wrappedLegacyDBPool) Begin(ctx context.Context) (Tx, error) {
	tx, err := w.legacy.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return PgxTx{Tx: tx}, nil
}

func WrapLegacyQuerier(legacy LegacyQuerier) DatabasePool {
	return wrappedLegacyQuerier{legacy: legacy}
}

func WrapLegacyDBPool(legacy LegacyDBPool) DBPool {
	return wrappedLegacyDBPool{
		wrappedLegacyQuerier: wrappedLegacyQuerier{legacy: legacy},
		legacy:               legacy,
	}
}
