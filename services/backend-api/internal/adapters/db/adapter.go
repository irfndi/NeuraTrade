// Package db provides an adapter that wraps the existing database
// to implement the ports.StateStore interface.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// Adapter wraps the existing database to implement StateStore.
type Adapter struct {
	db database.Database
}

// NewAdapter creates a new database adapter.
func NewAdapter(db database.Database) *Adapter {
	return &Adapter{db: db}
}

// ============================================================
// StateStore Implementation
// ============================================================

// Positions returns the positions repository.
func (a *Adapter) Positions() ports.PositionsRepository {
	return &positionsRepo{db: a.db}
}

// Orders returns the orders repository.
func (a *Adapter) Orders() ports.OrdersRepository {
	return &ordersRepo{db: a.db}
}

// Trades returns the trades repository.
func (a *Adapter) Trades() ports.TradesRepository {
	return &tradesRepo{db: a.db}
}

// Signals returns the signals repository.
func (a *Adapter) Signals() ports.SignalsRepository {
	return &signalsRepo{db: a.db}
}

// Config returns the config repository.
func (a *Adapter) Config() ports.ConfigRepository {
	return &configRepo{db: a.db}
}

// Health checks if the database is healthy.
func (a *Adapter) Health(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return a.db.HealthCheck(ctx)
}

// ============================================================
// Positions Repository
// ============================================================

type positionsRepo struct {
	db database.Database
}

