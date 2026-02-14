package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// SlowQueryLog represents a slow query log entry.
type SlowQueryLog struct {
	ID           int64                  `json:"id"`
	QueryText    string                 `json:"query_text"`
	QueryHash    string                 `json:"query_hash"`
	Operation    string                 `json:"operation"`
	TableName    string                 `json:"table_name"`
	DurationMs   int64                  `json:"duration_ms"`
	RowsAffected int64                  `json:"rows_affected"`
	ServiceName  string                 `json:"service_name"`
	Context      map[string]interface{} `json:"context"`
	CreatedAt    time.Time              `json:"created_at"`
}

// SlowQueryLogger handles logging slow queries to the database.
type SlowQueryLogger struct {
	db          DBPool
	serviceName string
	thresholdMs int64
}

// NewSlowQueryLogger creates a new slow query logger.
func NewSlowQueryLogger(db DBPool, serviceName string, thresholdMs int64) *SlowQueryLogger {
	if thresholdMs <= 0 {
		thresholdMs = 1000 // Default 1 second threshold for DB logging
	}
	if serviceName == "" {
		serviceName = "postgresql"
	}
	return &SlowQueryLogger{
		db:          db,
		serviceName: serviceName,
		thresholdMs: thresholdMs,
	}
}

// LogSlowQuery logs a slow query to the database.
func (l *SlowQueryLogger) LogSlowQuery(ctx context.Context, query string, durationMs int64, rowsAffected int64, context map[string]interface{}) error {
	if durationMs < l.thresholdMs {
		return nil // Skip queries below threshold
	}

	operation, tableName := parseSQL(query)
	queryHash := hashQuery(query)

	logEntry := SlowQueryLog{
		QueryText:    truncateSQL(query, 2000),
		QueryHash:    queryHash,
		Operation:    operation,
		TableName:    tableName,
		DurationMs:   durationMs,
		RowsAffected: rowsAffected,
		ServiceName:  l.serviceName,
		Context:      context,
		CreatedAt:    time.Now(),
	}

	// Build the query dynamically based on available columns
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`
		INSERT INTO slow_query_log 
		(query_text, query_hash, operation, table_name, duration_ms, rows_affected, service_name, context, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)

	// Serialize context to JSON for JSONB column
	var contextJSON []byte
	if logEntry.Context != nil {
		var err error
		contextJSON, err = json.Marshal(logEntry.Context)
		if err != nil {
			contextJSON = []byte("{}")
		}
	} else {
		contextJSON = []byte("{}")
	}

	_, err := l.db.Exec(ctx, queryBuilder.String(),
		logEntry.QueryText,
		logEntry.QueryHash,
		logEntry.Operation,
		logEntry.TableName,
		logEntry.DurationMs,
		logEntry.RowsAffected,
		logEntry.ServiceName,
		contextJSON,
		logEntry.CreatedAt,
	)

	if err != nil {
		// Log error but don't fail the main operation
		fmt.Printf("Failed to log slow query: %v\n", err)
	}

	return nil
}

// GetSlowQueries retrieves slow queries from the database.
func (l *SlowQueryLogger) GetSlowQueries(ctx context.Context, limit int, since time.Time) ([]SlowQueryLog, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, query_text, query_hash, operation, table_name, duration_ms, 
		       rows_affected, service_name, context, created_at
		FROM slow_query_log
		WHERE created_at > $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := l.db.Query(ctx, query, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []SlowQueryLog
	for rows.Next() {
		var log SlowQueryLog
		err := rows.Scan(
			&log.ID,
			&log.QueryText,
			&log.QueryHash,
			&log.Operation,
			&log.TableName,
			&log.DurationMs,
			&log.RowsAffected,
			&log.ServiceName,
			&log.Context,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, rows.Err()
}

// GetSlowQueryStats returns statistics about slow queries.
func (l *SlowQueryLogger) GetSlowQueryStats(ctx context.Context, since time.Time) (map[string]interface{}, error) {
	query := `
		SELECT 
			COUNT(*) as total_slow_queries,
			AVG(duration_ms) as avg_duration_ms,
			MAX(duration_ms) as max_duration_ms,
			COUNT(DISTINCT table_name) as unique_tables,
			COUNT(DISTINCT query_hash) as unique_queries
		FROM slow_query_log
		WHERE created_at > $1
	`

	row := l.db.QueryRow(ctx, query, since)

	var stats struct {
		TotalSlowQueries int64   `json:"total_slow_queries"`
		AvgDurationMs    float64 `json:"avg_duration_ms"`
		MaxDurationMs    int64   `json:"max_duration_ms"`
		UniqueTables     int64   `json:"unique_tables"`
		UniqueQueries    int64   `json:"unique_queries"`
	}

	err := row.Scan(
		&stats.TotalSlowQueries,
		&stats.AvgDurationMs,
		&stats.MaxDurationMs,
		&stats.UniqueTables,
		&stats.UniqueQueries,
	)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	return map[string]interface{}{
		"total_slow_queries": stats.TotalSlowQueries,
		"avg_duration_ms":    stats.AvgDurationMs,
		"max_duration_ms":    stats.MaxDurationMs,
		"unique_tables":      stats.UniqueTables,
		"unique_queries":     stats.UniqueQueries,
		"since":              since,
	}, nil
}

// CleanupOldLogs removes old slow query logs.
func (l *SlowQueryLogger) CleanupOldLogs(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}

	query := `DELETE FROM slow_query_log WHERE created_at < NOW() - INTERVAL '1 day' * $1`
	result, err := l.db.Exec(ctx, query, retentionDays)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// hashQuery creates a hash of the query for aggregation.
func hashQuery(query string) string {
	// Normalize query: remove extra spaces and lowercase
	normalized := strings.ToLower(strings.TrimSpace(query))
	normalized = strings.Join(strings.Fields(normalized), " ")

	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}
