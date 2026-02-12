package database

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Context key for query start time
type queryStartTimeKey struct{}

// RedisSentryHook implements redis.Hook for Sentry error tracking and tracing.
type RedisSentryHook struct {
	serviceName string
}

// NewRedisSentryHook creates a new Redis Sentry hook.
func NewRedisSentryHook(serviceName string) *RedisSentryHook {
	if serviceName == "" {
		serviceName = "redis"
	}
	return &RedisSentryHook{serviceName: serviceName}
}

// DialHook is called when a new connection is established.
func (h *RedisSentryHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		span := sentry.StartSpan(ctx, "db.redis.dial")
		span.Description = fmt.Sprintf("Redis dial %s", addr)
		span.SetTag("db.system", "redis")
		span.SetTag("net.peer.name", addr)
		span.SetTag("net.transport", network)
		defer span.Finish()

		conn, err := next(ctx, network, addr)
		if err != nil {
			span.Status = sentry.SpanStatusInternalError
			span.SetTag("error", "true")
			captureRedisError(ctx, err, "dial", addr)
		} else {
			span.Status = sentry.SpanStatusOK
		}
		return conn, err
	}
}

// ProcessHook is called before processing a command.
func (h *RedisSentryHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		cmdName := cmd.Name()

		// Start span for the Redis command
		span := sentry.StartSpan(ctx, "db.redis")
		span.Description = cmdName
		span.SetTag("db.system", "redis")
		span.SetTag("db.operation", cmdName)
		span.SetTag("service", h.serviceName)

		// Add breadcrumb for debugging
		addRedisBreadcrumb(ctx, cmdName, "start")

		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start)

		span.SetData("db.duration_ms", duration.Milliseconds())

		if err != nil && err != redis.Nil {
			span.Status = sentry.SpanStatusInternalError
			span.SetTag("error", "true")
			span.SetData("error.message", err.Error())
			captureRedisError(ctx, err, cmdName, "")
		} else {
			span.Status = sentry.SpanStatusOK
		}

		span.Finish()

		// Track slow queries (>100ms)
		if duration > 100*time.Millisecond {
			addRedisBreadcrumb(ctx, cmdName, fmt.Sprintf("slow query: %dms", duration.Milliseconds()))
		}

		return err
	}
}

// ProcessPipelineHook is called before processing a pipeline.
func (h *RedisSentryHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		span := sentry.StartSpan(ctx, "db.redis.pipeline")
		span.Description = fmt.Sprintf("Redis pipeline (%d commands)", len(cmds))
		span.SetTag("db.system", "redis")
		span.SetTag("db.operation", "pipeline")
		span.SetData("db.pipeline_size", len(cmds))

		// Collect command names for context
		cmdNames := make([]string, 0, len(cmds))
		for _, cmd := range cmds {
			cmdNames = append(cmdNames, cmd.Name())
		}
		span.SetData("db.commands", cmdNames)

		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start)

		span.SetData("db.duration_ms", duration.Milliseconds())

		if err != nil && err != redis.Nil {
			span.Status = sentry.SpanStatusInternalError
			span.SetTag("error", "true")
			captureRedisError(ctx, err, "pipeline", "")
		} else {
			span.Status = sentry.SpanStatusOK
		}

		span.Finish()
		return err
	}
}

// captureRedisError captures a Redis error with context.
func captureRedisError(ctx context.Context, err error, operation string, addr string) {
	if err == nil || err == redis.Nil {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("db.system", "redis")
		scope.SetTag("db.operation", operation)
		scope.SetLevel(sentry.LevelError)
		if addr != "" {
			scope.SetTag("net.peer.name", addr)
		}
		scope.SetExtra("error_type", fmt.Sprintf("%T", err))
		hub.CaptureException(err)
	})
}

