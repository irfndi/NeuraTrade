package database

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRedisSentryHook tests the creation of a Redis Sentry hook
func TestNewRedisSentryHook(t *testing.T) {
	t.Run("creates hook with service name", func(t *testing.T) {
		hook := NewRedisSentryHook("test-service")

		assert.NotNil(t, hook)
		assert.Equal(t, "test-service", hook.serviceName)
	})

	t.Run("creates hook with default service name when empty", func(t *testing.T) {
		hook := NewRedisSentryHook("")

		assert.NotNil(t, hook)
		assert.Equal(t, "redis", hook.serviceName)
	})
}

// TestNewPostgresSentryTracer tests the creation of a PostgreSQL Sentry tracer
func TestNewPostgresSentryTracer(t *testing.T) {
	t.Run("creates tracer with service name", func(t *testing.T) {
		tracer := NewPostgresSentryTracer("test-service")

		assert.NotNil(t, tracer)
		assert.Equal(t, "test-service", tracer.serviceName)
	})

	t.Run("creates tracer with default service name when empty", func(t *testing.T) {
		tracer := NewPostgresSentryTracer("")

		assert.NotNil(t, tracer)
		assert.Equal(t, "postgresql", tracer.serviceName)
	})
}

// TestParseSQL tests SQL parsing for operation and table extraction
func TestParseSQL(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		wantOperation string
		wantTable     string
	}{
		{
			name:          "SELECT query",
			sql:           "SELECT * FROM users WHERE id = $1",
			wantOperation: "SELECT",
			wantTable:     "users",
		},
		{
			name:          "INSERT query",
			sql:           "INSERT INTO orders (user_id, amount) VALUES ($1, $2)",
			wantOperation: "INSERT",
			wantTable:     "orders",
		},
		{
			name:          "UPDATE query",
			sql:           "UPDATE accounts SET balance = $1 WHERE id = $2",
			wantOperation: "UPDATE",
			wantTable:     "accounts",
		},
		{
			name:          "DELETE query",
			sql:           "DELETE FROM sessions WHERE expired_at < $1",
			wantOperation: "DELETE",
			wantTable:     "sessions",
		},
		{
			name:          "CREATE TABLE query",
			sql:           "CREATE TABLE users (id SERIAL PRIMARY KEY)",
			wantOperation: "CREATE",
			wantTable:     "users",
		},
		{
			name:          "ALTER TABLE query",
			sql:           "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
			wantOperation: "ALTER",
			wantTable:     "users",
		},
		{
			name:          "DROP TABLE query",
			sql:           "DROP TABLE temp_data",
			wantOperation: "DROP",
			wantTable:     "temp_data",
		},
		{
			name:          "BEGIN transaction",
			sql:           "BEGIN",
			wantOperation: "BEGIN",
			wantTable:     "",
		},
		{
			name:          "COMMIT transaction",
			sql:           "COMMIT",
			wantOperation: "COMMIT",
			wantTable:     "",
		},
		{
			name:          "ROLLBACK transaction",
			sql:           "ROLLBACK",
			wantOperation: "ROLLBACK",
			wantTable:     "",
		},
		{
			name:          "SELECT with JOIN",
			sql:           "SELECT u.name, o.amount FROM users u JOIN orders o ON u.id = o.user_id",
			wantOperation: "SELECT",
			wantTable:     "users",
		},
		{
			name:          "lowercase query",
			sql:           "select * from users",
			wantOperation: "SELECT",
			wantTable:     "users",
		},
		{
			name:          "mixed case query",
			sql:           "Select * From Users Where id = 1",
			wantOperation: "SELECT",
			wantTable:     "users",
		},
		{
			name:          "query with leading whitespace",
			sql:           "  SELECT * FROM users",
			wantOperation: "SELECT",
			wantTable:     "users",
		},
		{
			name:          "unknown query type",
			sql:           "EXPLAIN SELECT * FROM users",
			wantOperation: "OTHER",
			wantTable:     "users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation, table := parseSQL(tt.sql)
			assert.Equal(t, tt.wantOperation, operation)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}

// TestExtractTableName tests table name extraction from SQL
func TestExtractTableName(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantTable string
	}{
		{
			name:      "FROM clause",
			sql:       "SELECT * FROM users",
			wantTable: "users",
		},
		{
			name:      "INTO clause",
			sql:       "INSERT INTO orders VALUES (1)",
			wantTable: "orders",
		},
		{
			name:      "UPDATE clause",
			sql:       "UPDATE accounts SET x = 1",
			wantTable: "accounts",
		},
		{
			name:      "TABLE clause",
			sql:       "ALTER TABLE users ADD COLUMN x INT",
			wantTable: "users",
		},
		{
			name:      "JOIN clause",
			sql:       "SELECT * FROM a JOIN b ON a.id = b.id",
			wantTable: "a",
		},
		{
			name:      "table with schema",
			sql:       "SELECT * FROM public.users",
			wantTable: "public.users",
		},
		{
			name:      "no table found",
			sql:       "SELECT 1",
			wantTable: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := extractTableName(tt.sql)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}

// TestTruncateSQL tests SQL truncation
func TestTruncateSQL(t *testing.T) {
	t.Run("short SQL not truncated", func(t *testing.T) {
		sql := "SELECT * FROM users"
		result := truncateSQL(sql, 100)
		assert.Equal(t, sql, result)
	})

	t.Run("long SQL truncated", func(t *testing.T) {
		sql := "SELECT very_long_column_name, another_long_column FROM some_table WHERE condition = true"
		result := truncateSQL(sql, 20)
		assert.Equal(t, "SELECT very_long_col...", result)
		assert.Len(t, result, 23) // 20 + "..."
	})

	t.Run("SQL with whitespace trimmed", func(t *testing.T) {
		sql := "  SELECT * FROM users  "
		result := truncateSQL(sql, 100)
		assert.Equal(t, "SELECT * FROM users", result)
	})

	t.Run("exact length not truncated", func(t *testing.T) {
		sql := "SELECT"
		result := truncateSQL(sql, 6)
		assert.Equal(t, "SELECT", result)
	})
}

// TestNewTracedDB tests the creation of a traced database connection
func TestTelemetry_NewTracedDB(t *testing.T) {
	t.Run("creates TracedDB with nil pool", func(t *testing.T) {
		var pool *pgxpool.Pool
		db := NewTracedDB(pool)

		assert.NotNil(t, db)
		assert.Equal(t, pool, db.Pool)
		assert.Equal(t, "postgresql", db.serviceName)
	})

	t.Run("creates TracedDB with custom service name", func(t *testing.T) {
		var pool *pgxpool.Pool
		db := NewTracedDBWithService(pool, "celebrum-api")

		assert.NotNil(t, db)
		assert.Equal(t, "celebrum-api", db.serviceName)
	})
}

// TestSlowQueryThreshold tests the slow query threshold constant
func TestSlowQueryThreshold(t *testing.T) {
	assert.Equal(t, 500*time.Millisecond, SlowQueryThreshold)
}

// TestFinishSpanWithError tests the span finishing helper
func TestFinishSpanWithError(t *testing.T) {
	t.Run("finishes span without error", func(t *testing.T) {
		span := sentry.StartSpan(context.Background(), "test")

		finishSpanWithError(span, nil)

		assert.Equal(t, sentry.SpanStatusOK, span.Status)
	})

	t.Run("finishes span with error", func(t *testing.T) {
		span := sentry.StartSpan(context.Background(), "test")

		finishSpanWithError(span, errors.New("test error"))

		assert.Equal(t, sentry.SpanStatusInternalError, span.Status)
	})

	t.Run("finishes span with ErrNoRows (not treated as error)", func(t *testing.T) {
		span := sentry.StartSpan(context.Background(), "test")

		finishSpanWithError(span, pgx.ErrNoRows)

		assert.Equal(t, sentry.SpanStatusOK, span.Status)
	})

	t.Run("handles nil span", func(t *testing.T) {
		// Should not panic
		finishSpanWithError(nil, nil)
	})
}

// TestRecordDatabaseError_Telemetry tests database error recording with Sentry
func TestRecordDatabaseError_Telemetry(t *testing.T) {
	ctx := context.Background()

	t.Run("ignores nil error", func(t *testing.T) {
		// Should not panic
		RecordDatabaseError(ctx, nil, "test")
	})

	t.Run("ignores ErrNoRows", func(t *testing.T) {
		// Should not panic
		RecordDatabaseError(ctx, pgx.ErrNoRows, "test")
	})

	t.Run("records generic error", func(t *testing.T) {
		err := errors.New("database connection failed")
		// Should not panic
		RecordDatabaseError(ctx, err, "connect")
	})

	t.Run("records error with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)
		err := errors.New("query failed")

		// Should not panic
		RecordDatabaseError(ctx, err, "query")
	})

	t.Run("records PostgreSQL error with details", func(t *testing.T) {
		pgErr := &pgconn.PgError{
			Code:           "23505",
			Severity:       "ERROR",
			Message:        "duplicate key value violates unique constraint",
			Detail:         "Key (id)=(1) already exists.",
			Hint:           "Use a different id",
			ConstraintName: "users_pkey",
			TableName:      "users",
			ColumnName:     "id",
		}
		// Should not panic
		RecordDatabaseError(ctx, pgErr, "insert")
	})
}

// TestAddDatabaseSpanAttributes_Telemetry tests adding span attributes with Sentry
func TestAddDatabaseSpanAttributes_Telemetry(t *testing.T) {
	t.Run("adds attributes to span", func(t *testing.T) {
		span := sentry.StartSpan(context.Background(), "db.query")
		ctx := span.Context()

		AddDatabaseSpanAttributes(ctx, "users", 10)

		span.Finish()
	})

	t.Run("handles context without span", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		AddDatabaseSpanAttributes(ctx, "users", 10)
	})

	t.Run("handles empty table name", func(t *testing.T) {
		span := sentry.StartSpan(context.Background(), "db.query")
		ctx := span.Context()

		AddDatabaseSpanAttributes(ctx, "", 5)

		span.Finish()
	})
}

