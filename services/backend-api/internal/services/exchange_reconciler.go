package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

// ReconciliationType defines when reconciliation occurs
type ReconciliationType string

const (
	ReconciliationStartup  ReconciliationType = "startup"
	ReconciliationPeriodic ReconciliationType = "periodic"
	ReconciliationManual   ReconciliationType = "manual"
	ReconciliationDoctor   ReconciliationType = "doctor"
)

// ReconciliationStatus defines the outcome of reconciliation
type ReconciliationStatus string

const (
	ReconciliationSuccess       ReconciliationStatus = "success"
	ReconciliationPartial       ReconciliationStatus = "partial"
	ReconciliationFailed        ReconciliationStatus = "failed"
	ReconciliationDriftDetected ReconciliationStatus = "drift_detected"
)

// ReconciliationResult represents the result of a reconciliation operation
type ReconciliationResult struct {
	Type                ReconciliationType
	Status              ReconciliationStatus
	Exchange            string
	OrdersMatched       int
	OrdersMismatched    int
	OrdersOrphaned      int
	PositionsMatched    int
	PositionsMismatched int
	PositionsOrphaned   int
	BalancesMatched     int
	BalancesMismatched  int
	DriftDetected       bool
	DriftDetails        []DriftDetail
	Timestamp           time.Time
}

// DriftDetail represents a detected drift between local and exchange state
type DriftDetail struct {
	Type          string // "order", "position", "balance"
	Exchange      string
	Symbol        string
	LocalValue    string
	ExchangeValue string
	Severity      string // "low", "medium", "high"
}

// ExchangePositionReconciler reconciles local state with exchange state
type ExchangePositionReconciler struct {
	mu          sync.RWMutex
	db          database.DBPool
	ccxtService ccxt.CCXTService
	logger      *log.Logger
	config      ReconcilerConfig

	// Callbacks for handling drift
	onOrderDrift    func(ctx context.Context, drift DriftDetail) error
	onPositionDrift func(ctx context.Context, drift DriftDetail) error
	onBalanceDrift  func(ctx context.Context, drift DriftDetail) error
}

// ReconcilerConfig holds configuration for the reconciler
type ReconcilerConfig struct {
	// EnableAutoSync automatically syncs local state with exchange
	EnableAutoSync bool
	// ReconciliationInterval is how often to run periodic reconciliation
	ReconciliationInterval time.Duration
	// DriftThreshold is the percentage threshold for drift warnings
	DriftThreshold float64
	// MaxOrphanedOrders is the max number of orphaned orders before alerting
	MaxOrphanedOrders int
}

// DefaultReconcilerConfig returns default configuration
func DefaultReconcilerConfig() ReconcilerConfig {
	return ReconcilerConfig{
		EnableAutoSync:         false, // Default to read-only
		ReconciliationInterval: 5 * time.Minute,
		DriftThreshold:         1.0, // 1% drift triggers warning
		MaxOrphanedOrders:      10,
	}
}

// NewExchangePositionReconciler creates a new reconciler
func NewExchangePositionReconciler(
	db database.DBPool,
	ccxtService ccxt.CCXTService,
	config ReconcilerConfig,
	logger *log.Logger,
) *ExchangePositionReconciler {
	if logger == nil {
		logger = log.Default()
	}
	return &ExchangePositionReconciler{
		db:          db,
		ccxtService: ccxtService,
		config:      config,
		logger:      logger,
	}
}

// ReconcileAll runs reconciliation for all configured exchanges
func (r *ExchangePositionReconciler) ReconcileAll(ctx context.Context, reconType ReconciliationType, chatID string) ([]ReconciliationResult, error) {
	exchanges := r.ccxtService.GetSupportedExchanges()
	results := make([]ReconciliationResult, 0, len(exchanges))

	for _, exchange := range exchanges {
		result, err := r.ReconcileExchange(ctx, exchange, reconType, chatID)
		if err != nil {
			r.logger.Printf("Reconciliation failed for %s: %v", exchange, err)
			result = ReconciliationResult{
				Type:     reconType,
				Status:   ReconciliationFailed,
				Exchange: exchange,
			}
		}
		results = append(results, result)
	}

	return results, nil
}

