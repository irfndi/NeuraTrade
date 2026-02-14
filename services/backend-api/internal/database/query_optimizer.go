package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type SlowQuery struct {
	Query       string        `json:"query"`
	TableName   string        `json:"table_name"`
	Duration    time.Duration `json:"duration_ms"`
	Timestamp   time.Time     `json:"timestamp"`
	RowCount    int           `json:"row_count"`
	CallCount   int           `json:"call_count"`
	ExplainPlan string        `json:"explain_plan,omitempty"`
}

type SlowQueryLogger struct {
	queries    map[string]*SlowQuery
	mu         sync.RWMutex
	maxEntries int
	threshold  time.Duration
}

func NewSlowQueryLogger(threshold time.Duration, maxEntries int) *SlowQueryLogger {
	return &SlowQueryLogger{
		queries:    make(map[string]*SlowQuery),
		maxEntries: maxEntries,
		threshold:  threshold,
	}
}

func (l *SlowQueryLogger) LogQuery(ctx context.Context, query string, duration time.Duration, rowCount int) {
	if duration < l.threshold {
		return
	}

	queryHash := l.hashQuery(query)
	tableName := extractTableNameFromQuery(query)

	l.mu.Lock()
	defer l.mu.Unlock()

	if existing, ok := l.queries[queryHash]; ok {
		existing.Duration = (existing.Duration*time.Duration(existing.CallCount) + duration) / time.Duration(existing.CallCount+1)
		existing.CallCount++
		existing.Timestamp = time.Now()
		if rowCount > existing.RowCount {
			existing.RowCount = rowCount
		}
	} else {
		l.queries[queryHash] = &SlowQuery{
			Query:     truncateQuery(query),
			TableName: tableName,
			Duration:  duration,
			Timestamp: time.Now(),
			RowCount:  rowCount,
			CallCount: 1,
		}

		if len(l.queries) > l.maxEntries {
			l.evictOldest()
		}
	}
}

func (l *SlowQueryLogger) GetSlowQueries() []SlowQuery {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]SlowQuery, 0, len(l.queries))
	for _, q := range l.queries {
		result = append(result, *q)
	}

	return result
}

func (l *SlowQueryLogger) GetSlowQueriesByTable(tableName string) []SlowQuery {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SlowQuery
	for _, q := range l.queries {
		if q.TableName == tableName {
			result = append(result, *q)
		}
	}

	return result
}

func (l *SlowQueryLogger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queries = make(map[string]*SlowQuery)
}

func (l *SlowQueryLogger) hashQuery(query string) string {
	normalized := normalizeQuery(query)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func (l *SlowQueryLogger) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, q := range l.queries {
		if oldestTime.IsZero() || q.Timestamp.Before(oldestTime) {
			oldestTime = q.Timestamp
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(l.queries, oldestKey)
	}
}

func normalizeQuery(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	query = strings.Join(strings.Fields(query), " ")
	return query
}

func extractTableNameFromQuery(query string) string {
	query = strings.ToLower(query)

	keywords := []string{"from", "into", "update", "table"}
	for _, keyword := range keywords {
		idx := strings.Index(query, keyword)
		if idx >= 0 {
			rest := strings.TrimSpace(query[idx+len(keyword):])
			parts := strings.Fields(rest)
			if len(parts) > 0 {
				table := parts[0]
				table = strings.Trim(table, "()\",;")
				return table
			}
		}
	}
	return "unknown"
}

func truncateQuery(query string) string {
	const maxLen = 200
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen] + "..."
}

type QueryOptimizer struct {
	slowQueryLogger *SlowQueryLogger
}

func NewQueryOptimizer(threshold time.Duration) *QueryOptimizer {
	return &QueryOptimizer{
		slowQueryLogger: NewSlowQueryLogger(threshold, 100),
	}
}

func (o *QueryOptimizer) LogQuery(ctx context.Context, query string, duration time.Duration, rowCount int) {
	o.slowQueryLogger.LogQuery(ctx, query, duration, rowCount)
}

func (o *QueryOptimizer) GetSlowQueries() []SlowQuery {
	return o.slowQueryLogger.GetSlowQueries()
}

func (o *QueryOptimizer) GetSlowQueriesByTable(tableName string) []SlowQuery {
	return o.slowQueryLogger.GetSlowQueriesByTable(tableName)
}

func (o *QueryOptimizer) GetSlowQueriesJSON() string {
	queries := o.GetSlowQueries()
	data, err := json.Marshal(map[string]interface{}{
		"slow_queries": len(queries),
		"queries":      queries,
	})
	if err != nil {
		return fmt.Sprintf("{\"slow_queries\": 0, \"error\": \"%s\"}", err.Error())
	}
	return string(data)
}

func (o *QueryOptimizer) ClearSlowQueries() {
	o.slowQueryLogger.Clear()
}

func (o *QueryOptimizer) SuggestIndexes() []string {
	queries := o.slowQueryLogger.GetSlowQueries()
	suggestions := make([]string, 0)
	seen := make(map[string]bool)

	for _, q := range queries {
		if q.CallCount > 5 && q.Duration > 100*time.Millisecond {
			suggestion := o.analyzeQueryForIndex(q.Query, q.TableName)
			if suggestion != "" && !seen[suggestion] {
				seen[suggestion] = true
				suggestions = append(suggestions, suggestion)
			}
		}
	}

	return suggestions
}

func (o *QueryOptimizer) analyzeQueryForIndex(query, tableName string) string {
	query = strings.ToLower(query)

	if strings.Contains(query, "where") {
		whereIdx := strings.Index(query, "where")
		conditions := strings.TrimSpace(query[whereIdx+5:])
		parts := strings.Fields(conditions)
		if len(parts) > 0 {
			column := strings.Trim(parts[0], "(),=<>")
			if !strings.Contains(column, "(") && !strings.Contains(column, " ") {
				return fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_%s ON %s(%s);",
					tableName, column, tableName, column)
			}
		}
	}

	if strings.Contains(query, "order by") {
		orderIdx := strings.Index(query, "order by")
		conditions := strings.TrimSpace(query[orderIdx+8:])
		parts := strings.Fields(conditions)
		if len(parts) > 0 {
			column := strings.Trim(parts[0], ",")
			if !strings.Contains(column, "(") {
				return fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_%s_order ON %s(%s);",
					tableName, column, tableName, column)
			}
		}
	}

	return ""
}