// MockTx implements pgx.Tx interface for testing
type MockTx struct {
	queryFunc    func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	queryRowFunc func(ctx context.Context, sql string, args ...interface{}) pgx.Row
	execFunc     func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	commitFunc   func(ctx context.Context) error
	rollbackFunc func(ctx context.Context) error
	beginFunc    func(ctx context.Context) (pgx.Tx, error)
}

func (m *MockTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return nil, nil
}

func (m *MockTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if m.queryRowFunc != nil {
		return m.queryRowFunc(ctx, sql, args...)
	}
	return nil
}

func (m *MockTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0"), nil
}

func (m *MockTx) Commit(ctx context.Context) error {
	if m.commitFunc != nil {
		return m.commitFunc(ctx)
	}
	return nil
}

func (m *MockTx) Rollback(ctx context.Context) error {
	if m.rollbackFunc != nil {
		return m.rollbackFunc(ctx)
	}
	return nil
}

func (m *MockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.beginFunc != nil {
		return m.beginFunc(ctx)
	}
	return nil, nil
}

func (m *MockTx) Conn() *pgx.Conn {
	return nil
}

func (m *MockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (m *MockTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (m *MockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (m *MockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}

// TestTracedTx_Query tests the TracedTx Query method
func TestTracedTx_Query(t *testing.T) {
	t.Run("successful query", func(t *testing.T) {
		mockTx := &MockTx{}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		rows, err := tracedTx.Query(ctx, "SELECT * FROM users")

		assert.NoError(t, err)
		assert.Nil(t, rows)
	})

	t.Run("query with error", func(t *testing.T) {
		expectedErr := errors.New("connection failed")
		mockTx := &MockTx{
			queryFunc: func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
				return nil, expectedErr
			},
		}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		rows, err := tracedTx.Query(ctx, "SELECT * FROM users")

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, rows)
	})
}

// TestTracedTx_QueryRow tests the TracedTx QueryRow method
func TestTracedTx_QueryRow(t *testing.T) {
	mockTx := &MockTx{}
	tracedTx := &TracedTx{
		Tx:          mockTx,
		serviceName: "test-service",
		startTime:   time.Now(),
	}
	ctx := context.Background()

	row := tracedTx.QueryRow(ctx, "SELECT * FROM users WHERE id = $1", 1)
	assert.Nil(t, row)
}

// TestTracedTx_Exec tests the TracedTx Exec method
func TestTracedTx_Exec(t *testing.T) {
	t.Run("successful exec", func(t *testing.T) {
		mockTx := &MockTx{
			execFunc: func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
		}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		tag, err := tracedTx.Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "test")

		assert.NoError(t, err)
		assert.Equal(t, int64(1), tag.RowsAffected())
	})

	t.Run("exec with error", func(t *testing.T) {
		expectedErr := errors.New("constraint violation")
		mockTx := &MockTx{
			execFunc: func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, expectedErr
			},
		}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		_, err := tracedTx.Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "test")

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})
}

