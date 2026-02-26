package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
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
	StopLoss   decimal.Decimal
	TakeProfit decimal.Decimal
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

type ManagedOpenPosition struct {
	PositionID          string
	OrderID             string
	ChatID              string
	Exchange            string
	Symbol              string
	Side                string
	MarketType          string
	Source              string
	Size                decimal.Decimal
	EntryPrice          decimal.Decimal
	StopLoss            decimal.Decimal
	TakeProfit          decimal.Decimal
	LastPrice           decimal.Decimal
	UnrealizedPnL       decimal.Decimal
	ProtectionUpdatedAt time.Time
	OpenedAt            time.Time
	UpdatedAt           time.Time
}

type LifecyclePerformanceSummary struct {
	Trades      int
	Wins        int
	Losses      int
	RealizedPnL decimal.Decimal
	BestTrade   decimal.Decimal
	WorstTrade  decimal.Decimal
}

type LifecycleExchangeSnapshot struct {
	OpenOrders     []ccxt.Order
	Positions      []ccxt.Position
	OrdersFresh    bool
	PositionsFresh bool
}

type LifecycleSnapshotReconcileSummary struct {
	OrdersSynced    int
	OrdersCancelled int
	PositionsSynced int
	PositionsClosed int
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
				stop_loss NUMERIC NOT NULL DEFAULT 0,
				take_profit NUMERIC NOT NULL DEFAULT 0,
				last_price NUMERIC NOT NULL DEFAULT 0,
				unrealized_pnl NUMERIC NOT NULL DEFAULT 0,
				protection_updated_at TIMESTAMP NULL,
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
		`ALTER TABLE trading_positions ADD COLUMN stop_loss NUMERIC`,
		`ALTER TABLE trading_positions ADD COLUMN take_profit NUMERIC`,
		`ALTER TABLE trading_positions ADD COLUMN last_price NUMERIC`,
		`ALTER TABLE trading_positions ADD COLUMN unrealized_pnl NUMERIC`,
		`ALTER TABLE trading_positions ADD COLUMN protection_updated_at TIMESTAMP`,
		`ALTER TABLE trading_positions ADD COLUMN source TEXT`,
		`ALTER TABLE trading_positions ADD COLUMN closed_at TIMESTAMP`,
	}
	for _, stmt := range legacyColumns {
		if _, err := s.db.Exec(ctx, stmt); err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("lifecycle schema alter failed: %w", err)
		}
	}

	indexStatements := []string{
		`CREATE INDEX IF NOT EXISTS idx_trading_orders_position_id ON trading_orders(position_id)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_orders_chat_status ON trading_orders(chat_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_positions_symbol_status ON trading_positions(symbol, status)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_positions_chat_status ON trading_positions(chat_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_realized_pnl_journal_chat_closed ON realized_pnl_journal(chat_id, closed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_realized_pnl_journal_symbol_closed ON realized_pnl_journal(symbol, closed_at)`,
	}
	for _, stmt := range indexStatements {
		if _, err := s.db.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("lifecycle schema index failed: %w", err)
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
				size, entry_price, stop_loss, take_profit, last_price, unrealized_pnl, protection_updated_at,
				status, source, opened_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'open',$15,$16,$17)
			ON CONFLICT(position_id) DO UPDATE SET
				order_id = EXCLUDED.order_id,
				chat_id = EXCLUDED.chat_id,
				exchange = EXCLUDED.exchange,
				symbol = EXCLUDED.symbol,
				side = EXCLUDED.side,
				market_type = EXCLUDED.market_type,
				size = EXCLUDED.size,
				entry_price = EXCLUDED.entry_price,
				stop_loss = CASE WHEN EXCLUDED.stop_loss > 0 THEN EXCLUDED.stop_loss ELSE trading_positions.stop_loss END,
				take_profit = CASE WHEN EXCLUDED.take_profit > 0 THEN EXCLUDED.take_profit ELSE trading_positions.take_profit END,
				last_price = EXCLUDED.last_price,
				unrealized_pnl = EXCLUDED.unrealized_pnl,
				protection_updated_at = CASE
					WHEN EXCLUDED.stop_loss > 0 OR EXCLUDED.take_profit > 0 THEN EXCLUDED.protection_updated_at
					ELSE trading_positions.protection_updated_at
				END,
				status = 'open',
				source = EXCLUDED.source,
				close_price = 0,
				realized_pnl = 0,
				closed_at = NULL,
				updated_at = EXCLUDED.updated_at
		`, positionID, orderID, strings.TrimSpace(rec.ChatID), strings.TrimSpace(rec.Exchange), strings.TrimSpace(rec.Symbol),
		side, marketType, rec.Amount, rec.EntryPrice, rec.StopLoss, rec.TakeProfit, rec.EntryPrice, decimal.Zero, now, source, now, now); err != nil {
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

	if _, err := s.db.Exec(ctx, `
		INSERT INTO trading_positions (
			position_id, order_id, chat_id, exchange, symbol, side, market_type,
			size, entry_price, close_price, realized_pnl, status, source, opened_at, closed_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,'futures',$7,$8,0,0,'open','bootstrap_positions',$9,NULL,$10)
		ON CONFLICT (position_id) DO UPDATE SET
			order_id = EXCLUDED.order_id,
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = EXCLUDED.side,
			market_type = EXCLUDED.market_type,
			size = EXCLUDED.size,
			entry_price = EXCLUDED.entry_price,
			close_price = 0,
			realized_pnl = 0,
			status = 'open',
			source = 'bootstrap_positions',
			closed_at = NULL,
			updated_at = EXCLUDED.updated_at
	`, positionID, positionID, strings.TrimSpace(chatID), strings.TrimSpace(exchange), strings.TrimSpace(position.Symbol),
		normalizeLifecycleSide(position.Side), position.Size, position.EntryPrice, now, now); err != nil {
		return fmt.Errorf("sync position upsert failed: %w", err)
	}
	return nil
}

func (s *TradingLifecycleStore) ReconcileExchangeSnapshot(
	ctx context.Context,
	chatID string,
	exchange string,
	snapshot LifecycleExchangeSnapshot,
	source string,
) (LifecycleSnapshotReconcileSummary, error) {
	summary := LifecycleSnapshotReconcileSummary{}
	chatID = strings.TrimSpace(chatID)
	exchange = strings.TrimSpace(exchange)
	now := time.Now().UTC()
	reconcileSource := normalizeLifecycleSource(source)
	openOrderIDs := make(map[string]struct{}, len(snapshot.OpenOrders))
	for _, order := range snapshot.OpenOrders {
		orderID := strings.TrimSpace(order.ID)
		if orderID == "" {
			continue
		}
		openOrderIDs[orderID] = struct{}{}
	}

	if snapshot.OrdersFresh {
		for _, order := range snapshot.OpenOrders {
			if strings.TrimSpace(order.ID) == "" {
				continue
			}
			if err := s.SyncOpenOrder(ctx, chatID, exchange, order); err != nil {
				return summary, fmt.Errorf("sync open order %s failed: %w", strings.TrimSpace(order.ID), err)
			}
			summary.OrdersSynced++
		}

		orderQuery := `
			SELECT order_id
			FROM trading_orders
			WHERE LOWER(status) IN ('open', 'pending', 'partial')
		`
		orderArgs := make([]interface{}, 0, 2)
		if chatID != "" {
			orderQuery += fmt.Sprintf(" AND COALESCE(chat_id, '') = $%d", len(orderArgs)+1)
			orderArgs = append(orderArgs, chatID)
		}
		if exchange != "" {
			orderQuery += fmt.Sprintf(" AND exchange = $%d", len(orderArgs)+1)
			orderArgs = append(orderArgs, exchange)
		}

		rows, err := s.db.Query(ctx, orderQuery, orderArgs...)
		if err != nil {
			return summary, fmt.Errorf("query open trading_orders failed: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var orderID string
			if err := rows.Scan(&orderID); err != nil {
				return summary, fmt.Errorf("scan open trading_order failed: %w", err)
			}
			orderID = strings.TrimSpace(orderID)
			if orderID == "" {
				continue
			}
			if _, ok := openOrderIDs[orderID]; ok {
				continue
			}
			if _, err := s.db.Exec(ctx, `
				UPDATE trading_orders
				SET status = 'cancelled', closed_at = $2, updated_at = $2
				WHERE order_id = $1
			`, orderID, now); err != nil {
				return summary, fmt.Errorf("cancel stale trading_order %s failed: %w", orderID, err)
			}
			summary.OrdersCancelled++
		}
		if err := rows.Err(); err != nil {
			return summary, fmt.Errorf("iterate open trading_orders failed: %w", err)
		}
	}

	if snapshot.PositionsFresh {
		for _, position := range snapshot.Positions {
			if err := s.SyncPosition(ctx, chatID, exchange, position); err != nil {
				return summary, fmt.Errorf("sync position %s failed: %w", strings.TrimSpace(position.Symbol), err)
			}
			if strings.TrimSpace(position.Symbol) != "" && !position.Size.IsZero() {
				summary.PositionsSynced++
			}
		}

		type localOpenPosition struct {
			PositionID    string
			OrderID       string
			Symbol        string
			Side          string
			Size          decimal.Decimal
			EntryPrice    decimal.Decimal
			LastPrice     decimal.Decimal
			UnrealizedPnL decimal.Decimal
			MarketType    string
			Source        string
		}

		posQuery := `
			SELECT
				position_id,
				COALESCE(order_id, ''),
				symbol,
				side,
				size,
				entry_price,
				COALESCE(last_price, 0),
				COALESCE(unrealized_pnl, 0),
				COALESCE(market_type, 'futures'),
				COALESCE(source, '')
			FROM trading_positions
			WHERE LOWER(status) = 'open'
		`
		posArgs := make([]interface{}, 0, 2)
		if chatID != "" {
			posQuery += fmt.Sprintf(" AND COALESCE(chat_id, '') = $%d", len(posArgs)+1)
			posArgs = append(posArgs, chatID)
		}
		if exchange != "" {
			posQuery += fmt.Sprintf(" AND exchange = $%d", len(posArgs)+1)
			posArgs = append(posArgs, exchange)
		}

		posRows, err := s.db.Query(ctx, posQuery, posArgs...)
		if err != nil {
			return summary, fmt.Errorf("query open trading_positions failed: %w", err)
		}
		defer posRows.Close()

		localOpen := make([]localOpenPosition, 0)
		for posRows.Next() {
			var p localOpenPosition
			if err := posRows.Scan(
				&p.PositionID,
				&p.OrderID,
				&p.Symbol,
				&p.Side,
				&p.Size,
				&p.EntryPrice,
				&p.LastPrice,
				&p.UnrealizedPnL,
				&p.MarketType,
				&p.Source,
			); err != nil {
				return summary, fmt.Errorf("scan open trading_position failed: %w", err)
			}
			localOpen = append(localOpen, p)
		}
		if err := posRows.Err(); err != nil {
			return summary, fmt.Errorf("iterate open trading_positions failed: %w", err)
		}

		remainingByKey := make(map[string]decimal.Decimal, len(snapshot.Positions))
		for _, pos := range snapshot.Positions {
			if strings.TrimSpace(pos.Symbol) == "" || pos.Size.IsZero() {
				continue
			}
			key := normalizeSymbolForComparison(pos.Symbol) + ":" + normalizeLifecycleSide(pos.Side)
			remainingByKey[key] = remainingByKey[key].Add(pos.Size.Abs())
		}

		sort.SliceStable(localOpen, func(i, j int) bool {
			leftSource := strings.TrimSpace(localOpen[i].Source)
			rightSource := strings.TrimSpace(localOpen[j].Source)
			leftBootstrap := strings.EqualFold(leftSource, "bootstrap_positions")
			rightBootstrap := strings.EqualFold(rightSource, "bootstrap_positions")
			if leftBootstrap != rightBootstrap {
				return leftBootstrap
			}
			leftSync := strings.HasPrefix(strings.TrimSpace(localOpen[i].PositionID), "sync-")
			rightSync := strings.HasPrefix(strings.TrimSpace(localOpen[j].PositionID), "sync-")
			if leftSync != rightSync {
				return leftSync
			}
			return strings.TrimSpace(localOpen[i].PositionID) < strings.TrimSpace(localOpen[j].PositionID)
		})

		for _, localPos := range localOpen {
			localOrderID := strings.TrimSpace(localPos.OrderID)
			if localOrderID != "" {
				// When open-order snapshots are stale/unavailable, avoid force-closing rows tied to orders.
				if !snapshot.OrdersFresh {
					continue
				}
				if _, ok := openOrderIDs[localOrderID]; ok {
					continue
				}
			}

			key := normalizeSymbolForComparison(localPos.Symbol) + ":" + normalizeLifecycleSide(localPos.Side)
			remaining := remainingByKey[key]
			localSize := localPos.Size.Abs()
			if remaining.GreaterThan(decimal.Zero) {
				if localSize.GreaterThan(decimal.Zero) && remaining.GreaterThanOrEqual(localSize) {
					remainingByKey[key] = remaining.Sub(localSize)
					continue
				}
				// Preserve the snapshot-backed aggregate row for partial-size leftovers.
				if strings.EqualFold(strings.TrimSpace(localPos.Source), "bootstrap_positions") ||
					strings.HasPrefix(strings.TrimSpace(localPos.PositionID), "sync-") {
					remainingByKey[key] = decimal.Zero
					continue
				}
			}

			filled := localPos.Size
			if filled.IsZero() {
				filled = decimal.NewFromInt(1)
			}
			exitPrice := localPos.LastPrice
			if exitPrice.IsZero() {
				exitPrice = localPos.EntryPrice
			}

			realized := localPos.UnrealizedPnL
			normalizedSide := normalizeLifecycleSide(localPos.Side)
			if realized.IsZero() &&
				filled.GreaterThan(decimal.Zero) &&
				localPos.EntryPrice.GreaterThan(decimal.Zero) &&
				exitPrice.GreaterThan(decimal.Zero) {
				if normalizedSide == "sell" {
					realized = localPos.EntryPrice.Sub(exitPrice).Mul(filled)
				} else {
					realized = exitPrice.Sub(localPos.EntryPrice).Mul(filled)
				}
			}

			orderID := strings.TrimSpace(localPos.OrderID)
			if orderID == "" {
				orderID = strings.TrimSpace(localPos.PositionID)
			}
			if orderID == "" {
				orderID = "reconciled-" + safeIDPart(localPos.Symbol+"-"+localPos.Side+"-"+now.Format(time.RFC3339Nano))
			}

			if err := s.RecordClosedOrder(ctx, LifecycleCloseRecord{
				OrderID:     orderID,
				ChatID:      chatID,
				Exchange:    exchange,
				Symbol:      localPos.Symbol,
				Side:        normalizedSide,
				MarketType:  normalizeLifecycleMarketType(localPos.MarketType),
				Filled:      filled,
				EntryPrice:  localPos.EntryPrice,
				ExitPrice:   exitPrice,
				RealizedPnL: realized,
				Fees:        decimal.Zero,
				Source:      reconcileSource,
				ClosedAt:    now,
			}); err != nil {
				return summary, fmt.Errorf("close stale trading_position %s failed: %w", localPos.PositionID, err)
			}
			summary.PositionsClosed++
		}
	}

	return summary, nil
}

func (s *TradingLifecycleStore) ListManagedOpenPositions(ctx context.Context, chatID, exchange string, limit int) ([]ManagedOpenPosition, error) {
	query := `
		SELECT
			position_id, order_id, COALESCE(chat_id, ''), exchange, symbol, side, market_type,
			COALESCE(source, 'autonomous'),
			size, entry_price, COALESCE(stop_loss, 0), COALESCE(take_profit, 0),
			COALESCE(last_price, 0), COALESCE(unrealized_pnl, 0),
			protection_updated_at, opened_at, updated_at
		FROM trading_positions
		WHERE status = 'open'
	`
	args := make([]interface{}, 0, 3)
	if strings.TrimSpace(chatID) != "" {
		query += " AND chat_id = $1"
		args = append(args, strings.TrimSpace(chatID))
		if strings.TrimSpace(exchange) != "" {
			query += " AND exchange = $2"
			args = append(args, strings.TrimSpace(exchange))
		}
	} else if strings.TrimSpace(exchange) != "" {
		query += " AND exchange = $1"
		args = append(args, strings.TrimSpace(exchange))
	}
	query += " ORDER BY updated_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such column") {
			return []ManagedOpenPosition{}, nil
		}
		return nil, fmt.Errorf("query open managed positions failed: %w", err)
	}
	defer rows.Close()

	positions := make([]ManagedOpenPosition, 0)
	for rows.Next() {
		var p ManagedOpenPosition
		var protectionRaw interface{}
		var openedRaw interface{}
		var updatedRaw interface{}
		if err := rows.Scan(
			&p.PositionID,
			&p.OrderID,
			&p.ChatID,
			&p.Exchange,
			&p.Symbol,
			&p.Side,
			&p.MarketType,
			&p.Source,
			&p.Size,
			&p.EntryPrice,
			&p.StopLoss,
			&p.TakeProfit,
			&p.LastPrice,
			&p.UnrealizedPnL,
			&protectionRaw,
			&openedRaw,
			&updatedRaw,
		); err != nil {
			return nil, fmt.Errorf("scan managed position failed: %w", err)
		}
		p.OpenedAt = parseLifecycleTimestamp(openedRaw)
		p.UpdatedAt = parseLifecycleTimestamp(updatedRaw)
		p.ProtectionUpdatedAt = parseLifecycleTimestamp(protectionRaw)
		if p.ProtectionUpdatedAt.IsZero() {
			p.ProtectionUpdatedAt = p.OpenedAt
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed positions failed: %w", err)
	}
	return positions, nil
}

func (s *TradingLifecycleStore) CountOpenOrders(ctx context.Context, chatID, exchange string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM trading_orders
		WHERE status IN ('open', 'partial')
	`
	args := make([]interface{}, 0, 2)
	if strings.TrimSpace(chatID) != "" {
		query += fmt.Sprintf(" AND COALESCE(chat_id, '') = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(chatID))
	}
	if strings.TrimSpace(exchange) != "" {
		query += fmt.Sprintf(" AND exchange = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(exchange))
	}

	var count int
	if err := s.db.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count open orders failed: %w", err)
	}
	return count, nil
}