// ReconcileExchange reconciles state for a specific exchange
func (r *ExchangePositionReconciler) ReconcileExchange(ctx context.Context, exchange string, reconType ReconciliationType, chatID string) (ReconciliationResult, error) {
	result := ReconciliationResult{
		Type:      reconType,
		Exchange:  exchange,
		Timestamp: time.Now(),
	}

	// 1. Fetch exchange state
	exchangeOrders, err := r.ccxtService.FetchOpenOrders(ctx, exchange)
	if err != nil {
		r.logger.Printf("Failed to fetch orders from %s: %v", exchange, err)
		// Continue with empty - we still want to check local vs nothing
		exchangeOrders = &ccxt.OpenOrdersResponse{Exchange: exchange, Orders: []ccxt.Order{}}
	}

	exchangePositions, err := r.ccxtService.FetchPositions(ctx, exchange)
	if err != nil {
		r.logger.Printf("Failed to fetch positions from %s: %v", exchange, err)
		exchangePositions = &ccxt.PositionsResponse{Exchange: exchange, Positions: []ccxt.Position{}}
	}

	exchangeBalance, err := r.ccxtService.FetchBalance(ctx, exchange)
	if err != nil {
		r.logger.Printf("Failed to fetch balance from %s: %v", exchange, err)
		// Continue - balance is optional for reconciliation
	}

	// 2. Fetch local state
	localOrders, err := r.getLocalOrders(ctx, exchange, chatID)
	if err != nil {
		return result, fmt.Errorf("failed to get local orders: %w", err)
	}

	localPositions, err := r.getLocalPositions(ctx, exchange, chatID)
	if err != nil {
		return result, fmt.Errorf("failed to get local positions: %w", err)
	}

	// 3. Compare and detect drift
	r.compareOrders(&result, localOrders, exchangeOrders.Orders)
	r.comparePositions(&result, localPositions, exchangePositions.Positions)

	if exchangeBalance != nil {
		r.compareBalances(&result, exchangeBalance)
	}

	// 4. Determine overall status
	if result.OrdersMismatched > 0 || result.PositionsMismatched > 0 || result.BalancesMismatched > 0 {
		result.Status = ReconciliationDriftDetected
		result.DriftDetected = true
	} else if result.OrdersOrphaned > 0 || result.PositionsOrphaned > 0 {
		result.Status = ReconciliationPartial
	} else {
		result.Status = ReconciliationSuccess
	}

	// 5. Log reconciliation
	if err := r.logReconciliation(ctx, chatID, &result); err != nil {
		r.logger.Printf("Failed to log reconciliation: %v", err)
	}

	// 6. Auto-sync if enabled
	if r.config.EnableAutoSync && result.DriftDetected {
		if err := r.syncLocalState(ctx, exchange, &result); err != nil {
			r.logger.Printf("Failed to sync local state: %v", err)
		}
	}

	return result, nil
}

// compareOrders compares local orders with exchange orders
func (r *ExchangePositionReconciler) compareOrders(result *ReconciliationResult, localOrders []ReconcilerOrderRecord, exchangeOrders []ccxt.Order) {
	exchangeOrderMap := make(map[string]ccxt.Order)
	for _, order := range exchangeOrders {
		exchangeOrderMap[order.ID] = order
	}

	for _, local := range localOrders {
		exchange, exists := exchangeOrderMap[local.OrderID]
		if !exists {
			// Local order not found on exchange - might be filled or cancelled
			result.OrdersOrphaned++
			result.DriftDetails = append(result.DriftDetails, DriftDetail{
				Type:          "order",
				Exchange:      result.Exchange,
				Symbol:        local.Symbol,
				LocalValue:    local.Status,
				ExchangeValue: "not_found",
				Severity:      "medium",
			})
			continue
		}

		// Compare status
		if !strings.EqualFold(local.Status, exchange.Status) {
			result.OrdersMismatched++
			result.DriftDetails = append(result.DriftDetails, DriftDetail{
				Type:          "order",
				Exchange:      result.Exchange,
				Symbol:        local.Symbol,
				LocalValue:    local.Status,
				ExchangeValue: exchange.Status,
				Severity:      "low",
			})
		} else {
			result.OrdersMatched++
		}
	}
}

// comparePositions compares local positions with exchange positions
func (r *ExchangePositionReconciler) comparePositions(result *ReconciliationResult, localPositions []PositionRecord, exchangePositions []ccxt.Position) {
	exchangePosMap := make(map[string]ccxt.Position)
	for _, pos := range exchangePositions {
		key := strings.ToUpper(strings.TrimSpace(pos.Symbol)) + "_" + normalizeReconcilerPositionSide(pos.Side)
		exchangePosMap[key] = pos
	}

	for _, local := range localPositions {
		key := strings.ToUpper(strings.TrimSpace(local.Symbol)) + "_" + normalizeReconcilerPositionSide(local.Side)
		exchange, exists := exchangePosMap[key]
		if !exists {
			// Local position not found on exchange
			if !local.Size.IsZero() {
				result.PositionsOrphaned++
				result.DriftDetails = append(result.DriftDetails, DriftDetail{
					Type:          "position",
					Exchange:      result.Exchange,
					Symbol:        local.Symbol,
					LocalValue:    local.Size.String(),
					ExchangeValue: "0",
					Severity:      "high",
				})
			}
			continue
		}

		// Compare size
		if !local.Size.Equal(exchange.Size) {
			result.PositionsMismatched++
			result.DriftDetails = append(result.DriftDetails, DriftDetail{
				Type:          "position",
				Exchange:      result.Exchange,
				Symbol:        local.Symbol,
				LocalValue:    local.Size.String(),
				ExchangeValue: exchange.Size.String(),
				Severity:      "high",
			})
		} else {
			result.PositionsMatched++
		}
	}
}