// addRedisBreadcrumb adds a breadcrumb for Redis operations.
func addRedisBreadcrumb(ctx context.Context, operation string, message string) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}

	hub.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  "redis",
		Message:   fmt.Sprintf("%s: %s", operation, message),
		Level:     sentry.LevelInfo,
		Timestamp: time.Now(),
	}, nil)
}

// PostgresSentryTracer implements pgx.QueryTracer for Sentry error tracking and tracing.
type PostgresSentryTracer struct {
	serviceName string
}

// NewPostgresSentryTracer creates a new PostgreSQL Sentry tracer.
func NewPostgresSentryTracer(serviceName string) *PostgresSentryTracer {
	if serviceName == "" {
		serviceName = "postgresql"
	}
	return &PostgresSentryTracer{serviceName: serviceName}
}

// TraceQueryStart is called at the beginning of a query execution.
func (t *PostgresSentryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	// Extract operation type and table from SQL
	operation, table := parseSQL(data.SQL)

	span := sentry.StartSpan(ctx, "db.sql.query")
	span.Description = truncateSQL(data.SQL, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", operation)
	if table != "" {
		span.SetTag("db.table", table)
	}
	span.SetTag("service", t.serviceName)

	// Store span in context for TraceQueryEnd
	ctx = context.WithValue(span.Context(), queryStartTimeKey{}, time.Now())

	// Add breadcrumb
	addPostgresBreadcrumb(ctx, operation, table, "start")

	return ctx
}

// TraceQueryEnd is called after a query execution.
func (t *PostgresSentryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	// Get the span from context
	span := sentry.SpanFromContext(ctx)
	if span == nil {
		// If no span, still capture errors
		if data.Err != nil && data.Err != pgx.ErrNoRows {
			capturePostgresError(ctx, data.Err, data.CommandTag.String())
		}
		return
	}

	// Calculate duration if start time was stored
	if startTime, ok := ctx.Value(queryStartTimeKey{}).(time.Time); ok {
		duration := time.Since(startTime)
		span.SetData("db.duration_ms", duration.Milliseconds())
		span.SetData("db.rows_affected", data.CommandTag.RowsAffected())

		// Track slow queries (>500ms)
		if duration > 500*time.Millisecond {
			operation, table := parseSQL(truncateSQL(span.Description, 100))
			addPostgresBreadcrumb(ctx, operation, table, fmt.Sprintf("slow query: %dms", duration.Milliseconds()))
		}
	}

	if data.Err != nil && data.Err != pgx.ErrNoRows {
		span.Status = sentry.SpanStatusInternalError
		span.SetTag("error", "true")
		span.SetData("error.message", data.Err.Error())
		capturePostgresError(ctx, data.Err, data.CommandTag.String())
	} else {
		span.Status = sentry.SpanStatusOK
	}

	span.Finish()
}

// capturePostgresError captures a PostgreSQL error with context.
func capturePostgresError(ctx context.Context, err error, commandTag string) {
	if err == nil || err == pgx.ErrNoRows {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("db.system", "postgresql")
		scope.SetLevel(sentry.LevelError)
		scope.SetExtra("command_tag", commandTag)
		scope.SetExtra("error_type", fmt.Sprintf("%T", err))

		// Extract PostgreSQL error code if available
		if pgErr, ok := err.(*pgconn.PgError); ok {
			scope.SetTag("pg.code", pgErr.Code)
			scope.SetTag("pg.severity", pgErr.Severity)
			scope.SetExtra("pg.message", pgErr.Message)
			scope.SetExtra("pg.detail", pgErr.Detail)
			scope.SetExtra("pg.hint", pgErr.Hint)
			scope.SetExtra("pg.constraint", pgErr.ConstraintName)
			scope.SetExtra("pg.table", pgErr.TableName)
			scope.SetExtra("pg.column", pgErr.ColumnName)
		}

		hub.CaptureException(err)
	})
}