func (r *positionsRepo) Create(ctx context.Context, pos ports.StoredPosition) (ports.StoredPosition, error) {
	metadata, _ := json.Marshal(pos.Metadata)
	query := `
		INSERT INTO positions (id, exchange, symbol, side, amount, entry_price, current_price,
			unrealized_pnl, realized_pnl, opened_at, updated_at, closed_at, status, strategy_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err := r.db.Exec(ctx, query,
		pos.ID, pos.Exchange, pos.Symbol, pos.Side, pos.Amount, pos.EntryPrice, pos.CurrentPrice,
		pos.UnrealizedPnL, pos.RealizedPnL, pos.OpenedAt, pos.UpdatedAt, pos.ClosedAt, pos.Status, pos.StrategyID, metadata,
	)
	if err != nil {
		return ports.StoredPosition{}, fmt.Errorf("failed to create position: %w", err)
	}
	return pos, nil
}

func (r *positionsRepo) Update(ctx context.Context, pos ports.StoredPosition) error {
	metadata, _ := json.Marshal(pos.Metadata)
	query := `
		UPDATE positions SET exchange = $1, symbol = $2, side = $3, amount = $4, entry_price = $5,
			current_price = $6, unrealized_pnl = $7, realized_pnl = $8, updated_at = $9,
			closed_at = $10, status = $11, strategy_id = $12, metadata = $13
		WHERE id = $14
	`
	_, err := r.db.Exec(ctx, query,
		pos.Exchange, pos.Symbol, pos.Side, pos.Amount, pos.EntryPrice, pos.CurrentPrice,
		pos.UnrealizedPnL, pos.RealizedPnL, pos.UpdatedAt, pos.ClosedAt, pos.Status, pos.StrategyID, metadata, pos.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update position: %w", err)
	}
	return nil
}

func (r *positionsRepo) GetByID(ctx context.Context, id string) (ports.StoredPosition, error) {
	query := `
		SELECT id, exchange, symbol, side, amount, entry_price, current_price,
			unrealized_pnl, realized_pnl, opened_at, updated_at, closed_at, status, strategy_id, metadata
		FROM positions WHERE id = $1
	`
	var pos ports.StoredPosition
	var metadata []byte
	var closedAt sql.NullTime
	err := r.db.QueryRow(ctx, query, id).Scan(
		&pos.ID, &pos.Exchange, &pos.Symbol, &pos.Side, &pos.Amount, &pos.EntryPrice, &pos.CurrentPrice,
		&pos.UnrealizedPnL, &pos.RealizedPnL, &pos.OpenedAt, &pos.UpdatedAt, &closedAt, &pos.Status, &pos.StrategyID, &metadata,
	)
	if err != nil {
		return ports.StoredPosition{}, fmt.Errorf("failed to get position: %w", err)
	}
	if closedAt.Valid {
		pos.ClosedAt = &closedAt.Time
	}
	_ = json.Unmarshal(metadata, &pos.Metadata)
	return pos, nil
}

func (r *positionsRepo) GetOpen(ctx context.Context) ([]ports.StoredPosition, error) {
	query := `
		SELECT id, exchange, symbol, side, amount, entry_price, current_price,
			unrealized_pnl, realized_pnl, opened_at, updated_at, closed_at, status, strategy_id, metadata
		FROM positions WHERE status = 'open'
	`
	return r.queryPositions(ctx, query)
}

func (r *positionsRepo) GetOpenBySymbol(ctx context.Context, exchange, symbol string) ([]ports.StoredPosition, error) {
	query := `
		SELECT id, exchange, symbol, side, amount, entry_price, current_price,
			unrealized_pnl, realized_pnl, opened_at, updated_at, closed_at, status, strategy_id, metadata
		FROM positions WHERE status = 'open' AND exchange = $1 AND symbol = $2
	`
	return r.queryPositions(ctx, query, exchange, symbol)
}

func (r *positionsRepo) Close(ctx context.Context, id string, closedAt time.Time, realizedPnL decimal.Decimal) error {
	query := `
		UPDATE positions SET status = 'closed', closed_at = $1, realized_pnl = $2, updated_at = $3
		WHERE id = $4
	`
	_, err := r.db.Exec(ctx, query, closedAt, realizedPnL, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to close position: %w", err)
	}
	return nil
}

func (r *positionsRepo) queryPositions(ctx context.Context, query string, args ...interface{}) ([]ports.StoredPosition, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query positions: %w", err)
	}
	defer rows.Close()

	var positions []ports.StoredPosition
	for rows.Next() {
		var pos ports.StoredPosition
		var metadata []byte
		var closedAt sql.NullTime
		err := rows.Scan(
			&pos.ID, &pos.Exchange, &pos.Symbol, &pos.Side, &pos.Amount, &pos.EntryPrice, &pos.CurrentPrice,
			&pos.UnrealizedPnL, &pos.RealizedPnL, &pos.OpenedAt, &pos.UpdatedAt, &closedAt, &pos.Status, &pos.StrategyID, &metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan position: %w", err)
		}
		if closedAt.Valid {
			pos.ClosedAt = &closedAt.Time
		}
		_ = json.Unmarshal(metadata, &pos.Metadata)
		positions = append(positions, pos)
	}
	return positions, nil
}

// ============================================================
// Orders Repository
// ============================================================

type ordersRepo struct {
	db database.Database
}

func (r *ordersRepo) Create(ctx context.Context, order ports.StoredOrder) (ports.StoredOrder, error) {
	metadata, _ := json.Marshal(order.Metadata)
	query := `
		INSERT INTO orders (id, exchange, symbol, exchange_order_id, client_order_id, side, type,
			amount, filled_amount, price, average_price, status, strategy_id, signal_id,
			created_at, updated_at, closed_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`
	_, err := r.db.Exec(ctx, query,
		order.ID, order.Exchange, order.Symbol, order.ExchangeOrderID, order.ClientOrderID, order.Side, order.Type,
		order.Amount, order.FilledAmount, order.Price, order.AveragePrice, order.Status, order.StrategyID, order.SignalID,
		order.CreatedAt, order.UpdatedAt, order.ClosedAt, metadata,
	)
	if err != nil {
		return ports.StoredOrder{}, fmt.Errorf("failed to create order: %w", err)
	}
	return order, nil
}

func (r *ordersRepo) Update(ctx context.Context, order ports.StoredOrder) error {
	metadata, _ := json.Marshal(order.Metadata)
	query := `
		UPDATE orders SET exchange = $1, symbol = $2, exchange_order_id = $3, client_order_id = $4,
			side = $5, type = $6, amount = $7, filled_amount = $8, price = $9, average_price = $10,
			status = $11, strategy_id = $12, signal_id = $13, updated_at = $14, closed_at = $15, metadata = $16
		WHERE id = $17
	`
	_, err := r.db.Exec(ctx, query,
		order.Exchange, order.Symbol, order.ExchangeOrderID, order.ClientOrderID, order.Side, order.Type,
		order.Amount, order.FilledAmount, order.Price, order.AveragePrice, order.Status, order.StrategyID, order.SignalID,
		order.UpdatedAt, order.ClosedAt, metadata, order.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}
	return nil
}

func (r *ordersRepo) GetByID(ctx context.Context, id string) (ports.StoredOrder, error) {
	query := `
		SELECT id, exchange, symbol, exchange_order_id, client_order_id, side, type,
			amount, filled_amount, price, average_price, status, strategy_id, signal_id,
			created_at, updated_at, closed_at, metadata
		FROM orders WHERE id = $1
	`
	return r.scanOrder(r.db.QueryRow(ctx, query, id))
}

func (r *ordersRepo) GetByExchangeOrderID(ctx context.Context, exchange, exchangeOrderID string) (ports.StoredOrder, error) {
	query := `
		SELECT id, exchange, symbol, exchange_order_id, client_order_id, side, type,
			amount, filled_amount, price, average_price, status, strategy_id, signal_id,
			created_at, updated_at, closed_at, metadata
		FROM orders WHERE exchange = $1 AND exchange_order_id = $2
	`
	return r.scanOrder(r.db.QueryRow(ctx, query, exchange, exchangeOrderID))
}

func (r *ordersRepo) GetByClientOrderID(ctx context.Context, exchange, clientOrderID string) (ports.StoredOrder, error) {
	query := `
		SELECT id, exchange, symbol, exchange_order_id, client_order_id, side, type,
			amount, filled_amount, price, average_price, status, strategy_id, signal_id,
			created_at, updated_at, closed_at, metadata
		FROM orders WHERE exchange = $1 AND client_order_id = $2
	`
	return r.scanOrder(r.db.QueryRow(ctx, query, exchange, clientOrderID))
}

func (r *ordersRepo) GetOpen(ctx context.Context) ([]ports.StoredOrder, error) {
	query := `
		SELECT id, exchange, symbol, exchange_order_id, client_order_id, side, type,
			amount, filled_amount, price, average_price, status, strategy_id, signal_id,
			created_at, updated_at, closed_at, metadata
		FROM orders WHERE status IN ('open', 'pending', 'partially_filled')
		ORDER BY created_at DESC
	`
	return r.queryOrders(ctx, query)
}

func (r *ordersRepo) GetRecent(ctx context.Context, limit int) ([]ports.StoredOrder, error) {
	query := `
		SELECT id, exchange, symbol, exchange_order_id, client_order_id, side, type,
			amount, filled_amount, price, average_price, status, strategy_id, signal_id,
			created_at, updated_at, closed_at, metadata
		FROM orders ORDER BY created_at DESC LIMIT $1
	`
	return r.queryOrders(ctx, query, limit)
}

func (r *ordersRepo) scanOrder(row database.Row) (ports.StoredOrder, error) {
	var order ports.StoredOrder
	var metadata []byte
	var closedAt sql.NullTime
	err := row.Scan(
		&order.ID, &order.Exchange, &order.Symbol, &order.ExchangeOrderID, &order.ClientOrderID, &order.Side, &order.Type,
		&order.Amount, &order.FilledAmount, &order.Price, &order.AveragePrice, &order.Status, &order.StrategyID, &order.SignalID,
		&order.CreatedAt, &order.UpdatedAt, &closedAt, &metadata,
	)
	if err != nil {
		return ports.StoredOrder{}, fmt.Errorf("failed to scan order: %w", err)
	}
	if closedAt.Valid {
		order.ClosedAt = &closedAt.Time
	}
	_ = json.Unmarshal(metadata, &order.Metadata)
	return order, nil
}

func (r *ordersRepo) queryOrders(ctx context.Context, query string, args ...interface{}) ([]ports.StoredOrder, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query orders: %w", err)
	}
	defer rows.Close()

	var orders []ports.StoredOrder
	for rows.Next() {
		var order ports.StoredOrder
		var metadata []byte
		var closedAt sql.NullTime
		err := rows.Scan(
			&order.ID, &order.Exchange, &order.Symbol, &order.ExchangeOrderID, &order.ClientOrderID, &order.Side, &order.Type,
			&order.Amount, &order.FilledAmount, &order.Price, &order.AveragePrice, &order.Status, &order.StrategyID, &order.SignalID,
			&order.CreatedAt, &order.UpdatedAt, &closedAt, &metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}
		if closedAt.Valid {
			order.ClosedAt = &closedAt.Time
		}
		_ = json.Unmarshal(metadata, &order.Metadata)
		orders = append(orders, order)
	}
	return orders, nil
}

// ============================================================
// Trades Repository
// ============================================================

type tradesRepo struct {
	db database.Database
}

func (r *tradesRepo) Create(ctx context.Context, trade ports.StoredTrade) (ports.StoredTrade, error) {
	metadata, _ := json.Marshal(trade.Metadata)
	query := `
		INSERT INTO trades (id, exchange, symbol, order_id, exchange_order_id, side, amount,
			price, fee, fee_currency, pnl, position_id, executed_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := r.db.Exec(ctx, query,
		trade.ID, trade.Exchange, trade.Symbol, trade.OrderID, trade.ExchangeOrderID, trade.Side, trade.Amount,
		trade.Price, trade.Fee, trade.FeeCurrency, trade.PnL, trade.PositionID, trade.ExecutedAt, metadata,
	)
	if err != nil {
		return ports.StoredTrade{}, fmt.Errorf("failed to create trade: %w", err)
	}
	return trade, nil
}

func (r *tradesRepo) GetByID(ctx context.Context, id string) (ports.StoredTrade, error) {
	query := `
		SELECT id, exchange, symbol, order_id, exchange_order_id, side, amount,
			price, fee, fee_currency, pnl, position_id, executed_at, metadata
		FROM trades WHERE id = $1
	`
	return r.scanTrade(r.db.QueryRow(ctx, query, id))
}

func (r *tradesRepo) GetByOrderID(ctx context.Context, orderID string) ([]ports.StoredTrade, error) {
	query := `
		SELECT id, exchange, symbol, order_id, exchange_order_id, side, amount,
			price, fee, fee_currency, pnl, position_id, executed_at, metadata
		FROM trades WHERE order_id = $1 ORDER BY executed_at
	`
	return r.queryTrades(ctx, query, orderID)
}

func (r *tradesRepo) GetRecent(ctx context.Context, limit int) ([]ports.StoredTrade, error) {
	query := `
		SELECT id, exchange, symbol, order_id, exchange_order_id, side, amount,
			price, fee, fee_currency, pnl, position_id, executed_at, metadata
		FROM trades ORDER BY executed_at DESC LIMIT $1
	`
	return r.queryTrades(ctx, query, limit)
}

func (r *tradesRepo) GetByPositionID(ctx context.Context, positionID string) ([]ports.StoredTrade, error) {
	query := `
		SELECT id, exchange, symbol, order_id, exchange_order_id, side, amount,
			price, fee, fee_currency, pnl, position_id, executed_at, metadata
		FROM trades WHERE position_id = $1 ORDER BY executed_at
	`
	return r.queryTrades(ctx, query, positionID)
}

func (r *tradesRepo) scanTrade(row database.Row) (ports.StoredTrade, error) {
	var trade ports.StoredTrade
	var metadata []byte
	err := row.Scan(
		&trade.ID, &trade.Exchange, &trade.Symbol, &trade.OrderID, &trade.ExchangeOrderID, &trade.Side, &trade.Amount,
		&trade.Price, &trade.Fee, &trade.FeeCurrency, &trade.PnL, &trade.PositionID, &trade.ExecutedAt, &metadata,
	)
	if err != nil {
		return ports.StoredTrade{}, fmt.Errorf("failed to scan trade: %w", err)
	}
	_ = json.Unmarshal(metadata, &trade.Metadata)
	return trade, nil
}

func (r *tradesRepo) queryTrades(ctx context.Context, query string, args ...interface{}) ([]ports.StoredTrade, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query trades: %w", err)
	}
	defer rows.Close()

	var trades []ports.StoredTrade
	for rows.Next() {
		var trade ports.StoredTrade
		var metadata []byte
		err := rows.Scan(
			&trade.ID, &trade.Exchange, &trade.Symbol, &trade.OrderID, &trade.ExchangeOrderID, &trade.Side, &trade.Amount,
			&trade.Price, &trade.Fee, &trade.FeeCurrency, &trade.PnL, &trade.PositionID, &trade.ExecutedAt, &metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan trade: %w", err)
		}
		_ = json.Unmarshal(metadata, &trade.Metadata)
		trades = append(trades, trade)
	}
	return trades, nil
}

// ============================================================
// Signals Repository
// ============================================================

type signalsRepo struct {
	db database.Database
}

func (r *signalsRepo) Create(ctx context.Context, signal ports.StoredSignal) (ports.StoredSignal, error) {
	metadata, _ := json.Marshal(signal.Metadata)
	query := `
		INSERT INTO signals (id, exchange, symbol, strategy_id, side, confidence, price,
			stop_loss, take_profit, status, generated_at, processed_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err := r.db.Exec(ctx, query,
		signal.ID, signal.Exchange, signal.Symbol, signal.StrategyID, signal.Side, signal.Confidence, signal.Price,
		signal.StopLoss, signal.TakeProfit, signal.Status, signal.GeneratedAt, signal.ProcessedAt, metadata,
	)
	if err != nil {
		return ports.StoredSignal{}, fmt.Errorf("failed to create signal: %w", err)
	}
	return signal, nil
}

func (r *signalsRepo) Update(ctx context.Context, signal ports.StoredSignal) error {
	metadata, _ := json.Marshal(signal.Metadata)
	query := `
		UPDATE signals SET exchange = $1, symbol = $2, strategy_id = $3, side = $4, confidence = $5,
			price = $6, stop_loss = $7, take_profit = $8, status = $9, processed_at = $10, metadata = $11
		WHERE id = $12
	`
	_, err := r.db.Exec(ctx, query,
		signal.Exchange, signal.Symbol, signal.StrategyID, signal.Side, signal.Confidence, signal.Price,
		signal.StopLoss, signal.TakeProfit, signal.Status, signal.ProcessedAt, metadata, signal.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update signal: %w", err)
	}
	return nil
}

func (r *signalsRepo) GetByID(ctx context.Context, id string) (ports.StoredSignal, error) {
	query := `
		SELECT id, exchange, symbol, strategy_id, side, confidence, price,
			stop_loss, take_profit, status, generated_at, processed_at, metadata
		FROM signals WHERE id = $1
	`
	return r.scanSignal(r.db.QueryRow(ctx, query, id))
}

func (r *signalsRepo) GetPending(ctx context.Context, limit int) ([]ports.StoredSignal, error) {
	query := `
		SELECT id, exchange, symbol, strategy_id, side, confidence, price,
			stop_loss, take_profit, status, generated_at, processed_at, metadata
		FROM signals WHERE status = 'pending' ORDER BY generated_at LIMIT $1
	`
	return r.querySignals(ctx, query, limit)
}

func (r *signalsRepo) MarkProcessed(ctx context.Context, id string, processedAt time.Time) error {
	query := `UPDATE signals SET status = 'processed', processed_at = $1 WHERE id = $2`
	_, err := r.db.Exec(ctx, query, processedAt, id)
	if err != nil {
		return fmt.Errorf("failed to mark signal processed: %w", err)
	}
	return nil
}

func (r *signalsRepo) scanSignal(row database.Row) (ports.StoredSignal, error) {
	var signal ports.StoredSignal
	var metadata []byte
	err := row.Scan(
		&signal.ID, &signal.Exchange, &signal.Symbol, &signal.StrategyID, &signal.Side, &signal.Confidence, &signal.Price,
		&signal.StopLoss, &signal.TakeProfit, &signal.Status, &signal.GeneratedAt, &signal.ProcessedAt, &metadata,
	)
	if err != nil {
		return ports.StoredSignal{}, fmt.Errorf("failed to scan signal: %w", err)
	}
	_ = json.Unmarshal(metadata, &signal.Metadata)
	return signal, nil
}

func (r *signalsRepo) querySignals(ctx context.Context, query string, args ...interface{}) ([]ports.StoredSignal, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query signals: %w", err)
	}
	defer rows.Close()

	var signals []ports.StoredSignal
	for rows.Next() {
		var signal ports.StoredSignal
		var metadata []byte
		err := rows.Scan(
			&signal.ID, &signal.Exchange, &signal.Symbol, &signal.StrategyID, &signal.Side, &signal.Confidence, &signal.Price,
			&signal.StopLoss, &signal.TakeProfit, &signal.Status, &signal.GeneratedAt, &signal.ProcessedAt, &metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan signal: %w", err)
		}
		_ = json.Unmarshal(metadata, &signal.Metadata)
		signals = append(signals, signal)
	}
	return signals, nil
}

// ============================================================
// Config Repository
// ============================================================

type configRepo struct {
	db database.Database
}

func (r *configRepo) GetStrategyConfig(ctx context.Context, strategyID string) (ports.StrategyConfig, error) {
	query := `SELECT id, name, enabled, config, updated_at FROM strategy_configs WHERE id = $1`
	var cfg ports.StrategyConfig
	var configJSON []byte
	err := r.db.QueryRow(ctx, query, strategyID).Scan(&cfg.ID, &cfg.Name, &cfg.Enabled, &configJSON, &cfg.UpdatedAt)
	if err != nil {
		return ports.StrategyConfig{}, fmt.Errorf("failed to get strategy config: %w", err)
	}
	_ = json.Unmarshal(configJSON, &cfg.Config)
	return cfg, nil
}

func (r *configRepo) UpdateStrategyConfig(ctx context.Context, config ports.StrategyConfig) error {
	configJSON, _ := json.Marshal(config.Config)
	query := `INSERT OR REPLACE INTO strategy_configs (id, name, enabled, config, updated_at) VALUES ($1, $2, $3, $4, $5)`
	_, err := r.db.Exec(ctx, query, config.ID, config.Name, config.Enabled, configJSON, config.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to update strategy config: %w", err)
	}
	return nil
}

// defaultRiskConfig returns the default risk configuration.
func defaultRiskConfig() ports.RiskConfig {
	return ports.RiskConfig{
		MaxPositionSize:  decimal.NewFromFloat(0.1),
		MaxDailyLoss:     decimal.NewFromFloat(0.05),
		MaxDrawdown:      decimal.NewFromFloat(0.1),
		MaxLeverage:      decimal.NewFromInt(2),
		AllowedSymbols:   []string{},
		AllowedExchanges: []string{},
		SafeMode:         false,
		KillSwitch:       false,
	}
}

func (r *configRepo) GetRiskConfig(ctx context.Context) (ports.RiskConfig, error) {
	query := `SELECT max_position_size, max_daily_loss, max_drawdown, max_leverage,
		allowed_symbols, allowed_exchanges, safe_mode, kill_switch FROM risk_config LIMIT 1`
	var cfg ports.RiskConfig
	var symbolsJSON, exchangesJSON []byte
	err := r.db.QueryRow(ctx, query).Scan(
		&cfg.MaxPositionSize, &cfg.MaxDailyLoss, &cfg.MaxDrawdown, &cfg.MaxLeverage,
		&symbolsJSON, &exchangesJSON, &cfg.SafeMode, &cfg.KillSwitch,
	)
	if err != nil {
		// Only return defaults if no config row exists
		if errors.Is(err, sql.ErrNoRows) {
			return defaultRiskConfig(), nil
		}
		return ports.RiskConfig{}, fmt.Errorf("failed to get risk config: %w", err)
	}
	_ = json.Unmarshal(symbolsJSON, &cfg.AllowedSymbols)
	_ = json.Unmarshal(exchangesJSON, &cfg.AllowedExchanges)
	return cfg, nil
}

func (r *configRepo) UpdateRiskConfig(ctx context.Context, config ports.RiskConfig) error {
	symbolsJSON, _ := json.Marshal(config.AllowedSymbols)
	exchangesJSON, _ := json.Marshal(config.AllowedExchanges)
	query := `INSERT OR REPLACE INTO risk_config (id, max_position_size, max_daily_loss, max_drawdown,
		max_leverage, allowed_symbols, allowed_exchanges, safe_mode, kill_switch) VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.db.Exec(ctx, query,
		config.MaxPositionSize, config.MaxDailyLoss, config.MaxDrawdown, config.MaxLeverage,
		symbolsJSON, exchangesJSON, config.SafeMode, config.KillSwitch,
	)
	if err != nil {
		return fmt.Errorf("failed to update risk config: %w", err)
	}
	return nil
}

func (r *configRepo) GetFeatureFlag(ctx context.Context, key string) (bool, error) {
	query := `SELECT value FROM feature_flags WHERE key = $1`
	var value bool
	err := r.db.QueryRow(ctx, query, key).Scan(&value)
	if err != nil {
		// Only return default false if no row exists
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get feature flag: %w", err)
	}
	return value, nil
}

func (r *configRepo) SetFeatureFlag(ctx context.Context, key string, value bool) error {
	query := `INSERT OR REPLACE INTO feature_flags (key, value, updated_at) VALUES ($1, $2, $3)`
	_, err := r.db.Exec(ctx, query, key, value, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set feature flag: %w", err)
	}
	return nil
}
