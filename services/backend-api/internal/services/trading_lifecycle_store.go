package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

// TradingLifecycleStore persists autonomous order/position lifecycle and realized PnL.
type TradingLifecycleStore struct {
	db     database.DBPool
	logger *log.Logger
}

type LifecycleExecutionRecord struct {
	OrderID    string
	ChatID     string
	Exchange   string
	Symbol     string
	Side       string
	OrderType  string
	MarketType string
	Amount     decimal.Decimal
	EntryPrice decimal.Decimal
	Source     string
	OpenedAt   time.Time
}

type LifecycleCloseRecord struct {
	OrderID     string
	ChatID      string
	Exchange    string
	Symbol      string
	Side        string
	MarketType  string
	Filled      decimal.Decimal
	EntryPrice  decimal.Decimal
	ExitPrice   decimal.Decimal
	RealizedPnL decimal.Decimal
	Fees        decimal.Decimal
	Source      string
	ClosedAt    time.Time
}

func NewTradingLifecycleStore(db database.DBPool, logger *log.Logger) (*TradingLifecycleStore, error) {
	if db == nil {
		return nil, fmt.Errorf("lifecycle store requires database")
	}
	if logger == nil {
		logger = log.Default()
	}

	store := &TradingLifecycleStore{
		db:     db,
		logger: logger,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *TradingLifecycleStore) EnsureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS trading_orders (
			order_id TEXT PRIMARY KEY,
			position_id TEXT NOT NULL,
			chat_id TEXT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			type TEXT NOT NULL,
			market_type TEXT NOT NULL DEFAULT 'spot',
			amount NUMERIC NOT NULL,
			price NUMERIC NOT NULL,
			filled_amount NUMERIC NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'autonomous',
			closed_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS trading_positions (
			position_id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			chat_id TEXT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			market_type TEXT NOT NULL DEFAULT 'spot',
			size NUMERIC NOT NULL,
			entry_price NUMERIC NOT NULL,
			close_price NUMERIC NOT NULL DEFAULT 0,
			realized_pnl NUMERIC NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'autonomous',
			opened_at TIMESTAMP NOT NULL,
			closed_at TIMESTAMP NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS realized_pnl_journal (
			id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL UNIQUE,
			chat_id TEXT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			filled_amount NUMERIC NOT NULL DEFAULT 0,
			entry_price NUMERIC NOT NULL DEFAULT 0,
			exit_price NUMERIC NOT NULL DEFAULT 0,
			realized_pnl NUMERIC NOT NULL DEFAULT 0,
			fees NUMERIC NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'autonomous',
			closed_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_orders_position_id ON trading_orders(position_id)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_orders_chat_status ON trading_orders(chat_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_positions_symbol_status ON trading_positions(symbol, status)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_positions_chat_status ON trading_positions(chat_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_realized_pnl_journal_chat_closed ON realized_pnl_journal(chat_id, closed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_realized_pnl_journal_symbol_closed ON realized_pnl_journal(symbol, closed_at)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("lifecycle schema statement failed: %w", err)
		}
	}

	legacyColumns := []string{
		`ALTER TABLE trading_orders ADD COLUMN chat_id TEXT`,
		`ALTER TABLE trading_orders ADD COLUMN market_type TEXT`,
		`ALTER TABLE trading_orders ADD COLUMN filled_amount NUMERIC`,
		`ALTER TABLE trading_orders ADD COLUMN source TEXT`,
		`ALTER TABLE trading_orders ADD COLUMN closed_at TIMESTAMP`,
		`ALTER TABLE trading_positions ADD COLUMN chat_id TEXT`,
		`ALTER TABLE trading_positions ADD COLUMN market_type TEXT`,
		`ALTER TABLE trading_positions ADD COLUMN close_price NUMERIC`,
		`ALTER TABLE trading_positions ADD COLUMN realized_pnl NUMERIC`,
		`ALTER TABLE trading_positions ADD COLUMN source TEXT`,
		`ALTER TABLE trading_positions ADD COLUMN closed_at TIMESTAMP`,
	}
	for _, stmt := range legacyColumns {
		if _, err := s.db.Exec(ctx, stmt); err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("lifecycle schema alter failed: %w", err)
		}
	}

	return nil
}

func (s *TradingLifecycleStore) RecordOrderExecution(ctx context.Context, rec LifecycleExecutionRecord) error {
	orderID := strings.TrimSpace(rec.OrderID)
	if orderID == "" {
		return fmt.Errorf("order_id is required")
	}
	now := rec.OpenedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	side := normalizeLifecycleSide(rec.Side)
	orderType := normalizeLifecycleOrderType(rec.OrderType)
	marketType := normalizeLifecycleMarketType(rec.MarketType)
	source := normalizeLifecycleSource(rec.Source)
	status := "open"
	positionID := defaultPositionID(orderID, rec.Symbol, side)

	var existingPositionID string
	if err := s.db.QueryRow(ctx, `SELECT position_id FROM trading_orders WHERE order_id = $1`, orderID).Scan(&existingPositionID); err == nil {
		if strings.TrimSpace(existingPositionID) != "" {
			positionID = existingPositionID
		}
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO trading_orders (
			order_id, position_id, chat_id, exchange, symbol, side, type, market_type,
			amount, price, filled_amount, status, source, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT(order_id) DO UPDATE SET
			position_id = EXCLUDED.position_id,
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = EXCLUDED.side,
			type = EXCLUDED.type,
			market_type = EXCLUDED.market_type,
			amount = EXCLUDED.amount,
			price = EXCLUDED.price,
			filled_amount = EXCLUDED.filled_amount,
			status = EXCLUDED.status,
			source = EXCLUDED.source,
			closed_at = NULL,
			updated_at = EXCLUDED.updated_at
	`, orderID, positionID, strings.TrimSpace(rec.ChatID), strings.TrimSpace(rec.Exchange), strings.TrimSpace(rec.Symbol),
		side, orderType, marketType, rec.Amount, rec.EntryPrice, decimal.Zero, status, source, now, now); err != nil {
		return fmt.Errorf("upsert trading_orders failed: %w", err)
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO trading_positions (
			position_id, order_id, chat_id, exchange, symbol, side, market_type,
			size, entry_price, status, source, opened_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'open',$10,$11,$12)
		ON CONFLICT(position_id) DO UPDATE SET
			order_id = EXCLUDED.order_id,
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = EXCLUDED.side,
			market_type = EXCLUDED.market_type,
			size = EXCLUDED.size,
			entry_price = EXCLUDED.entry_price,
			status = 'open',
			source = EXCLUDED.source,
			close_price = 0,
			realized_pnl = 0,
			closed_at = NULL,
			updated_at = EXCLUDED.updated_at
	`, positionID, orderID, strings.TrimSpace(rec.ChatID), strings.TrimSpace(rec.Exchange), strings.TrimSpace(rec.Symbol),
		side, marketType, rec.Amount, rec.EntryPrice, source, now, now); err != nil {
		return fmt.Errorf("upsert trading_positions failed: %w", err)
	}

	return nil
}

func (s *TradingLifecycleStore) RecordClosedOrder(ctx context.Context, rec LifecycleCloseRecord) error {
	orderID := strings.TrimSpace(rec.OrderID)
	if orderID == "" {
		return fmt.Errorf("order_id is required")
	}
	closedAt := rec.ClosedAt.UTC()
	if closedAt.IsZero() {
		closedAt = time.Now().UTC()
	}
	side := normalizeLifecycleSide(rec.Side)
	marketType := normalizeLifecycleMarketType(rec.MarketType)
	source := normalizeLifecycleSource(rec.Source)
	status := "closed"

	positionID := defaultPositionID(orderID, rec.Symbol, side)
	var existingEntryPrice decimal.Decimal
	err := s.db.QueryRow(ctx, `SELECT position_id, price FROM trading_orders WHERE order_id = $1`, orderID).Scan(&positionID, &existingEntryPrice)
	if err != nil && !isLifecycleNoRows(err) {
		return fmt.Errorf("load existing order failed: %w", err)
	}
	if rec.EntryPrice.IsZero() && !existingEntryPrice.IsZero() {
		rec.EntryPrice = existingEntryPrice
	}
	if rec.Filled.IsZero() {
		rec.Filled = decimal.NewFromInt(1)
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO trading_orders (
			order_id, position_id, chat_id, exchange, symbol, side, type, market_type,
			amount, price, filled_amount, status, source, closed_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,'market',$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT(order_id) DO UPDATE SET
			position_id = EXCLUDED.position_id,
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = EXCLUDED.side,
			market_type = EXCLUDED.market_type,
			filled_amount = EXCLUDED.filled_amount,
			status = EXCLUDED.status,
			source = EXCLUDED.source,
			closed_at = EXCLUDED.closed_at,
			updated_at = EXCLUDED.updated_at
	`, orderID, positionID, strings.TrimSpace(rec.ChatID), strings.TrimSpace(rec.Exchange), strings.TrimSpace(rec.Symbol),
		side, marketType, rec.Filled, rec.EntryPrice, rec.Filled, status, source, closedAt, closedAt, closedAt); err != nil {
		return fmt.Errorf("upsert closed trading_orders failed: %w", err)
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO trading_positions (
			position_id, order_id, chat_id, exchange, symbol, side, market_type,
			size, entry_price, close_price, realized_pnl, status, source, opened_at, closed_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'closed',$12,$13,$14,$15)
		ON CONFLICT(position_id) DO UPDATE SET
			order_id = EXCLUDED.order_id,
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = EXCLUDED.side,
			market_type = EXCLUDED.market_type,
			entry_price = CASE WHEN trading_positions.entry_price = 0 AND EXCLUDED.entry_price > 0 THEN EXCLUDED.entry_price ELSE trading_positions.entry_price END,
			close_price = EXCLUDED.close_price,
			realized_pnl = EXCLUDED.realized_pnl,
			status = 'closed',
			source = EXCLUDED.source,
			closed_at = EXCLUDED.closed_at,
			updated_at = EXCLUDED.updated_at
	`, positionID, orderID, strings.TrimSpace(rec.ChatID), strings.TrimSpace(rec.Exchange), strings.TrimSpace(rec.Symbol),
		side, marketType, rec.Filled, rec.EntryPrice, rec.ExitPrice, rec.RealizedPnL, source, closedAt, closedAt, closedAt); err != nil {
		return fmt.Errorf("upsert closed trading_positions failed: %w", err)
	}

	journalID := "pnl-" + safeIDPart(orderID)
	if _, err := s.db.Exec(ctx, `
		INSERT INTO realized_pnl_journal (
			id, order_id, chat_id, exchange, symbol, side, filled_amount,
			entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT(order_id) DO UPDATE SET
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = EXCLUDED.side,
			filled_amount = EXCLUDED.filled_amount,
			entry_price = EXCLUDED.entry_price,
			exit_price = EXCLUDED.exit_price,
			realized_pnl = EXCLUDED.realized_pnl,
			fees = EXCLUDED.fees,
			source = EXCLUDED.source,
			closed_at = EXCLUDED.closed_at
	`, journalID, orderID, strings.TrimSpace(rec.ChatID), strings.TrimSpace(rec.Exchange), strings.TrimSpace(rec.Symbol),
		side, rec.Filled, rec.EntryPrice, rec.ExitPrice, rec.RealizedPnL, rec.Fees, source, closedAt, closedAt); err != nil {
		return fmt.Errorf("upsert realized_pnl_journal failed: %w", err)
	}

	return nil
}

func (s *TradingLifecycleStore) SyncOpenOrder(ctx context.Context, chatID, exchange string, order ccxt.Order) error {
	if strings.TrimSpace(order.ID) == "" {
		return nil
	}
	return s.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    strings.TrimSpace(order.ID),
		ChatID:     strings.TrimSpace(chatID),
		Exchange:   strings.TrimSpace(exchange),
		Symbol:     strings.TrimSpace(order.Symbol),
		Side:       strings.TrimSpace(order.Side),
		OrderType:  strings.TrimSpace(order.Type),
		MarketType: "futures",
		Amount:     order.Amount,
		EntryPrice: order.Price,
		Source:     "bootstrap_open_orders",
		OpenedAt:   order.CreatedAt,
	})
}

func (s *TradingLifecycleStore) SyncPosition(ctx context.Context, chatID, exchange string, position ccxt.Position) error {
	if strings.TrimSpace(position.Symbol) == "" || position.Size.IsZero() {
		return nil
	}
	positionID := "sync-" + safeIDPart(strings.ToLower(strings.TrimSpace(exchange))+"-"+normalizeSymbolForComparison(position.Symbol)+"-"+strings.ToLower(strings.TrimSpace(position.Side)))
	now := position.Timestamp.Time().UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	res, err := s.db.Exec(ctx, `
		UPDATE trading_positions
		SET chat_id = $2, exchange = $3, symbol = $4, side = $5, market_type = 'futures',
			size = $6, entry_price = $7, close_price = 0, realized_pnl = 0, status = 'open',
			source = 'bootstrap_positions', closed_at = NULL, updated_at = $8
		WHERE position_id = $1
	`, positionID, strings.TrimSpace(chatID), strings.TrimSpace(exchange), strings.TrimSpace(position.Symbol),
		normalizeLifecycleSide(position.Side), position.Size, position.EntryPrice, now)
	if err != nil {
		return fmt.Errorf("sync position update failed: %w", err)
	}
	updated, _ := res.RowsAffected()
	if updated == 0 {
		if _, err := s.db.Exec(ctx, `
			INSERT INTO trading_positions (
				position_id, order_id, chat_id, exchange, symbol, side, market_type,
				size, entry_price, status, source, opened_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,'futures',$7,$8,'open','bootstrap_positions',$9,$10)
		`, positionID, positionID, strings.TrimSpace(chatID), strings.TrimSpace(exchange), strings.TrimSpace(position.Symbol),
			normalizeLifecycleSide(position.Side), position.Size, position.EntryPrice, now, now); err != nil {
			return fmt.Errorf("sync position insert failed: %w", err)
		}
	}
	return nil
}

func normalizeLifecycleSide(side string) string {
	normalized := strings.ToLower(strings.TrimSpace(side))
	switch normalized {
	case "long", "open_long":
		return "buy"
	case "short", "open_short":
		return "sell"
	case "buy", "sell":
		return normalized
	default:
		return normalized
	}
}

func normalizeLifecycleOrderType(orderType string) string {
	normalized := strings.ToLower(strings.TrimSpace(orderType))
	if normalized == "" {
		return "market"
	}
	return normalized
}

func normalizeLifecycleMarketType(marketType string) string {
	normalized := strings.ToLower(strings.TrimSpace(marketType))
	if normalized == "" {
		return "futures"
	}
	return normalized
}

func normalizeLifecycleSource(source string) string {
	normalized := strings.TrimSpace(source)
	if normalized == "" {
		return "autonomous"
	}
	return normalized
}

func defaultPositionID(orderID, symbol, side string) string {
	if strings.TrimSpace(orderID) != "" {
		return "pos-" + safeIDPart(orderID)
	}
	return "pos-" + safeIDPart(symbol+"-"+side)
}

func safeIDPart(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	var builder strings.Builder
	for _, ch := range trimmed {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			builder.WriteRune(ch)
			continue
		}
		builder.WriteRune('-')
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	return result
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name") ||
		strings.Contains(msg, "column") && strings.Contains(msg, "already exists")
}

func isLifecycleNoRows(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no rows")
}