// addPostgresBreadcrumb adds a breadcrumb for PostgreSQL operations.
func addPostgresBreadcrumb(ctx context.Context, operation string, table string, message string) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}

	data := map[string]interface{}{
		"operation": operation,
	}
	if table != "" {
		data["table"] = table
	}

	hub.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  "postgresql",
		Message:   message,
		Level:     sentry.LevelInfo,
		Data:      data,
		Timestamp: time.Now(),
	}, nil)
}

// parseSQL extracts operation type and table name from SQL.
func parseSQL(sql string) (operation string, table string) {
	sql = strings.TrimSpace(strings.ToUpper(sql))

	// Determine operation type
	switch {
	case strings.HasPrefix(sql, "SELECT"):
		operation = "SELECT"
	case strings.HasPrefix(sql, "INSERT"):
		operation = "INSERT"
	case strings.HasPrefix(sql, "UPDATE"):
		operation = "UPDATE"
	case strings.HasPrefix(sql, "DELETE"):
		operation = "DELETE"
	case strings.HasPrefix(sql, "CREATE"):
		operation = "CREATE"
	case strings.HasPrefix(sql, "ALTER"):
		operation = "ALTER"
	case strings.HasPrefix(sql, "DROP"):
		operation = "DROP"
	case strings.HasPrefix(sql, "BEGIN"):
		operation = "BEGIN"
	case strings.HasPrefix(sql, "COMMIT"):
		operation = "COMMIT"
	case strings.HasPrefix(sql, "ROLLBACK"):
		operation = "ROLLBACK"
	default:
		operation = "OTHER"
	}

	// Try to extract table name
	table = extractTableName(sql)
	return
}

// extractTableName attempts to extract the table name from SQL.
func extractTableName(sql string) string {
	sql = strings.ToUpper(sql)

	// Common patterns for table extraction
	patterns := []struct {
		prefix string
		offset int
	}{
		{"FROM ", 5},
		{"INTO ", 5},
		{"UPDATE ", 7},
		{"TABLE ", 6},
		{"JOIN ", 5},
	}

	for _, p := range patterns {
		if idx := strings.Index(sql, p.prefix); idx != -1 {
			rest := sql[idx+p.offset:]
			// Extract until space, newline, or parenthesis
			end := strings.IndexAny(rest, " \t\n()")
			if end == -1 {
				end = len(rest)
			}
			if end > 0 {
				return strings.ToLower(strings.TrimSpace(rest[:end]))
			}
		}
	}
	return ""
}

// truncateSQL truncates SQL to a maximum length for display.
func truncateSQL(sql string, maxLen int) string {
	sql = strings.TrimSpace(sql)
	if len(sql) <= maxLen {
		return sql
	}
	return sql[:maxLen] + "..."
}

// SlowQueryThreshold defines the duration after which a query is considered slow.
const SlowQueryThreshold = 500 * time.Millisecond

// TracedDB wraps a database connection with Sentry tracing capabilities.
type TracedDB struct {
	Pool        *pgxpool.Pool
	serviceName string
}

// NewTracedDB creates a new traced database connection.
//
// Parameters:
//
//	pool: Database pool.
//
// Returns:
//
//	*TracedDB: Traced database wrapper.
func NewTracedDB(pool *pgxpool.Pool) *TracedDB {
	return &TracedDB{
		Pool:        pool,
		serviceName: "postgresql",
	}
}

// NewTracedDBWithService creates a new traced database with a custom service name.
func NewTracedDBWithService(pool *pgxpool.Pool, serviceName string) *TracedDB {
	return &TracedDB{
		Pool:        pool,
		serviceName: serviceName,
	}
}