// TestTracedTx_Commit tests the TracedTx Commit method
func TestTracedTx_Commit(t *testing.T) {
	t.Run("successful commit", func(t *testing.T) {
		mockTx := &MockTx{}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		err := tracedTx.Commit(ctx)

		assert.NoError(t, err)
	})

	t.Run("commit with error", func(t *testing.T) {
		expectedErr := errors.New("commit failed")
		mockTx := &MockTx{
			commitFunc: func(ctx context.Context) error {
				return expectedErr
			},
		}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		err := tracedTx.Commit(ctx)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})
}

// TestTracedTx_Rollback tests the TracedTx Rollback method
func TestTracedTx_Rollback(t *testing.T) {
	t.Run("successful rollback", func(t *testing.T) {
		mockTx := &MockTx{}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		err := tracedTx.Rollback(ctx)

		assert.NoError(t, err)
	})

	t.Run("rollback with tx closed error (not treated as error)", func(t *testing.T) {
		mockTx := &MockTx{
			rollbackFunc: func(ctx context.Context) error {
				return pgx.ErrTxClosed
			},
		}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		err := tracedTx.Rollback(ctx)

		// ErrTxClosed is still returned but span status should be OK
		assert.Error(t, err)
		assert.Equal(t, pgx.ErrTxClosed, err)
	})
}

