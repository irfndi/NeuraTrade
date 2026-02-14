package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// PaperTradeRecorder records paper trades to the database.
type PaperTradeRecorder struct {
	db     DBPool
	Logger Logger
}

// PaperTrade represents a recorded paper trade.
type PaperTrade struct {
	ID         int64           `json:"id" db:"id"`
	UserID     string          `json:"user_id" db:"user_id"`
	QuestID    *int64          `json:"quest_id" db:"quest_id"`
	StrategyID string          `json:"strategy_id" db:"strategy_id"`
	Exchange   string          `json:"exchange" db:"exchange"`
	Symbol     string          `json:"symbol" db:"symbol"`
	Side       string          `json:"side" db:"side"` // "buy" or "sell"
	EntryPrice decimal.Decimal `json:"entry_price" db:"entry_price"`
	ExitPrice  decimal.Decimal `json:"exit_price" db:"exit_price"`
	Size       decimal.Decimal `json:"size" db:"size"`
	Fees       decimal.Decimal `json:"fees" db:"fees"`
	PnL        decimal.Decimal `json:"pnl" db:"pnl"`
	CostBasis  decimal.Decimal `json:"cost_basis" db:"cost_basis"`
	Status     string          `json:"status" db:"status"` // "open", "closed", "cancelled"
	OpenedAt   time.Time       `json:"opened_at" db:"opened_at"`
	ClosedAt   *time.Time      `json:"closed_at" db:"closed_at"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at" db:"updated_at"`
}

// NewPaperTradeRecorder creates a new paper trade recorder.
func NewPaperTradeRecorder(db DBPool, logger Logger) *PaperTradeRecorder {
	return &PaperTradeRecorder{
		db:     db,
		Logger: logger,
	}
}

// RecordOpenTrade records a new open paper trade.
func (r *PaperTradeRecorder) RecordOpenTrade(ctx context.Context, trade *PaperTrade) (*PaperTrade, error) {
	if trade.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if trade.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if trade.Side != "buy" && trade.Side != "sell" {
		return nil, fmt.Errorf("side must be 'buy' or 'sell'")
	}
	if trade.Size.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("size must be greater than zero")
	}

	query := `
		INSERT INTO paper_trades (user_id, quest_id, strategy_id, exchange, symbol, side, entry_price, size, fees, cost_basis, status, opened_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'open', NOW())
		RETURNING id, user_id, quest_id, strategy_id, exchange, symbol, side, entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at, created_at, updated_at
	`

	var result PaperTrade
	err := r.db.QueryRow(ctx, query,
		trade.UserID,
		trade.QuestID,
		trade.StrategyID,
		trade.Exchange,
		trade.Symbol,
		trade.Side,
		trade.EntryPrice,
		trade.Size,
		trade.Fees,
		trade.CostBasis,
	).Scan(
		&result.ID,
		&result.UserID,
		&result.QuestID,
		&result.StrategyID,
		&result.Exchange,
		&result.Symbol,
		&result.Side,
		&result.EntryPrice,
		&result.ExitPrice,
		&result.Size,
		&result.Fees,
		&result.PnL,
		&result.CostBasis,
		&result.Status,
		&result.OpenedAt,
		&result.ClosedAt,
		&result.CreatedAt,
		&result.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to record open trade: %w", err)
	}

	r.Logger.WithFields(map[string]interface{}{
		"trade_id": result.ID,
		"user_id":  result.UserID,
		"symbol":   result.Symbol,
		"side":     result.Side,
		"size":     result.Size.String(),
		"price":    result.EntryPrice.String(),
	}).Info("Paper trade opened")

	return &result, nil
}