// Query executes a query with Sentry tracing.
func (db *TracedDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	operation, table := parseSQL(sql)
	span := sentry.StartSpan(ctx, "db.sql.query")
	span.Description = truncateSQL(sql, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", operation)
	span.SetTag("service", db.serviceName)
	if table != "" {
		span.SetTag("db.table", table)
	}

	start := time.Now()
	rows, err := db.Pool.Query(ctx, sql, args...)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	finishSpanWithError(span, err)

	if duration > SlowQueryThreshold {
		addPostgresBreadcrumb(ctx, operation, table, fmt.Sprintf("slow query: %dms", duration.Milliseconds()))
	}

	return rows, err
}

// QueryRow executes a query that returns a single row with Sentry tracing.
func (db *TracedDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	operation, table := parseSQL(sql)
	span := sentry.StartSpan(ctx, "db.sql.query")
	span.Description = truncateSQL(sql, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", operation)
	span.SetTag("service", db.serviceName)
	if table != "" {
		span.SetTag("db.table", table)
	}

	start := time.Now()
	row := db.Pool.QueryRow(ctx, sql, args...)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.Status = sentry.SpanStatusOK
	span.Finish()

	if duration > SlowQueryThreshold {
		addPostgresBreadcrumb(ctx, operation, table, fmt.Sprintf("slow query: %dms", duration.Milliseconds()))
	}

	return row
}

// Exec executes a query without returning rows with Sentry tracing.
func (db *TracedDB) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	operation, table := parseSQL(sql)
	span := sentry.StartSpan(ctx, "db.sql.exec")
	span.Description = truncateSQL(sql, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", operation)
	span.SetTag("service", db.serviceName)
	if table != "" {
		span.SetTag("db.table", table)
	}

	start := time.Now()
	tag, err := db.Pool.Exec(ctx, sql, arguments...)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.SetData("db.rows_affected", tag.RowsAffected())
	finishSpanWithError(span, err)

	if duration > SlowQueryThreshold {
		addPostgresBreadcrumb(ctx, operation, table, fmt.Sprintf("slow exec: %dms, rows: %d", duration.Milliseconds(), tag.RowsAffected()))
	}

	return tag, err
}

// Begin starts a transaction with Sentry tracing.
func (db *TracedDB) Begin(ctx context.Context) (pgx.Tx, error) {
	span := sentry.StartSpan(ctx, "db.sql.transaction")
	span.Description = "BEGIN"
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "BEGIN")
	span.SetTag("service", db.serviceName)

	start := time.Now()
	tx, err := db.Pool.Begin(ctx)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	finishSpanWithError(span, err)

	if err != nil {
		return nil, err
	}

	addPostgresBreadcrumb(ctx, "BEGIN", "", "transaction started")
	return &TracedTx{Tx: tx, serviceName: db.serviceName, startTime: start}, nil
}

// BeginTx starts a transaction with options and Sentry tracing.
func (db *TracedDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	span := sentry.StartSpan(ctx, "db.sql.transaction")
	span.Description = "BEGIN"
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "BEGIN")
	span.SetTag("service", db.serviceName)
	span.SetData("tx.isolation_level", string(txOptions.IsoLevel))
	span.SetData("tx.access_mode", string(txOptions.AccessMode))

	start := time.Now()
	tx, err := db.Pool.BeginTx(ctx, txOptions)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	finishSpanWithError(span, err)

	if err != nil {
		return nil, err
	}

	addPostgresBreadcrumb(ctx, "BEGIN", "", "transaction started with options")
	return &TracedTx{Tx: tx, serviceName: db.serviceName, startTime: start}, nil
}

// Ping verifies the connection to the database with Sentry tracing.
func (db *TracedDB) Ping(ctx context.Context) error {
	span := sentry.StartSpan(ctx, "db.sql.ping")
	span.Description = "PING"
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "PING")
	span.SetTag("service", db.serviceName)

	start := time.Now()
	err := db.Pool.Ping(ctx)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	finishSpanWithError(span, err)

	return err
}

// Close closes the database connection pool.
func (db *TracedDB) Close() {
	db.Pool.Close()
}

// TracedTx wraps a database transaction with Sentry tracing.
type TracedTx struct {
	Tx          pgx.Tx
	serviceName string
	startTime   time.Time
}