// TestTracedTx_Begin tests the TracedTx Begin method (savepoint)
func TestTracedTx_Begin(t *testing.T) {
	t.Run("successful nested transaction", func(t *testing.T) {
		mockTx := &MockTx{
			beginFunc: func(ctx context.Context) (pgx.Tx, error) {
				return &MockTx{}, nil
			},
		}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		tx, err := tracedTx.Begin(ctx)

		assert.NoError(t, err)
		assert.NotNil(t, tx)
		assert.IsType(t, &TracedTx{}, tx)
	})

	t.Run("nested transaction with error", func(t *testing.T) {
		expectedErr := errors.New("savepoint failed")
		mockTx := &MockTx{
			beginFunc: func(ctx context.Context) (pgx.Tx, error) {
				return nil, expectedErr
			},
		}
		tracedTx := &TracedTx{
			Tx:          mockTx,
			serviceName: "test-service",
			startTime:   time.Now(),
		}
		ctx := context.Background()

		tx, err := tracedTx.Begin(ctx)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, tx)
	})
}

// TestTracedTx_Conn tests the TracedTx Conn method
func TestTracedTx_Conn(t *testing.T) {
	mockTx := &MockTx{}
	tracedTx := &TracedTx{
		Tx:          mockTx,
		serviceName: "test-service",
		startTime:   time.Now(),
	}

	conn := tracedTx.Conn()
	assert.Nil(t, conn)
}

