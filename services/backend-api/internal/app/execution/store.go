package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// SQLIdempotencyStore implements IdempotencyStore using SQL database
type SQLIdempotencyStore struct {
	db *sql.DB
}

// NewSQLIdempotencyStore creates a new SQL-backed idempotency store
func NewSQLIdempotencyStore(db *sql.DB) (*SQLIdempotencyStore, error) {
	store := &SQLIdempotencyStore{db: db}
	if err := store.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure idempotency table: %w", err)
	}
	return store, nil
}

// ensureTable creates the idempotency table if it doesn't exist
func (s *SQLIdempotencyStore) ensureTable() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS order_intents (
			intent_id TEXT PRIMARY KEY,
			client_order_id TEXT UNIQUE NOT NULL,
			exchange_order_id TEXT,
			status TEXT NOT NULL CHECK (status IN ('pending', 'open', 'filled', 'partial', 'cancelled', 'rejected')),
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
			order_type TEXT NOT NULL CHECK (order_type IN ('market', 'limit')),
			amount TEXT NOT NULL,
			price TEXT,
			stop_price TEXT,
			take_profit TEXT,
			reduce_only BOOLEAN DEFAULT FALSE,
			post_only BOOLEAN DEFAULT FALSE,
			filled_amount TEXT DEFAULT '0',
			fill_price TEXT DEFAULT '0',
			reject_reason TEXT,
			attempt_count INTEGER DEFAULT 1,
			submitted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			strategy_id TEXT,
			signal_id TEXT,
			metadata TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_order_intents_client_id ON order_intents(client_order_id);`,
		`CREATE INDEX IF NOT EXISTS idx_order_intents_exchange_id ON order_intents(exchange_order_id);`,
		`CREATE INDEX IF NOT EXISTS idx_order_intents_status ON order_intents(status);`,
		`CREATE INDEX IF NOT EXISTS idx_order_intents_submitted ON order_intents(submitted_at);`,
		`CREATE INDEX IF NOT EXISTS idx_order_intents_exchange ON order_intents(exchange);`,
		`CREATE INDEX IF NOT EXISTS idx_order_intents_symbol ON order_intents(symbol);`,
		`CREATE INDEX IF NOT EXISTS idx_order_intents_strategy ON order_intents(strategy_id);`,
	}

	for i, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("execute idempotency schema statement %d: %w", i+1, err)
		}
	}
	return nil
}

// SaveIntent persists a new order intent
func (s *SQLIdempotencyStore) SaveIntent(ctx context.Context, intent *OrderIntent) error {
	query := `
	INSERT INTO order_intents (
		intent_id, client_order_id, exchange_order_id, status,
		exchange, symbol, side, order_type, amount, price,
		stop_price, take_profit, reduce_only, post_only,
		filled_amount, fill_price, reject_reason, attempt_count,
		submitted_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	query = rebindQuestionPlaceholders(query)

	_, err := s.db.ExecContext(ctx, query,
		intent.IntentID,
		intent.ClientOrderID,
		intent.ExchangeOrderID,
		string(intent.Status),
		intent.Request.Exchange,
		intent.Request.Symbol,
		string(intent.Request.Side),
		string(intent.Request.Type),
		intent.Request.Amount.String(),
		optionalDecimal(intent.Request.Price),
		optionalDecimal(intent.Request.StopPrice),
		optionalDecimal(intent.Request.TakeProfit),
		intent.Request.ReduceOnly,
		intent.Request.PostOnly,
		decimalString(intent.FilledAmount),
		decimalString(intent.FillPrice),
		intent.RejectReason,
		intent.AttemptCount,
		intent.SubmittedAt,
		intent.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("save intent %s: %w", intent.IntentID, err)
	}

	return nil
}

// GetIntent retrieves an intent by IntentID
func (s *SQLIdempotencyStore) GetIntent(ctx context.Context, intentID string) (*OrderIntent, error) {
	query := `
	SELECT intent_id, client_order_id, exchange_order_id, status,
		exchange, symbol, side, order_type, amount, price,
		stop_price, take_profit, reduce_only, post_only,
		filled_amount, fill_price, reject_reason, attempt_count,
		submitted_at, updated_at
	FROM order_intents WHERE intent_id = ?
	`
	query = rebindQuestionPlaceholders(query)

	var intent OrderIntent
	var statusStr, sideStr, typeStr string
	var amountStr, filledStr, fillPriceStr string
	var priceStr, stopPriceStr, takeProfitStr, rejectReason sql.NullString

	err := s.db.QueryRowContext(ctx, query, intentID).Scan(
		&intent.IntentID,
		&intent.ClientOrderID,
		&intent.ExchangeOrderID,
		&statusStr,
		&intent.Request.Exchange,
		&intent.Request.Symbol,
		&sideStr,
		&typeStr,
		&amountStr,
		&priceStr,
		&stopPriceStr,
		&takeProfitStr,
		&intent.Request.ReduceOnly,
		&intent.Request.PostOnly,
		&filledStr,
		&fillPriceStr,
		&rejectReason,
		&intent.AttemptCount,
		&intent.SubmittedAt,
		&intent.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query intent by intent_id %s: %w", intentID, err)
	}

	// Parse enums
	intent.Status = parseOrderStatus(statusStr)
	intent.Request.Side = parseOrderSide(sideStr)
	intent.Request.Type = parseOrderType(typeStr)
	intent.RejectReason = nullStringValue(rejectReason)

	// Parse decimals
	intent.Request.Amount, err = parseDecimal("order_intents.amount", amountStr)
	if err != nil {
		return nil, fmt.Errorf("decode intent %s amount: %w", intentID, err)
	}
	intent.Request.Price, err = parseNullableDecimal("order_intents.price", priceStr)
	if err != nil {
		return nil, fmt.Errorf("decode intent %s price: %w", intentID, err)
	}
	intent.Request.StopPrice, err = parseNullableDecimal("order_intents.stop_price", stopPriceStr)
	if err != nil {
		return nil, fmt.Errorf("decode intent %s stop_price: %w", intentID, err)
	}
	intent.Request.TakeProfit, err = parseNullableDecimal("order_intents.take_profit", takeProfitStr)
	if err != nil {
		return nil, fmt.Errorf("decode intent %s take_profit: %w", intentID, err)
	}
	intent.FilledAmount, err = parseDecimal("order_intents.filled_amount", filledStr)
	if err != nil {
		return nil, fmt.Errorf("decode intent %s filled_amount: %w", intentID, err)
	}
	intent.FillPrice, err = parseDecimal("order_intents.fill_price", fillPriceStr)
	if err != nil {
		return nil, fmt.Errorf("decode intent %s fill_price: %w", intentID, err)
	}

	return &intent, nil
}

// GetIntentByClientID retrieves an intent by ClientOrderID
func (s *SQLIdempotencyStore) GetIntentByClientID(ctx context.Context, clientID string) (*OrderIntent, error) {
	query := `
	SELECT intent_id FROM order_intents WHERE client_order_id = ?
	`
	query = rebindQuestionPlaceholders(query)

	var intentID string
	err := s.db.QueryRowContext(ctx, query, clientID).Scan(&intentID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query intent by client_order_id %s: %w", clientID, err)
	}

	return s.GetIntent(ctx, intentID)
}

// GetIntentByExchangeID retrieves an intent by exchange and ExchangeOrderID.
func (s *SQLIdempotencyStore) GetIntentByExchangeID(ctx context.Context, exchange, exchangeOrderID string) (*OrderIntent, error) {
	query := `
	SELECT intent_id FROM order_intents WHERE exchange = ? AND exchange_order_id = ?
	ORDER BY updated_at DESC
	LIMIT 1
	`
	query = rebindQuestionPlaceholders(query)

	var intentID string
	err := s.db.QueryRowContext(ctx, query, exchange, exchangeOrderID).Scan(&intentID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"query intent by exchange=%s exchange_order_id=%s: %w",
			exchange,
			exchangeOrderID,
			err,
		)
	}

	return s.GetIntent(ctx, intentID)
}