// Query executes a query within the transaction with Sentry tracing.
func (tx *TracedTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	operation, table := parseSQL(sql)
	span := sentry.StartSpan(ctx, "db.sql.query")
	span.Description = truncateSQL(sql, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", operation)
	span.SetTag("service", tx.serviceName)
	span.SetTag("db.in_transaction", "true")
	if table != "" {
		span.SetTag("db.table", table)
	}

	start := time.Now()
	rows, err := tx.Tx.Query(ctx, sql, args...)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	finishSpanWithError(span, err)

	return rows, err
}

// QueryRow executes a query that returns a single row within the transaction.
func (tx *TracedTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	operation, table := parseSQL(sql)
	span := sentry.StartSpan(ctx, "db.sql.query")
	span.Description = truncateSQL(sql, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", operation)
	span.SetTag("service", tx.serviceName)
	span.SetTag("db.in_transaction", "true")
	if table != "" {
		span.SetTag("db.table", table)
	}

	start := time.Now()
	row := tx.Tx.QueryRow(ctx, sql, args...)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.Status = sentry.SpanStatusOK
	span.Finish()

	return row
}

// Exec executes a query without returning rows within the transaction.
func (tx *TracedTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	operation, table := parseSQL(sql)
	span := sentry.StartSpan(ctx, "db.sql.exec")
	span.Description = truncateSQL(sql, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", operation)
	span.SetTag("service", tx.serviceName)
	span.SetTag("db.in_transaction", "true")
	if table != "" {
		span.SetTag("db.table", table)
	}

	start := time.Now()
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.SetData("db.rows_affected", tag.RowsAffected())
	finishSpanWithError(span, err)

	return tag, err
}

// Commit commits the transaction with Sentry tracing.
func (tx *TracedTx) Commit(ctx context.Context) error {
	span := sentry.StartSpan(ctx, "db.sql.commit")
	span.Description = "COMMIT"
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "COMMIT")
	span.SetTag("service", tx.serviceName)

	start := time.Now()
	err := tx.Tx.Commit(ctx)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.SetData("tx.total_duration_ms", time.Since(tx.startTime).Milliseconds())
	finishSpanWithError(span, err)

	addPostgresBreadcrumb(ctx, "COMMIT", "", fmt.Sprintf("transaction committed, total: %dms", time.Since(tx.startTime).Milliseconds()))

	return err
}

// Rollback rolls back the transaction with Sentry tracing.
func (tx *TracedTx) Rollback(ctx context.Context) error {
	span := sentry.StartSpan(ctx, "db.sql.rollback")
	span.Description = "ROLLBACK"
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "ROLLBACK")
	span.SetTag("service", tx.serviceName)

	start := time.Now()
	err := tx.Tx.Rollback(ctx)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.SetData("tx.total_duration_ms", time.Since(tx.startTime).Milliseconds())

	// Don't treat "tx is closed" as an error for rollback
	if err != nil && err != pgx.ErrTxClosed {
		finishSpanWithError(span, err)
	} else {
		span.Status = sentry.SpanStatusOK
		span.Finish()
	}

	addPostgresBreadcrumb(ctx, "ROLLBACK", "", fmt.Sprintf("transaction rolled back, total: %dms", time.Since(tx.startTime).Milliseconds()))

	return err
}

// Begin starts a nested transaction (savepoint) with Sentry tracing.
func (tx *TracedTx) Begin(ctx context.Context) (pgx.Tx, error) {
	span := sentry.StartSpan(ctx, "db.sql.savepoint")
	span.Description = "SAVEPOINT"
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "SAVEPOINT")
	span.SetTag("service", tx.serviceName)

	start := time.Now()
	nestedTx, err := tx.Tx.Begin(ctx)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	finishSpanWithError(span, err)

	if err != nil {
		return nil, err
	}

	return &TracedTx{Tx: nestedTx, serviceName: tx.serviceName, startTime: start}, nil
}