// RecordCloseTrade closes an existing open paper trade and calculates PnL.
func (r *PaperTradeRecorder) RecordCloseTrade(ctx context.Context, tradeID int64, exitPrice, fees decimal.Decimal) (*PaperTrade, error) {
	// First get the existing trade to calculate PnL
	trade, err := r.GetTrade(ctx, tradeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}
	if trade == nil {
		return nil, fmt.Errorf("trade not found: %d", tradeID)
	}
	if trade.Status != "open" {
		return nil, fmt.Errorf("trade is not open: status=%s", trade.Status)
	}

	// Calculate PnL based on side
	var pnl decimal.Decimal
	_ = trade.CostBasis // preserved for future use

	if trade.Side == "buy" {
		// Long position: PnL = (exit_price - entry_price) * size - fees
		pnl = exitPrice.Sub(trade.EntryPrice).Mul(trade.Size).Sub(fees)
	} else {
		// Short position: PnL = (entry_price - exit_price) * size - fees
		pnl = trade.EntryPrice.Sub(exitPrice).Mul(trade.Size).Sub(fees)
	}

	now := time.Now()
	query := `
		UPDATE paper_trades
		SET exit_price = $1, fees = fees + $2, pnl = $3, status = 'closed', closed_at = $4, updated_at = NOW()
		WHERE id = $5 AND status = 'open'
		RETURNING id, user_id, quest_id, strategy_id, exchange, symbol, side, entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at, created_at, updated_at
	`

	var result PaperTrade
	err = r.db.QueryRow(ctx, query, exitPrice, fees, pnl, now, tradeID).Scan(
		&result.ID,
		&result.UserID,
		&result.QuestID,
		&result.StrategyID,
		&result.Exchange,
		&result.Symbol,
		&result.Side,
		&result.EntryPrice,
		&result.ExitPrice,
		&result.Size,
		&result.Fees,
		&result.PnL,
		&result.CostBasis,
		&result.Status,
		&result.OpenedAt,
		&result.ClosedAt,
		&result.CreatedAt,
		&result.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("trade already closed or cancelled: %d", tradeID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to record close trade: %w", err)
	}

	r.Logger.WithFields(map[string]interface{}{
		"trade_id": result.ID,
		"user_id":  result.UserID,
		"symbol":   result.Symbol,
		"pnl":      result.PnL.String(),
	}).Info("Paper trade closed")

	return &result, nil
}

// GetTrade retrieves a paper trade by ID.
func (r *PaperTradeRecorder) GetTrade(ctx context.Context, tradeID int64) (*PaperTrade, error) {
	query := `
		SELECT id, user_id, quest_id, strategy_id, exchange, symbol, side, entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at, created_at, updated_at
		FROM paper_trades
		WHERE id = $1
	`

	var trade PaperTrade
	err := r.db.QueryRow(ctx, query, tradeID).Scan(
		&trade.ID,
		&trade.UserID,
		&trade.QuestID,
		&trade.StrategyID,
		&trade.Exchange,
		&trade.Symbol,
		&trade.Side,
		&trade.EntryPrice,
		&trade.ExitPrice,
		&trade.Size,
		&trade.Fees,
		&trade.PnL,
		&trade.CostBasis,
		&trade.Status,
		&trade.OpenedAt,
		&trade.ClosedAt,
		&trade.CreatedAt,
		&trade.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}

	return &trade, nil
}

// GetOpenTrades retrieves all open trades for a user.
func (r *PaperTradeRecorder) GetOpenTrades(ctx context.Context, userID string) ([]*PaperTrade, error) {
	query := `
		SELECT id, user_id, quest_id, strategy_id, exchange, symbol, side, entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at, created_at, updated_at
		FROM paper_trades
		WHERE user_id = $1 AND status = 'open'
		ORDER BY opened_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get open trades: %w", err)
	}
	defer rows.Close()

	var trades []*PaperTrade
	for rows.Next() {
		var trade PaperTrade
		err := rows.Scan(
			&trade.ID,
			&trade.UserID,
			&trade.QuestID,
			&trade.StrategyID,
			&trade.Exchange,
			&trade.Symbol,
			&trade.Side,
			&trade.EntryPrice,
			&trade.ExitPrice,
			&trade.Size,
			&trade.Fees,
			&trade.PnL,
			&trade.CostBasis,
			&trade.Status,
			&trade.OpenedAt,
			&trade.ClosedAt,
			&trade.CreatedAt,
			&trade.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan trade: %w", err)
		}
		trades = append(trades, &trade)
	}

	return trades, nil
}

// GetTradeHistory retrieves closed trades for a user within a date range.
func (r *PaperTradeRecorder) GetTradeHistory(ctx context.Context, userID string, from, to time.Time) ([]*PaperTrade, error) {
	query := `
		SELECT id, user_id, quest_id, strategy_id, exchange, symbol, side, entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at, created_at, updated_at
		FROM paper_trades
		WHERE user_id = $1 AND status = 'closed' AND closed_at BETWEEN $2 AND $3
		ORDER BY closed_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get trade history: %w", err)
	}
	defer rows.Close()

	var trades []*PaperTrade
	for rows.Next() {
		var trade PaperTrade
		err := rows.Scan(
			&trade.ID,
			&trade.UserID,
			&trade.QuestID,
			&trade.StrategyID,
			&trade.Exchange,
			&trade.Symbol,
			&trade.Side,
			&trade.EntryPrice,
			&trade.ExitPrice,
			&trade.Size,
			&trade.Fees,
			&trade.PnL,
			&trade.CostBasis,
			&trade.Status,
			&trade.OpenedAt,
			&trade.ClosedAt,
			&trade.CreatedAt,
			&trade.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan trade: %w", err)
		}
		trades = append(trades, &trade)
	}

	return trades, nil
}

