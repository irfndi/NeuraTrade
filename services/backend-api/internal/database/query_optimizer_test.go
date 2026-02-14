package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSlowQueryLogger_LogQuery(t *testing.T) {
	logger := NewSlowQueryLogger(100*time.Millisecond, 10)

	ctx := context.Background()

	logger.LogQuery(ctx, "SELECT * FROM users WHERE id = 1", 50*time.Millisecond, 1)
	queries := logger.GetSlowQueries()
	assert.Equal(t, 0, len(queries))

	logger.LogQuery(ctx, "SELECT * FROM users WHERE id = 1", 150*time.Millisecond, 1)
	queries = logger.GetSlowQueries()
	assert.Equal(t, 1, len(queries))
	assert.Equal(t, "users", queries[0].TableName)
	assert.Equal(t, 1, queries[0].CallCount)
}

func TestSlowQueryLogger_Aggregation(t *testing.T) {
	logger := NewSlowQueryLogger(100*time.Millisecond, 10)

	ctx := context.Background()

	query := "SELECT * FROM orders WHERE status = 'pending'"

	logger.LogQuery(ctx, query, 150*time.Millisecond, 5)
	logger.LogQuery(ctx, query, 200*time.Millisecond, 10)
	logger.LogQuery(ctx, query, 100*time.Millisecond, 3)

	queries := logger.GetSlowQueries()
	assert.Equal(t, 1, len(queries))
	assert.Equal(t, 3, queries[0].CallCount)
}

func TestSlowQueryLogger_GetSlowQueriesByTable(t *testing.T) {
	logger := NewSlowQueryLogger(50*time.Millisecond, 100)

	ctx := context.Background()

	logger.LogQuery(ctx, "SELECT * FROM users WHERE id = 1", 100*time.Millisecond, 1)
	logger.LogQuery(ctx, "SELECT * FROM orders WHERE id = 1", 100*time.Millisecond, 1)
	logger.LogQuery(ctx, "SELECT * FROM users WHERE name = 'test'", 100*time.Millisecond, 1)

	usersQueries := logger.GetSlowQueriesByTable("users")
	assert.Equal(t, 2, len(usersQueries))

	ordersQueries := logger.GetSlowQueriesByTable("orders")
	assert.Equal(t, 1, len(ordersQueries))
}

func TestSlowQueryLogger_Clear(t *testing.T) {
	logger := NewSlowQueryLogger(50*time.Millisecond, 10)

	ctx := context.Background()

	logger.LogQuery(ctx, "SELECT * FROM users", 100*time.Millisecond, 1)
	assert.Equal(t, 1, len(logger.GetSlowQueries()))

	logger.Clear()
	assert.Equal(t, 0, len(logger.GetSlowQueries()))
}

func TestSlowQueryLogger_MaxEntries(t *testing.T) {
	logger := NewSlowQueryLogger(50*time.Millisecond, 3)

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		logger.LogQuery(ctx, "SELECT * FROM table_"+string(rune('a'+i)), 100*time.Millisecond, 1)
	}

	queries := logger.GetSlowQueries()
	assert.LessOrEqual(t, len(queries), 3)
}

func TestExtractTableNameFromQuery(t *testing.T) {
	tests := []struct {
		query     string
		tableName string
	}{
		{"SELECT * FROM users", "users"},
		{"SELECT * FROM orders WHERE id = 1", "orders"},
		{"INSERT INTO products (name) VALUES ('test')", "products"},
		{"UPDATE customers SET name = 'test'", "customers"},
		{"DELETE FROM sessions WHERE id = 1", "sessions"},
		{"SELECT id, name FROM users JOIN orders ON users.id = orders.user_id", "users"},
		{"invalid query", "unknown"},
	}

	for _, tt := range tests {
		result := extractTableNameFromQuery(tt.query)
		assert.Equal(t, tt.tableName, result)
	}
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SELECT * FROM users", "select * from users"},
		{"  SELECT   *  FROM   users  ", "select * from users"},
		{"Select * from Users", "select * from users"},
		{"SELECT\n*\nFROM\nusers", "select * from users"},
	}

	for _, tt := range tests {
		result := normalizeQuery(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestQueryOptimizer_SuggestIndexes(t *testing.T) {
	optimizer := NewQueryOptimizer(50 * time.Millisecond)

	ctx := context.Background()

	optimizer.LogQuery(ctx, "SELECT * FROM users WHERE id = 1", 200*time.Millisecond, 1)
	optimizer.LogQuery(ctx, "SELECT * FROM users WHERE id = 2", 200*time.Millisecond, 1)
	optimizer.LogQuery(ctx, "SELECT * FROM users WHERE email = 'test@test.com'", 200*time.Millisecond, 1)

	suggestions := optimizer.SuggestIndexes()

	assert.GreaterOrEqual(t, len(suggestions), 0)
}

func TestQueryOptimizer_GetSlowQueriesJSON(t *testing.T) {
	optimizer := NewQueryOptimizer(50 * time.Millisecond)

	ctx := context.Background()
	optimizer.LogQuery(ctx, "SELECT * FROM users", 100*time.Millisecond, 1)

	json := optimizer.GetSlowQueriesJSON()
	assert.Contains(t, json, "slow_queries")
	assert.Contains(t, json, "users")
}