// UpdateIntent updates an existing intent
func (s *SQLIdempotencyStore) UpdateIntent(ctx context.Context, intent *OrderIntent) error {
	intent.UpdatedAt = time.Now()

	query := `
	UPDATE order_intents SET
		exchange_order_id = ?,
		status = ?,
		filled_amount = ?,
		fill_price = ?,
		reject_reason = ?,
		attempt_count = ?,
		updated_at = ?
	WHERE intent_id = ?
	`
	query = rebindQuestionPlaceholders(query)

	result, err := s.db.ExecContext(ctx, query,
		intent.ExchangeOrderID,
		string(intent.Status),
		intent.FilledAmount.String(),
		intent.FillPrice.String(),
		intent.RejectReason,
		intent.AttemptCount,
		intent.UpdatedAt,
		intent.IntentID,
	)

	if err != nil {
		return fmt.Errorf("update intent %s: %w", intent.IntentID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for intent %s: %w", intent.IntentID, err)
	}
	if rows == 0 {
		return errors.New("intent not found")
	}

	return nil
}

// SQLAuditLogger implements AuditLogger using SQL database
type SQLAuditLogger struct {
	db *sql.DB
}

// NewSQLAuditLogger creates a new SQL-backed audit logger
func NewSQLAuditLogger(db *sql.DB) (*SQLAuditLogger, error) {
	logger := &SQLAuditLogger{db: db}
	if err := logger.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure audit table: %w", err)
	}
	return logger, nil
}