// Conn returns the underlying connection.
func (tx *TracedTx) Conn() *pgx.Conn {
	return tx.Tx.Conn()
}

// CopyFrom copies data from a source to a destination table with Sentry tracing.
func (tx *TracedTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	span := sentry.StartSpan(ctx, "db.sql.copy")
	span.Description = fmt.Sprintf("COPY %v", tableName)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "COPY")
	span.SetTag("service", tx.serviceName)
	span.SetTag("db.table", strings.Join(tableName, "."))
	span.SetData("db.columns", columnNames)

	start := time.Now()
	rowsAffected, err := tx.Tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.SetData("db.rows_affected", rowsAffected)
	finishSpanWithError(span, err)

	return rowsAffected, err
}

// LargeObjects returns the large object manager.
func (tx *TracedTx) LargeObjects() pgx.LargeObjects {
	return tx.Tx.LargeObjects()
}

// Prepare prepares a statement with Sentry tracing.
func (tx *TracedTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	span := sentry.StartSpan(ctx, "db.sql.prepare")
	span.Description = truncateSQL(sql, 200)
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "PREPARE")
	span.SetTag("service", tx.serviceName)
	span.SetData("db.statement_name", name)

	start := time.Now()
	stmt, err := tx.Tx.Prepare(ctx, name, sql)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	finishSpanWithError(span, err)

	return stmt, err
}

// SendBatch sends a batch of queries with Sentry tracing.
func (tx *TracedTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	span := sentry.StartSpan(ctx, "db.sql.batch")
	span.Description = fmt.Sprintf("Batch (%d queries)", b.Len())
	span.SetTag("db.system", "postgresql")
	span.SetTag("db.operation", "BATCH")
	span.SetTag("service", tx.serviceName)
	span.SetData("db.batch_size", b.Len())

	start := time.Now()
	results := tx.Tx.SendBatch(ctx, b)
	duration := time.Since(start)

	span.SetData("db.duration_ms", duration.Milliseconds())
	span.Status = sentry.SpanStatusOK
	span.Finish()

	return results
}

// finishSpanWithError finishes a span and sets error status if needed.
func finishSpanWithError(span *sentry.Span, err error) {
	if span == nil {
		return
	}

	if err != nil && err != pgx.ErrNoRows {
		span.Status = sentry.SpanStatusInternalError
		span.SetTag("error", "true")
		span.SetData("error.message", err.Error())
	} else {
		span.Status = sentry.SpanStatusOK
	}

	span.Finish()
}

// RecordDatabaseError records a database error to Sentry with full context.
//
// Parameters:
//
//	ctx: Context.
//	err: Error.
//	operation: Operation name.
func RecordDatabaseError(ctx context.Context, err error, operation string) {
	if err == nil || err == pgx.ErrNoRows {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("db.system", "postgresql")
		scope.SetTag("db.operation", operation)
		scope.SetLevel(sentry.LevelError)
		scope.SetExtra("error_type", fmt.Sprintf("%T", err))

		// Extract PostgreSQL error details if available
		if pgErr, ok := err.(*pgconn.PgError); ok {
			scope.SetTag("pg.code", pgErr.Code)
			scope.SetTag("pg.severity", pgErr.Severity)
			scope.SetExtra("pg.message", pgErr.Message)
			scope.SetExtra("pg.detail", pgErr.Detail)
			scope.SetExtra("pg.hint", pgErr.Hint)
		}

		hub.CaptureException(err)
	})
}

// AddDatabaseSpanAttributes adds database-specific attributes to the current span.
//
// Parameters:
//
//	ctx: Context.
//	table: Table name.
//	rowsAffected: Rows affected.
func AddDatabaseSpanAttributes(ctx context.Context, table string, rowsAffected int64) {
	span := sentry.SpanFromContext(ctx)
	if span == nil {
		return
	}

	if table != "" {
		span.SetTag("db.table", table)
	}
	span.SetData("db.rows_affected", rowsAffected)
}