func (s *TradingLifecycleStore) GetRealizedPerformance(
	ctx context.Context,
	chatID string,
	exchange string,
	since time.Time,
) (LifecyclePerformanceSummary, error) {
	if since.IsZero() {
		since = time.Now().UTC().Add(-24 * time.Hour)
	}
	query := `
		SELECT
			COUNT(*),
			COALESCE(SUM(realized_pnl), 0),
			COALESCE(SUM(CASE WHEN realized_pnl > 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN realized_pnl < 0 THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(realized_pnl), 0),
			COALESCE(MIN(realized_pnl), 0)
		FROM realized_pnl_journal
		WHERE closed_at >= $1
	`
	args := []interface{}{since.UTC()}
	if strings.TrimSpace(chatID) != "" {
		query += fmt.Sprintf(" AND COALESCE(chat_id, '') = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(chatID))
	}
	if strings.TrimSpace(exchange) != "" {
		query += fmt.Sprintf(" AND exchange = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(exchange))
	}

	var summary LifecyclePerformanceSummary
	var wins int64
	var losses int64
	if err := s.db.QueryRow(ctx, query, args...).Scan(
		&summary.Trades,
		&summary.RealizedPnL,
		&wins,
		&losses,
		&summary.BestTrade,
		&summary.WorstTrade,
	); err != nil {
		return LifecyclePerformanceSummary{}, fmt.Errorf("query realized performance failed: %w", err)
	}
	summary.Wins = int(wins)
	summary.Losses = int(losses)
	if summary.Trades == 0 {
		summary.BestTrade = decimal.Zero
		summary.WorstTrade = decimal.Zero
	}
	return summary, nil
}

func (s *TradingLifecycleStore) GetRealizedReturnSeries(
	ctx context.Context,
	chatID string,
	exchange string,
	since time.Time,
) ([]float64, error) {
	if since.IsZero() {
		since = time.Now().UTC().Add(-24 * time.Hour)
	}
	query := `
		SELECT realized_pnl, entry_price, filled_amount
		FROM realized_pnl_journal
		WHERE closed_at >= $1
	`
	args := []interface{}{since.UTC()}
	if strings.TrimSpace(chatID) != "" {
		query += fmt.Sprintf(" AND COALESCE(chat_id, '') = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(chatID))
	}
	if strings.TrimSpace(exchange) != "" {
		query += fmt.Sprintf(" AND exchange = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(exchange))
	}
	query += " ORDER BY closed_at ASC"

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query realized return series failed: %w", err)
	}
	defer rows.Close()

	series := make([]float64, 0, 64)
	for rows.Next() {
		var pnl decimal.Decimal
		var entry decimal.Decimal
		var filled decimal.Decimal
		if err := rows.Scan(&pnl, &entry, &filled); err != nil {
			return nil, fmt.Errorf("scan realized return row failed: %w", err)
		}

		notional := entry.Abs().Mul(filled.Abs())
		if notional.LessThanOrEqual(decimal.Zero) {
			continue
		}

		ret := pnl.Div(notional).InexactFloat64()
		if math.IsNaN(ret) || math.IsInf(ret, 0) {
			continue
		}
		series = append(series, ret)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realized return rows failed: %w", err)
	}
	return series, nil
}

func parseLifecycleTimestamp(raw interface{}) time.Time {
	switch value := raw.(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return value.UTC()
	case string:
		return parseLifecycleTimestampString(value)
	case []byte:
		return parseLifecycleTimestampString(string(value))
	default:
		return parseLifecycleTimestampString(fmt.Sprintf("%v", value))
	}
}

func parseLifecycleTimestampString(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "<nil>" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func (s *TradingLifecycleStore) UpdatePositionProtection(
	ctx context.Context,
	positionID string,
	stopLoss decimal.Decimal,
	takeProfit decimal.Decimal,
	lastPrice decimal.Decimal,
	unrealizedPnL decimal.Decimal,
	updatedAt time.Time,
) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("position_id is required")
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	var existing ManagedOpenPosition
	var protectionRaw interface{}
	var openedRaw interface{}
	var updatedRaw interface{}
	if err := s.db.QueryRow(ctx, `
		SELECT
			position_id, order_id, COALESCE(chat_id, ''), exchange, symbol, side, market_type,
			COALESCE(source, 'autonomous'),
			size, entry_price, COALESCE(stop_loss, 0), COALESCE(take_profit, 0),
			COALESCE(last_price, 0), COALESCE(unrealized_pnl, 0),
			COALESCE(protection_updated_at, opened_at), opened_at, updated_at
		FROM trading_positions
		WHERE position_id = $1
	`, positionID).Scan(
		&existing.PositionID,
		&existing.OrderID,
		&existing.ChatID,
		&existing.Exchange,
		&existing.Symbol,
		&existing.Side,
		&existing.MarketType,
		&existing.Source,
		&existing.Size,
		&existing.EntryPrice,
		&existing.StopLoss,
		&existing.TakeProfit,
		&existing.LastPrice,
		&existing.UnrealizedPnL,
		&protectionRaw,
		&openedRaw,
		&updatedRaw,
	); err != nil {
		return fmt.Errorf("load position for protection update failed: %w", err)
	}
	existing.ProtectionUpdatedAt = parseLifecycleTimestamp(protectionRaw)
	existing.OpenedAt = parseLifecycleTimestamp(openedRaw)
	existing.UpdatedAt = parseLifecycleTimestamp(updatedRaw)
	if strings.TrimSpace(existing.OrderID) == "" {
		existing.OrderID = positionID
	}
	if strings.TrimSpace(existing.MarketType) == "" {
		existing.MarketType = "futures"
	}
	if existing.OpenedAt.IsZero() {
		existing.OpenedAt = updatedAt
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO trading_positions (
			position_id, order_id, chat_id, exchange, symbol, side, market_type,
			size, entry_price, stop_loss, take_profit, last_price, unrealized_pnl,
			protection_updated_at, status, source, opened_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'open',$15,$16,$17)
		ON CONFLICT(position_id) DO UPDATE SET
			stop_loss = EXCLUDED.stop_loss,
			take_profit = EXCLUDED.take_profit,
			last_price = EXCLUDED.last_price,
			unrealized_pnl = EXCLUDED.unrealized_pnl,
			protection_updated_at = EXCLUDED.protection_updated_at,
			updated_at = EXCLUDED.updated_at
	`, positionID, existing.OrderID, existing.ChatID, existing.Exchange, existing.Symbol, existing.Side, existing.MarketType,
		existing.Size, existing.EntryPrice, stopLoss, takeProfit, lastPrice, unrealizedPnL, updatedAt.UTC(),
		normalizeLifecycleSource(existing.Source), existing.OpenedAt.UTC(), updatedAt.UTC()); err != nil {
		return fmt.Errorf("upsert position protection failed: %w", err)
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
