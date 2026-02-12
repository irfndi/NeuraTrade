package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
)

type MockDBPool struct {
	mock pgxmock.PgxPoolIface
}

func NewMockDBPool(mock pgxmock.PgxPoolIface) DBPool {
	return &MockDBPool{mock: mock}
}

func NewMockDBPoolFromNewPool() (DBPool, pgxmock.PgxPoolIface, error) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		return nil, nil, err
	}
	return &MockDBPool{mock: mockPool}, mockPool, nil
}

func (m *MockDBPool) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := m.mock.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxRows{Rows: rows}, nil
}

func (m *MockDBPool) QueryRow(ctx context.Context, query string, args ...any) Row {
	return PgxRow{Row: m.mock.QueryRow(ctx, query, args...)}
}

func (m *MockDBPool) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	tag, err := m.mock.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxResult{CommandTag: tag}, nil
}

func (m *MockDBPool) Begin(ctx context.Context) (Tx, error) {
	tx, err := m.mock.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return PgxTx{Tx: tx}, nil
}

func (m *MockDBPool) Close() {
	m.mock.Close()
}

func (m *MockDBPool) ExpectationsWereMet() error {
	return m.mock.ExpectationsWereMet()
}

func (m *MockDBPool) PgxMock() pgxmock.PgxPoolIface {
	return m.mock
}

type MockTxAdapter struct {
	tx pgx.Tx
}

// NewMockTxAdapter creates a transaction adapter by beginning a real transaction from the mock pool.
// This ensures queries are executed in a transaction context for proper test behavior.
func NewMockTxAdapter(ctx context.Context, mock pgxmock.PgxPoolIface) (Tx, error) {
	tx, err := mock.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &MockTxAdapter{tx: tx}, nil
}

func (m *MockTxAdapter) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := m.tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxRows{Rows: rows}, nil
}

func (m *MockTxAdapter) QueryRow(ctx context.Context, query string, args ...any) Row {
	return PgxRow{Row: m.tx.QueryRow(ctx, query, args...)}
}

func (m *MockTxAdapter) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	tag, err := m.tx.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return PgxResult{CommandTag: tag}, nil
}

func (m *MockTxAdapter) Commit(ctx context.Context) error {
	if m.tx != nil {
		return m.tx.Commit(ctx)
	}
	return nil
}

func (m *MockTxAdapter) Rollback(ctx context.Context) error {
	if m.tx != nil {
		return m.tx.Rollback(ctx)
	}
	return nil
}
