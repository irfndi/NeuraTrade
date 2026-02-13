package services

import (
	"context"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

type mockDBPool struct {
	ready bool
}

func (m *mockDBPool) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	return nil, nil
}

func (m *mockDBPool) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return nil
}

func (m *mockDBPool) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	return database.PgxResult{CommandTag: pgconn.CommandTag{}}, nil
}

func (m *mockDBPool) Begin(ctx context.Context) (database.Tx, error) {
	return nil, nil
}

func (m *mockDBPool) IsReady() bool {
	return m.ready
}

func TestIsNilDBPool_NilInput(t *testing.T) {
	result := isNilDBPool(nil)
	assert.True(t, result)
}

func TestIsNilDBPool_NilPointer(t *testing.T) {
	var db *mockDBPool
	result := isNilDBPool(db)
	assert.True(t, result)
}

func TestIsNilDBPool_NotReady(t *testing.T) {
	db := &mockDBPool{ready: false}
	result := isNilDBPool(db)
	assert.True(t, result)
}

func TestIsNilDBPool_Ready(t *testing.T) {
	db := &mockDBPool{ready: true}
	result := isNilDBPool(db)
	assert.False(t, result)
}

func TestIsNilDBPool_NoIsReadyMethod(t *testing.T) {
	db := &simpleMockDBPool{}
	result := isNilDBPool(db)
	assert.False(t, result)
}

type simpleMockDBPool struct{}

func (m *simpleMockDBPool) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	return nil, nil
}

func (m *simpleMockDBPool) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return nil
}

func (m *simpleMockDBPool) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	return database.PgxResult{CommandTag: pgconn.CommandTag{}}, nil
}

func (m *simpleMockDBPool) Begin(ctx context.Context) (database.Tx, error) {
	return nil, nil
}

func TestIsNilDBPool_VariousStates(t *testing.T) {
	tests := []struct {
		name     string
		db       DBPool
		expected bool
	}{
		{"nil", nil, true},
		{"ready pool", &mockDBPool{ready: true}, false},
		{"not ready pool", &mockDBPool{ready: false}, true},
		{"simple pool", &simpleMockDBPool{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNilDBPool(tt.db)
			assert.Equal(t, tt.expected, result)
		})
	}
}