// ensureTable creates the audit log table if it doesn't exist
func (l *SQLAuditLogger) ensureTable() error {
	idColumn := "id BIGSERIAL PRIMARY KEY"
	if isSQLiteDriver(l.db) {
		// SQLite equivalent of auto-incrementing primary key.
		idColumn = "id INTEGER PRIMARY KEY"
	}

	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS order_audit_log (
			%s,
			event_id TEXT UNIQUE NOT NULL,
			intent_id TEXT NOT NULL,
			client_order_id TEXT NOT NULL,
			exchange_order_id TEXT,
			event_type TEXT NOT NULL CHECK (event_type IN ('submitted', 'placed', 'filled', 'rejected', 'cancelled', 'cancel_failed', 'validation_failed')),
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT,
			amount TEXT,
			price TEXT,
			filled_amount TEXT,
			fill_price TEXT,
			reason TEXT,
			metadata TEXT,
			timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			hash_chain TEXT
		);`, idColumn),
		`CREATE INDEX IF NOT EXISTS idx_audit_intent ON order_audit_log(intent_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_client_id ON order_audit_log(client_order_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_exchange_id ON order_audit_log(exchange_order_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_event_id ON order_audit_log(event_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON order_audit_log(timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_type ON order_audit_log(event_type);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_exchange ON order_audit_log(exchange);`,
	}

	for i, stmt := range statements {
		if _, err := l.db.Exec(stmt); err != nil {
			return fmt.Errorf("execute audit schema statement %d: %w", i+1, err)
		}
	}
	return nil
}

// LogOrderEvent persists an audit event
func (l *SQLAuditLogger) LogOrderEvent(ctx context.Context, event OrderAuditEvent) error {
	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata for event %s: %w", event.EventID, err)
	}

	query := `
	INSERT INTO order_audit_log (
		event_id, intent_id, client_order_id, exchange_order_id,
		event_type, exchange, symbol, side, amount, price,
		filled_amount, fill_price, reason, metadata, timestamp, hash_chain
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	query = rebindQuestionPlaceholders(query)

	_, err = l.db.ExecContext(ctx, query,
		event.EventID,
		event.IntentID,
		event.ClientOrderID,
		event.ExchangeOrderID,
		event.EventType,
		event.Exchange,
		event.Symbol,
		event.Side,
		decimalString(event.Amount),
		decimalString(event.Price),
		decimalString(event.FilledAmount),
		decimalString(event.FillPrice),
		event.Reason,
		string(metadataJSON),
		event.Timestamp,
		event.HashChain,
	)
	if err != nil {
		return fmt.Errorf("insert audit event %s: %w", event.EventID, err)
	}

	return nil
}

// GetOrderHistory retrieves audit history for an intent
func (l *SQLAuditLogger) GetOrderHistory(ctx context.Context, intentID string) ([]OrderAuditEvent, error) {
	query := `
	SELECT event_id, intent_id, client_order_id, exchange_order_id,
		event_type, exchange, symbol, side, amount, price,
		filled_amount, fill_price, reason, metadata, timestamp, hash_chain
	FROM order_audit_log
	WHERE intent_id = ?
	ORDER BY timestamp ASC
	`
	query = rebindQuestionPlaceholders(query)

	rows, err := l.db.QueryContext(ctx, query, intentID)
	if err != nil {
		return nil, fmt.Errorf("query audit history for intent %s: %w", intentID, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var events []OrderAuditEvent
	for rows.Next() {
		var event OrderAuditEvent
		var amountStr, priceStr, filledStr, fillPriceStr sql.NullString
		var side, metadataStr, exchangeOrderID, reason sql.NullString

		err := rows.Scan(
			&event.EventID,
			&event.IntentID,
			&event.ClientOrderID,
			&exchangeOrderID,
			&event.EventType,
			&event.Exchange,
			&event.Symbol,
			&side,
			&amountStr,
			&priceStr,
			&filledStr,
			&fillPriceStr,
			&reason,
			&metadataStr,
			&event.Timestamp,
			&event.HashChain,
		)
		if err != nil {
			return nil, fmt.Errorf("scan audit row for intent %s: %w", intentID, err)
		}

		event.ExchangeOrderID = nullStringValue(exchangeOrderID)
		event.Side = nullStringValue(side)
		event.Reason = nullStringValue(reason)

		event.Amount, err = parseNullableDecimal("order_audit_log.amount", amountStr)
		if err != nil {
			return nil, fmt.Errorf("decode audit event %s amount: %w", event.EventID, err)
		}
		event.Price, err = parseNullableDecimal("order_audit_log.price", priceStr)
		if err != nil {
			return nil, fmt.Errorf("decode audit event %s price: %w", event.EventID, err)
		}
		event.FilledAmount, err = parseNullableDecimal("order_audit_log.filled_amount", filledStr)
		if err != nil {
			return nil, fmt.Errorf("decode audit event %s filled_amount: %w", event.EventID, err)
		}
		event.FillPrice, err = parseNullableDecimal("order_audit_log.fill_price", fillPriceStr)
		if err != nil {
			return nil, fmt.Errorf("decode audit event %s fill_price: %w", event.EventID, err)
		}

		if metadataStr.Valid && metadataStr.String != "" {
			if err := json.Unmarshal([]byte(metadataStr.String), &event.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata for event %s: %w", event.EventID, err)
			}
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit rows for intent %s: %w", intentID, err)
	}
	return events, nil
}

// Helper functions

func optionalDecimal(d decimal.Decimal) interface{} {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

func decimalString(d decimal.Decimal) string {
	return d.String()
}

func parseDecimal(field, s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, fmt.Errorf("parse decimal for %s: empty value", field)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parse decimal for %s: %w", field, err)
	}
	return d, nil
}

func parseNullableDecimal(field string, s sql.NullString) (decimal.Decimal, error) {
	if !s.Valid || s.String == "" {
		return decimal.Zero, nil
	}
	return parseDecimal(field, s.String)
}

func nullStringValue(s sql.NullString) string {
	if !s.Valid {
		return ""
	}
	return s.String
}

func rebindQuestionPlaceholders(query string) string {
	argPos := 1
	var out strings.Builder
	out.Grow(len(query) + 16)
	for _, ch := range query {
		if ch != '?' {
			out.WriteRune(ch)
			continue
		}
		out.WriteByte('$')
		out.WriteString(strconv.Itoa(argPos))
		argPos++
	}
	return out.String()
}

func isSQLiteDriver(db *sql.DB) bool {
	return strings.Contains(strings.ToLower(fmt.Sprintf("%T", db.Driver())), "sqlite")
}

func parseOrderStatus(s string) ports.OrderStatus {
	switch s {
	case "pending":
		return ports.OrderStatusPending
	case "open":
		return ports.OrderStatusOpen
	case "filled":
		return ports.OrderStatusFilled
	case "partial":
		return ports.OrderStatusPartial
	case "cancelled":
		return ports.OrderStatusCancelled
	case "rejected":
		return ports.OrderStatusRejected
	default:
		return ports.OrderStatusPending
	}
}

func parseOrderSide(s string) ports.OrderSide {
	switch s {
	case "buy":
		return ports.OrderSideBuy
	case "sell":
		return ports.OrderSideSell
	default:
		return ports.OrderSideBuy
	}
}

func parseOrderType(s string) ports.OrderType {
	switch s {
	case "market":
		return ports.OrderTypeMarket
	case "limit":
		return ports.OrderTypeLimit
	default:
		return ports.OrderTypeMarket
	}
}

// Compile-time interface checks
var (
	_ IdempotencyStore = (*SQLIdempotencyStore)(nil)
	_ AuditLogger      = (*SQLAuditLogger)(nil)
)