// compareBalances checks for balance drift
func (r *ExchangePositionReconciler) compareBalances(result *ReconciliationResult, balance *ccxt.BalanceResponse) {
	// For now, just note that we have balance data
	// Full implementation would compare with cached/local balance records
	result.BalancesMatched = len(balance.Total)
}

// getLocalOrders retrieves local orders from database
func (r *ExchangePositionReconciler) getLocalOrders(ctx context.Context, exchange, chatID string) ([]ReconcilerOrderRecord, error) {
	if r.db == nil {
		return []ReconcilerOrderRecord{}, nil
	}

	query := `SELECT id, exchange, symbol, side, status, price, amount, filled
	          FROM open_orders
	          WHERE exchange = $1 AND LOWER(status) = 'open'`
	args := []any{exchange}
	if chatID != "" {
		query = `SELECT id, exchange, symbol, side, status, price, amount, filled
		         FROM open_orders
		         WHERE exchange = $1 AND chat_id = $2 AND LOWER(status) = 'open'`
		args = append(args, chatID)
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		if isMissingTableError(err) {
			r.logger.Printf("open_orders table missing, falling back to trading_orders for %s", exchange)
			return r.getLocalOrdersFromTradingOrders(ctx, exchange, chatID)
		}
		return nil, err
	}
	defer rows.Close()

	var orders []ReconcilerOrderRecord
	for rows.Next() {
		var o ReconcilerOrderRecord
		if err := rows.Scan(&o.OrderID, &o.Exchange, &o.Symbol, &o.Side, &o.Status, &o.Price, &o.Size, &o.Filled); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}

	return orders, nil
}