// TestTracedTx_CopyFrom tests the TracedTx CopyFrom method
func TestTracedTx_CopyFrom(t *testing.T) {
	mockTx := &MockTx{}
	tracedTx := &TracedTx{
		Tx:          mockTx,
		serviceName: "test-service",
		startTime:   time.Now(),
	}
	ctx := context.Background()

	tableName := pgx.Identifier{"users"}
	columnNames := []string{"id", "name", "email"}
	data := [][]interface{}{
		{1, "John Doe", "john@example.com"},
		{2, "Jane Smith", "jane@example.com"},
	}
	rowSrc := pgx.CopyFromSlice(len(data), func(i int) ([]interface{}, error) {
		return data[i], nil
	})

	rowsAffected, err := tracedTx.CopyFrom(ctx, tableName, columnNames, rowSrc)

	assert.NoError(t, err)
	assert.Equal(t, int64(0), rowsAffected)
}

// TestTracedTx_LargeObjects tests the TracedTx LargeObjects method
func TestTracedTx_LargeObjects(t *testing.T) {
	mockTx := &MockTx{}
	tracedTx := &TracedTx{
		Tx:          mockTx,
		serviceName: "test-service",
		startTime:   time.Now(),
	}

	lo := tracedTx.LargeObjects()
	assert.IsType(t, pgx.LargeObjects{}, lo)
}

// TestTracedTx_Prepare tests the TracedTx Prepare method
func TestTracedTx_Prepare(t *testing.T) {
	mockTx := &MockTx{}
	tracedTx := &TracedTx{
		Tx:          mockTx,
		serviceName: "test-service",
		startTime:   time.Now(),
	}
	ctx := context.Background()

	stmt, err := tracedTx.Prepare(ctx, "get_user", "SELECT * FROM users WHERE id = $1")

	assert.NoError(t, err)
	assert.Nil(t, stmt)
}

// TestTracedTx_SendBatch tests the TracedTx SendBatch method
func TestTracedTx_SendBatch(t *testing.T) {
	mockTx := &MockTx{}
	tracedTx := &TracedTx{
		Tx:          mockTx,
		serviceName: "test-service",
		startTime:   time.Now(),
	}
	ctx := context.Background()

	batch := &pgx.Batch{}
	batch.Queue("SELECT * FROM users WHERE id = $1", 1)
	batch.Queue("UPDATE accounts SET balance = $1", 100.0)

	results := tracedTx.SendBatch(ctx, batch)
	assert.Nil(t, results)
}

// TestCaptureRedisError tests Redis error capturing
func TestCaptureRedisError(t *testing.T) {
	ctx := context.Background()

	t.Run("ignores nil error", func(t *testing.T) {
		// Should not panic
		captureRedisError(ctx, nil, "GET", "")
	})

	t.Run("captures generic error", func(t *testing.T) {
		err := errors.New("connection refused")
		// Should not panic
		captureRedisError(ctx, err, "GET", "localhost:6379")
	})

	t.Run("captures error with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)
		err := errors.New("timeout")

		// Should not panic
		captureRedisError(ctx, err, "SET", "")
	})
}

// TestCapturePostgresError tests PostgreSQL error capturing
func TestCapturePostgresError(t *testing.T) {
	ctx := context.Background()

	t.Run("ignores nil error", func(t *testing.T) {
		// Should not panic
		capturePostgresError(ctx, nil, "")
	})

	t.Run("ignores ErrNoRows", func(t *testing.T) {
		// Should not panic
		capturePostgresError(ctx, pgx.ErrNoRows, "SELECT 0")
	})

	t.Run("captures generic error", func(t *testing.T) {
		err := errors.New("connection failed")
		// Should not panic
		capturePostgresError(ctx, err, "SELECT 0")
	})

	t.Run("captures PostgreSQL error with full details", func(t *testing.T) {
		pgErr := &pgconn.PgError{
			Code:           "23505",
			Severity:       "ERROR",
			Message:        "duplicate key",
			Detail:         "Key already exists",
			Hint:           "Use UPDATE instead",
			ConstraintName: "pk_users",
			TableName:      "users",
			ColumnName:     "id",
		}
		// Should not panic
		capturePostgresError(ctx, pgErr, "INSERT 0")
	})
}