// GetUserSummary returns a summary of paper trading performance for a user.
func (r *PaperTradeRecorder) GetUserSummary(ctx context.Context, userID string) (*PaperTradeSummary, error) {
	query := `
		SELECT 
			COUNT(*) as total_trades,
			COUNT(CASE WHEN pnl > 0 THEN 1 END) as winning_trades,
			COUNT(CASE WHEN pnl < 0 THEN 1 END) as losing_trades,
			COALESCE(SUM(pnl), 0) as total_pnl,
			COALESCE(AVG(pnl), 0) as avg_pnl,
			COALESCE(SUM(fees), 0) as total_fees
		FROM paper_trades
		WHERE user_id = $1 AND status = 'closed'
	`

	var summary PaperTradeSummary
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&summary.TotalTrades,
		&summary.WinningTrades,
		&summary.LosingTrades,
		&summary.TotalPnL,
		&summary.AveragePnL,
		&summary.TotalFees,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get user summary: %w", err)
	}

	if summary.TotalTrades > 0 {
		summary.WinRate = decimal.NewFromInt(summary.WinningTrades).
			Div(decimal.NewFromInt(summary.TotalTrades)).
			Mul(decimal.NewFromInt(100))
	}

	return &summary, nil
}

// PaperTradeSummary represents a summary of paper trading performance.
type PaperTradeSummary struct {
	TotalTrades   int64           `json:"total_trades"`
	WinningTrades int64           `json:"winning_trades"`
	LosingTrades  int64           `json:"losing_trades"`
	WinRate       decimal.Decimal `json:"win_rate"`
	TotalPnL      decimal.Decimal `json:"total_pnl"`
	AveragePnL    decimal.Decimal `json:"average_pnl"`
	TotalFees     decimal.Decimal `json:"total_fees"`
}

// CancelTrade cancels an open paper trade.
func (r *PaperTradeRecorder) CancelTrade(ctx context.Context, tradeID int64) (*PaperTrade, error) {
	query := `
		UPDATE paper_trades
		SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND status = 'open'
		RETURNING id, user_id, quest_id, strategy_id, exchange, symbol, side, entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at, created_at, updated_at
	`

	var result PaperTrade
	err := r.db.QueryRow(ctx, query, tradeID).Scan(
		&result.ID,
		&result.UserID,
		&result.QuestID,
		&result.StrategyID,
		&result.Exchange,
		&result.Symbol,
		&result.Side,
		&result.EntryPrice,
		&result.ExitPrice,
		&result.Size,
		&result.Fees,
		&result.PnL,
		&result.CostBasis,
		&result.Status,
		&result.OpenedAt,
		&result.ClosedAt,
		&result.CreatedAt,
		&result.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("trade not found or already closed: %d", tradeID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to cancel trade: %w", err)
	}

	r.Logger.WithFields(map[string]interface{}{
		"trade_id": result.ID,
		"user_id":  result.UserID,
	}).Info("Paper trade cancelled")

	return &result, nil
}