func (r *ExchangePositionReconciler) getLocalOrdersFromTradingOrders(ctx context.Context, exchange, chatID string) ([]ReconcilerOrderRecord, error) {
	if chatID != "" {
		r.logger.Printf("chat-specific order reconciliation fallback unavailable for trading_orders on %s; using exchange-wide rows", exchange)
	}

	rows, err := r.db.Query(ctx, `
		SELECT order_id, exchange, symbol, side, status, price, amount
		FROM trading_orders
		WHERE exchange = $1 AND LOWER(status) IN ('open', 'pending', 'partial')
	`, exchange)
	if err != nil {
		if isMissingTableError(err) {
			r.logger.Printf("trading_orders table missing, skipping order reconciliation for %s", exchange)
			return []ReconcilerOrderRecord{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	orders := make([]ReconcilerOrderRecord, 0)
	for rows.Next() {
		var o ReconcilerOrderRecord
		if err := rows.Scan(&o.OrderID, &o.Exchange, &o.Symbol, &o.Side, &o.Status, &o.Price, &o.Size); err != nil {
			return nil, err
		}
		o.Filled = decimal.Zero
		orders = append(orders, o)
	}

	return orders, nil
}

// getLocalPositions retrieves local positions from database
func (r *ExchangePositionReconciler) getLocalPositions(ctx context.Context, exchange, chatID string) ([]PositionRecord, error) {
	if r.db == nil {
		return []PositionRecord{}, nil
	}

	query := `SELECT symbol, side, size, entry_price, current_price
	          FROM reconciled_positions
	          WHERE exchange = $1`
	args := []any{exchange}
	if chatID != "" {
		query = `SELECT symbol, side, size, entry_price, current_price
		         FROM reconciled_positions
		         WHERE exchange = $1 AND chat_id = $2`
		args = append(args, chatID)
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		if isMissingTableError(err) {
			r.logger.Printf("reconciled_positions table missing, falling back to trading_positions for %s", exchange)
			return r.getLocalPositionsFromTradingPositions(ctx, exchange, chatID)
		}
		return nil, err
	}
	defer rows.Close()

	var positions []PositionRecord
	for rows.Next() {
		var p PositionRecord
		if err := rows.Scan(&p.Symbol, &p.Side, &p.Size, &p.EntryPrice, &p.CurrentPrice); err != nil {
			return nil, err
		}
		positions = append(positions, p)
	}

	return positions, nil
}

func (r *ExchangePositionReconciler) getLocalPositionsFromTradingPositions(ctx context.Context, exchange, chatID string) ([]PositionRecord, error) {
	if chatID != "" {
		r.logger.Printf("chat-specific position reconciliation fallback unavailable for trading_positions on %s; using exchange-wide rows", exchange)
	}

	rows, err := r.db.Query(ctx, `
		SELECT symbol, side, size, entry_price
		FROM trading_positions
		WHERE exchange = $1 AND LOWER(status) = 'open'
	`, exchange)
	if err != nil {
		if isMissingTableError(err) {
			r.logger.Printf("trading_positions table missing, skipping position reconciliation for %s", exchange)
			return []PositionRecord{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	positions := make([]PositionRecord, 0)
	for rows.Next() {
		var p PositionRecord
		if err := rows.Scan(&p.Symbol, &p.Side, &p.Size, &p.EntryPrice); err != nil {
			return nil, err
		}
		p.CurrentPrice = p.EntryPrice
		positions = append(positions, p)
	}

	return positions, nil
}

// logReconciliation logs the reconciliation result to database
func (r *ExchangePositionReconciler) logReconciliation(ctx context.Context, chatID string, result *ReconciliationResult) error {
	if r.db == nil {
		return nil
	}

	detailsJSON, _ := json.Marshal(result.DriftDetails)

	_, err := r.db.Exec(ctx, `
		INSERT INTO reconciliation_log (
			chat_id, exchange, reconciliation_type, status,
			orders_matched, orders_mismatched, orders_orphaned,
			positions_matched, positions_mismatched, positions_orphaned,
			balances_matched, balances_mismatched, details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, chatID, result.Exchange, string(result.Type), string(result.Status),
		result.OrdersMatched, result.OrdersMismatched, result.OrdersOrphaned,
		result.PositionsMatched, result.PositionsMismatched, result.PositionsOrphaned,
		result.BalancesMatched, result.BalancesMismatched, detailsJSON)

	if err != nil && isMissingTableError(err) {
		r.logger.Printf("reconciliation_log table missing, skipping reconciliation audit insert")
		return nil
	}
	return err
}

// syncLocalState updates local state to match exchange
func (r *ExchangePositionReconciler) syncLocalState(ctx context.Context, exchange string, result *ReconciliationResult) error {
	r.logger.Printf("Auto-syncing local state for %s", exchange)

	// Update orphaned orders to match reality
	for _, drift := range result.DriftDetails {
		if drift.Type == "order" && drift.ExchangeValue == "not_found" {
			// Mark local order as closed
			_, err := r.db.Exec(ctx, `
					UPDATE open_orders 
					SET status = 'cancelled', closed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
					WHERE exchange = $1 AND symbol = $2 AND status = 'open'
				`, exchange, drift.Symbol)
			if err != nil {
				if isMissingTableError(err) {
					continue
				}
				r.logger.Printf("Failed to update orphaned order: %v", err)
			}
		}
	}

	return nil
}

// GetReconciliationSummary returns a human-readable summary
func (r *ExchangePositionReconciler) GetReconciliationSummary(result *ReconciliationResult) string {
	status := "✅ All systems in sync"
	switch result.Status {
	case ReconciliationDriftDetected:
		status = fmt.Sprintf("⚠️ Drift detected: %d orders, %d positions mismatched",
			result.OrdersMismatched, result.PositionsMismatched)
	case ReconciliationPartial:
		status = fmt.Sprintf("⚠️ Partial sync: %d orphaned orders, %d orphaned positions",
			result.OrdersOrphaned, result.PositionsOrphaned)
	case ReconciliationFailed:
		status = "❌ Reconciliation failed"
	}

	return fmt.Sprintf("Exchange: %s | %s | Checked: %s",
		result.Exchange, status, result.Timestamp.Format(time.RFC3339))
}

// Helper types
type ReconcilerOrderRecord struct {
	OrderID  string
	Exchange string
	Symbol   string
	Side     string
	Status   string
	Price    decimal.Decimal
	Size     decimal.Decimal
	Filled   decimal.Decimal
}

// SetOnOrderDrift sets the callback for order drift detection
func (r *ExchangePositionReconciler) SetOnOrderDrift(callback func(ctx context.Context, drift DriftDetail) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onOrderDrift = callback
}

// SetOnPositionDrift sets the callback for position drift detection
func (r *ExchangePositionReconciler) SetOnPositionDrift(callback func(ctx context.Context, drift DriftDetail) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onPositionDrift = callback
}

// SetOnBalanceDrift sets the callback for balance drift detection
func (r *ExchangePositionReconciler) SetOnBalanceDrift(callback func(ctx context.Context, drift DriftDetail) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onBalanceDrift = callback
}

func isMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "does not exist")
}

func normalizeReconcilerPositionSide(side string) string {
	normalized := strings.ToLower(strings.TrimSpace(side))
	switch normalized {
	case "open_long", "long", "buy":
		return "long"
	case "open_short", "short", "sell":
		return "short"
	default:
		return normalized
	}
}