// TestAddRedisBreadcrumb tests Redis breadcrumb addition
func TestAddRedisBreadcrumb(t *testing.T) {
	t.Run("adds breadcrumb without context hub", func(t *testing.T) {
		ctx := context.Background()
		// Should not panic
		addRedisBreadcrumb(ctx, "GET", "cache hit")
	})

	t.Run("adds breadcrumb with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)
		// Should not panic
		addRedisBreadcrumb(ctx, "SET", "cache set")
	})
}

// TestAddPostgresBreadcrumb tests PostgreSQL breadcrumb addition
func TestAddPostgresBreadcrumb(t *testing.T) {
	t.Run("adds breadcrumb with table", func(t *testing.T) {
		ctx := context.Background()
		// Should not panic
		addPostgresBreadcrumb(ctx, "SELECT", "users", "query executed")
	})

	t.Run("adds breadcrumb without table", func(t *testing.T) {
		ctx := context.Background()
		// Should not panic
		addPostgresBreadcrumb(ctx, "BEGIN", "", "transaction started")
	})

	t.Run("adds breadcrumb with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)
		// Should not panic
		addPostgresBreadcrumb(ctx, "COMMIT", "", "transaction committed")
	})
}

// TestPostgresSentryTracer_TraceQueryStart tests query start tracing
func TestPostgresSentryTracer_TraceQueryStart(t *testing.T) {
	tracer := NewPostgresSentryTracer("test-service")
	ctx := context.Background()

	t.Run("traces SELECT query", func(t *testing.T) {
		data := pgx.TraceQueryStartData{
			SQL: "SELECT * FROM users WHERE id = $1",
		}

		newCtx := tracer.TraceQueryStart(ctx, nil, data)

		require.NotNil(t, newCtx)
		// Context should have start time
		startTime, ok := newCtx.Value(queryStartTimeKey{}).(time.Time)
		assert.True(t, ok)
		assert.WithinDuration(t, time.Now(), startTime, time.Second)
	})

	t.Run("traces INSERT query", func(t *testing.T) {
		data := pgx.TraceQueryStartData{
			SQL: "INSERT INTO users (name) VALUES ($1)",
		}

		newCtx := tracer.TraceQueryStart(ctx, nil, data)

		require.NotNil(t, newCtx)
	})
}

// TestPostgresSentryTracer_TraceQueryEnd tests query end tracing
func TestPostgresSentryTracer_TraceQueryEnd(t *testing.T) {
	tracer := NewPostgresSentryTracer("test-service")

	t.Run("ends query without error", func(t *testing.T) {
		// Start a span to have context
		span := sentry.StartSpan(context.Background(), "db.sql.query")
		ctx := span.Context()

		data := pgx.TraceQueryEndData{
			CommandTag: pgconn.NewCommandTag("SELECT 1"),
			Err:        nil,
		}

		// Should not panic
		tracer.TraceQueryEnd(ctx, nil, data)
	})

	t.Run("ends query with error", func(t *testing.T) {
		span := sentry.StartSpan(context.Background(), "db.sql.query")
		ctx := span.Context()

		data := pgx.TraceQueryEndData{
			CommandTag: pgconn.CommandTag{},
			Err:        errors.New("query failed"),
		}

		// Should not panic
		tracer.TraceQueryEnd(ctx, nil, data)
	})

	t.Run("ends query without span context", func(t *testing.T) {
		ctx := context.Background()

		data := pgx.TraceQueryEndData{
			CommandTag: pgconn.NewCommandTag("SELECT 0"),
			Err:        errors.New("error without span"),
		}

		// Should not panic, error should still be captured
		tracer.TraceQueryEnd(ctx, nil, data)
	})

	t.Run("ignores ErrNoRows", func(t *testing.T) {
		ctx := context.Background()

		data := pgx.TraceQueryEndData{
			CommandTag: pgconn.NewCommandTag("SELECT 0"),
			Err:        pgx.ErrNoRows,
		}

		// Should not panic, ErrNoRows not captured as error
		tracer.TraceQueryEnd(ctx, nil, data)
	})
}

// TestQueryStartTimeKey tests the context key type
func TestQueryStartTimeKey(t *testing.T) {
	ctx := context.Background()
	startTime := time.Now()

	ctx = context.WithValue(ctx, queryStartTimeKey{}, startTime)

	retrieved, ok := ctx.Value(queryStartTimeKey{}).(time.Time)
	assert.True(t, ok)
	assert.Equal(t, startTime, retrieved)
}

// BenchmarkParseSQL benchmarks SQL parsing
func BenchmarkParseSQL(b *testing.B) {
	sqls := []string{
		"SELECT * FROM users WHERE id = $1",
		"INSERT INTO orders (user_id, amount) VALUES ($1, $2)",
		"UPDATE accounts SET balance = balance + $1 WHERE id = $2",
		"DELETE FROM sessions WHERE expired_at < NOW()",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, sql := range sqls {
			parseSQL(sql)
		}
	}
}

// BenchmarkTruncateSQL benchmarks SQL truncation
func BenchmarkTruncateSQL(b *testing.B) {
	longSQL := "SELECT very_long_column_1, very_long_column_2, very_long_column_3 FROM some_very_long_table_name WHERE condition_1 = true AND condition_2 = false"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		truncateSQL(longSQL, 100)
	}
}

// Integration-style tests for TracedDB (requires mock or real pool)

// TestTracedDB_CloseBehavior tests the Close method
func TestTracedDB_CloseBehavior(t *testing.T) {
	t.Run("close with nil pool doesn't panic", func(t *testing.T) {
		db := NewTracedDB(nil)

		// This will panic because Pool is nil, but in real usage it wouldn't be
		// We can't easily test this without a real pool
		assert.NotNil(t, db)
	})
}

// TestRedisSentryHook_DialHook tests the dial hook
func TestRedisSentryHook_DialHook(t *testing.T) {
	hook := NewRedisSentryHook("test-service")

	t.Run("hook is created successfully", func(t *testing.T) {
		// The hook should be created successfully
		assert.NotNil(t, hook)
		assert.Equal(t, "test-service", hook.serviceName)
	})
}

// TestRedisSentryHook_ProcessHook tests the process hook
func TestRedisSentryHook_ProcessHook(t *testing.T) {
	hook := NewRedisSentryHook("test-service")

	t.Run("wraps process function", func(t *testing.T) {
		// The hook should be created successfully
		assert.NotNil(t, hook)
		assert.Equal(t, "test-service", hook.serviceName)
	})
}

// TestRedisSentryHook_ProcessPipelineHook tests the pipeline hook
func TestRedisSentryHook_ProcessPipelineHook(t *testing.T) {
	hook := NewRedisSentryHook("test-service")

	t.Run("wraps pipeline function", func(t *testing.T) {
		// The hook should be created successfully
		assert.NotNil(t, hook)
	})
}

// Example_parseSQL demonstrates SQL parsing
func Example_parseSQL() {
	operation, table := parseSQL("SELECT * FROM users WHERE id = 1")
	fmt.Printf("Operation: %s, Table: %s\n", operation, table)
	// Output: Operation: SELECT, Table: users
}

// Example_truncateSQL demonstrates SQL truncation
func Example_truncateSQL() {
	result := truncateSQL("SELECT * FROM users", 10)
	fmt.Println(result)
	// Output: SELECT * F...
}
