package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

// StopLossConfig holds configuration for stop-loss execution.
type StopLossConfig struct {
	// Default stop-loss percentages
	DefaultStopLossPct decimal.Decimal // Default stop-loss as % of entry (e.g., 0.02 = 2%)
	TightStopLossPct   decimal.Decimal // Tight stop-loss for high-confidence setups (e.g., 0.01 = 1%)
	TrailingStopPct    decimal.Decimal // Trailing stop distance (e.g., 0.015 = 1.5%)

	// Risk limits
	MaxStopLossPct     decimal.Decimal // Maximum allowed stop-loss %
	MinStopLossPct     decimal.Decimal // Minimum allowed stop-loss %
	MaxPositionRiskUSD decimal.Decimal // Maximum risk per position in USD

	// Execution settings
	ExecutionTimeout time.Duration
	MaxSlippagePct   decimal.Decimal // Maximum acceptable slippage
	UseMarketOrders  bool            // Use market orders for stop execution

	// Integration settings
	EnableTrailingStop bool // Enable trailing stop functionality
	EnableBreakEven    bool // Move to break-even after profit target
}

// DefaultStopLossConfig returns default stop-loss configuration.
func DefaultStopLossConfig() StopLossConfig {
	return StopLossConfig{
		DefaultStopLossPct: decimal.NewFromFloat(0.02),
		TightStopLossPct:   decimal.NewFromFloat(0.01),
		TrailingStopPct:    decimal.NewFromFloat(0.015),
		MaxStopLossPct:     decimal.NewFromFloat(0.05),
		MinStopLossPct:     decimal.NewFromFloat(0.005),
		MaxPositionRiskUSD: decimal.NewFromInt(1000),
		ExecutionTimeout:   30 * time.Second,
		MaxSlippagePct:     decimal.NewFromFloat(0.5),
		UseMarketOrders:    true,
		EnableTrailingStop: true,
		EnableBreakEven:    true,
	}
}

// StopLossStatus represents the current status of a stop-loss order.
type StopLossStatus string

const (
	StopLossStatusActive    StopLossStatus = "active"
	StopLossStatusTriggered StopLossStatus = "triggered"
	StopLossStatusExecuted  StopLossStatus = "executed"
	StopLossStatusCancelled StopLossStatus = "cancelled"
	StopLossStatusExpired   StopLossStatus = "expired"
)

// StopLossOrder represents a stop-loss order configuration and state.
type StopLossOrder struct {
	ID           string          `json:"id"`
	PositionID   string          `json:"position_id"`
	Symbol       string          `json:"symbol"`
	Exchange     string          `json:"exchange"`
	Side         string          `json:"side"` // "long" or "short"
	EntryPrice   decimal.Decimal `json:"entry_price"`
	PositionSize decimal.Decimal `json:"position_size"`

	// Stop-loss configuration
	StopPrice    decimal.Decimal `json:"stop_price"`
	StopLossPct  decimal.Decimal `json:"stop_loss_pct"`
	IsTrailing   bool            `json:"is_trailing"`
	TrailingDist decimal.Decimal `json:"trailing_distance,omitempty"`
	HighestPrice decimal.Decimal `json:"highest_price,omitempty"` // For trailing stop tracking
	LowestPrice  decimal.Decimal `json:"lowest_price,omitempty"`  // For trailing stop tracking

	// Execution tracking
	Status         StopLossStatus  `json:"status"`
	TriggerPrice   decimal.Decimal `json:"trigger_price,omitempty"`
	ExecutionPrice decimal.Decimal `json:"execution_price,omitempty"`
	ExecutionTime  *time.Time      `json:"execution_time,omitempty"`
	RealizedPnL    decimal.Decimal `json:"realized_pnl,omitempty"`
	SlippagePct    decimal.Decimal `json:"slippage_pct,omitempty"`

	// Metadata
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IsActive returns true if the stop-loss order is still active.
func (s *StopLossOrder) IsActive() bool {
	return s.Status == StopLossStatusActive
}

// ShouldTrigger checks if current price should trigger the stop-loss.
func (s *StopLossOrder) ShouldTrigger(currentPrice decimal.Decimal) bool {
	if !s.IsActive() {
		return false
	}

	if s.Side == "long" {
		return currentPrice.LessThanOrEqual(s.StopPrice)
	}
	return currentPrice.GreaterThanOrEqual(s.StopPrice)
}

// UpdateTrailingStop updates the stop price for trailing stops based on current price.
func (s *StopLossOrder) UpdateTrailingStop(currentPrice decimal.Decimal) bool {
	if !s.IsTrailing || !s.IsActive() {
		return false
	}

	updated := false

	if s.Side == "long" {
		// Track highest price
		if currentPrice.GreaterThan(s.HighestPrice) {
			s.HighestPrice = currentPrice
			// Move stop up
			newStop := currentPrice.Sub(currentPrice.Mul(s.TrailingDist))
			if newStop.GreaterThan(s.StopPrice) {
				s.StopPrice = newStop
				updated = true
			}
		}
	} else {
		// Track lowest price
		if s.LowestPrice.IsZero() || currentPrice.LessThan(s.LowestPrice) {
			s.LowestPrice = currentPrice
			// Move stop down
			newStop := currentPrice.Add(currentPrice.Mul(s.TrailingDist))
			if newStop.LessThan(s.StopPrice) || s.StopPrice.IsZero() {
				s.StopPrice = newStop
				updated = true
			}
		}
	}

	if updated {
		s.UpdatedAt = time.Now().UTC()
	}
	return updated
}

// StopLossExecutionResult represents the result of executing a stop-loss.
type StopLossExecutionResult struct {
	OrderID        string          `json:"order_id"`
	Success        bool            `json:"success"`
	ExecutionPrice decimal.Decimal `json:"execution_price"`
	RealizedPnL    decimal.Decimal `json:"realized_pnl"`
	SlippagePct    decimal.Decimal `json:"slippage_pct"`
	Error          string          `json:"error,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
}

// StopLossService manages stop-loss orders and execution.
type StopLossService struct {
	config      StopLossConfig
	ccxtService ccxt.CCXTService
	logger      *zaplogrus.Logger

	// Active stop-loss orders
	orders   map[string]*StopLossOrder // orderID -> order
	ordersMu sync.RWMutex

	// Orders by position for quick lookup
	positionOrders map[string]string // positionID -> orderID
	positionMu     sync.RWMutex

	// Order book imbalance detector for optimal timing
	imbalanceDetector *OrderBookImbalanceDetector

	// Callback for execution
	executionCallback func(ctx context.Context, order *StopLossOrder) (*StopLossExecutionResult, error)
}

// NewStopLossService creates a new stop-loss service.
func NewStopLossService(
	config StopLossConfig,
	ccxtService ccxt.CCXTService,
	logger *zaplogrus.Logger,
	imbalanceDetector *OrderBookImbalanceDetector,
) *StopLossService {
	return &StopLossService{
		config:            config,
		ccxtService:       ccxtService,
		logger:            logger,
		orders:            make(map[string]*StopLossOrder),
		positionOrders:    make(map[string]string),
		imbalanceDetector: imbalanceDetector,
	}
}

// CreateStopLoss creates a new stop-loss order for a position.
func (s *StopLossService) CreateStopLoss(ctx context.Context, params StopLossParams) (*StopLossOrder, error) {
	// Validate parameters
	if err := s.validateParams(params); err != nil {
		return nil, fmt.Errorf("invalid stop-loss parameters: %w", err)
	}

	// Calculate stop price
	stopPrice := s.calculateStopPrice(params)

	// Check risk limits
	if err := s.checkRiskLimits(params, stopPrice); err != nil {
		return nil, err
	}

	order := &StopLossOrder{
		ID:           uuid.New().String(),
		PositionID:   params.PositionID,
		Symbol:       params.Symbol,
		Exchange:     params.Exchange,
		Side:         params.Side,
		EntryPrice:   params.EntryPrice,
		PositionSize: params.PositionSize,
		StopPrice:    stopPrice,
		StopLossPct:  params.StopLossPct,
		IsTrailing:   params.IsTrailing,
		TrailingDist: s.config.TrailingStopPct,
		HighestPrice: params.EntryPrice, // Initialize with entry price
		Status:       StopLossStatusActive,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour), // Default 24h expiry
	}

	// Store order
	s.ordersMu.Lock()
	s.orders[order.ID] = order
	s.ordersMu.Unlock()

	s.positionMu.Lock()
	s.positionOrders[order.PositionID] = order.ID
	s.positionMu.Unlock()

	s.logger.Info("Stop-loss order created",
		"order_id", order.ID,
		"position_id", order.PositionID,
		"symbol", order.Symbol,
		"stop_price", order.StopPrice,
		"stop_loss_pct", order.StopLossPct)

	return order, nil
}

// CreateTightStopLoss creates a tight stop-loss using order book imbalance for optimal timing.
func (s *StopLossService) CreateTightStopLoss(ctx context.Context, params StopLossParams) (*StopLossOrder, error) {
	// Use tight stop-loss percentage
	params.StopLossPct = s.config.TightStopLossPct

	// Check order book imbalance for signal confirmation
	imbalance, err := s.imbalanceDetector.Detect(ctx, params.Exchange, params.Symbol)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to check order book imbalance, proceeding with standard stop-loss")
	}

	// If strong imbalance against our position, tighten stop further
	if imbalance != nil {
		if (params.Side == "long" && imbalance.Direction == "bearish" && imbalance.Strength == "strong") ||
			(params.Side == "short" && imbalance.Direction == "bullish" && imbalance.Strength == "strong") {
			// Tighten stop by 20%
			params.StopLossPct = params.StopLossPct.Mul(decimal.NewFromFloat(0.8))
			s.logger.Info("Tightening stop-loss due to adverse order book imbalance",
				"symbol", params.Symbol,
				"original_pct", s.config.TightStopLossPct,
				"adjusted_pct", params.StopLossPct,
				"imbalance_signal", imbalance.Direction)
		}
	}

	return s.CreateStopLoss(ctx, params)
}

// Evaluate checks all active stop-loss orders and triggers if conditions are met.
func (s *StopLossService) Evaluate(ctx context.Context) ([]*StopLossExecutionResult, error) {
	results := make([]*StopLossExecutionResult, 0)

	s.ordersMu.RLock()
	activeOrders := make([]*StopLossOrder, 0)
	for _, order := range s.orders {
		if order.IsActive() {
			activeOrders = append(activeOrders, order)
		}
	}
	s.ordersMu.RUnlock()

	for _, order := range activeOrders {
		// Fetch current price
		ticker, err := s.ccxtService.FetchSingleTicker(ctx, order.Exchange, order.Symbol)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to fetch ticker for stop-loss evaluation",
				"order_id", order.ID,
				"symbol", order.Symbol)
			continue
		}

		currentPrice := decimal.NewFromFloat(ticker.GetPrice())

		// Update trailing stops
		if order.IsTrailing {
			if updated := order.UpdateTrailingStop(currentPrice); updated {
				s.logger.Info("Trailing stop updated",
					"order_id", order.ID,
					"new_stop_price", order.StopPrice)
			}
		}

		// Check if stop should trigger
		if order.ShouldTrigger(currentPrice) {
			result, err := s.ExecuteStopLoss(ctx, order.ID, currentPrice)
			if err != nil {
				s.logger.WithError(err).Error("Failed to execute stop-loss",
					"order_id", order.ID)
				continue
			}
			results = append(results, result)
		}
	}

	return results, nil
}

// ExecuteStopLoss executes a stop-loss order.
func (s *StopLossService) ExecuteStopLoss(ctx context.Context, orderID string, triggerPrice decimal.Decimal) (*StopLossExecutionResult, error) {
	s.ordersMu.Lock()
	order, exists := s.orders[orderID]
	if !exists || !order.IsActive() {
		s.ordersMu.Unlock()
		return nil, fmt.Errorf("stop-loss order not found or not active: %s", orderID)
	}

	// Update status
	order.Status = StopLossStatusTriggered
	order.TriggerPrice = triggerPrice
	s.ordersMu.Unlock()

	s.logger.Info("Executing stop-loss",
		"order_id", order.ID,
		"symbol", order.Symbol,
		"trigger_price", triggerPrice)

	// Use callback or default execution
	var result *StopLossExecutionResult
	var err error
	if s.executionCallback != nil {
		result, err = s.executionCallback(ctx, order)
	} else {
		result = s.simulateExecution(order, triggerPrice)
	}

	if err != nil {
		order.Status = StopLossStatusActive // Reset status
		return nil, fmt.Errorf("stop-loss execution failed: %w", err)
	}

	// Update order with result
	s.ordersMu.Lock()
	order.Status = StopLossStatusExecuted
	order.ExecutionPrice = result.ExecutionPrice
	order.RealizedPnL = result.RealizedPnL
	order.SlippagePct = result.SlippagePct
	now := time.Now().UTC()
	order.ExecutionTime = &now
	s.ordersMu.Unlock()

	return result, nil
}

// CancelStopLoss cancels an active stop-loss order.
func (s *StopLossService) CancelStopLoss(orderID string) error {
	s.ordersMu.Lock()
	defer s.ordersMu.Unlock()

	order, exists := s.orders[orderID]
	if !exists {
		return fmt.Errorf("stop-loss order not found: %s", orderID)
	}

	if !order.IsActive() {
		return fmt.Errorf("stop-loss order is not active: %s", orderID)
	}

	order.Status = StopLossStatusCancelled
	order.UpdatedAt = time.Now().UTC()

	s.logger.Info("Stop-loss order cancelled", "order_id", orderID)
	return nil
}

// GetStopLoss retrieves a stop-loss order by ID.
func (s *StopLossService) GetStopLoss(orderID string) (*StopLossOrder, bool) {
	s.ordersMu.RLock()
	defer s.ordersMu.RUnlock()

	order, exists := s.orders[orderID]
	return order, exists
}

// GetStopLossByPosition retrieves a stop-loss order by position ID.
func (s *StopLossService) GetStopLossByPosition(positionID string) (*StopLossOrder, bool) {
	s.positionMu.RLock()
	orderID, exists := s.positionOrders[positionID]
	s.positionMu.RUnlock()

	if !exists {
		return nil, false
	}

	return s.GetStopLoss(orderID)
}

// SetExecutionCallback sets a custom execution callback for testing or integration.
func (s *StopLossService) SetExecutionCallback(callback func(ctx context.Context, order *StopLossOrder) (*StopLossExecutionResult, error)) {
	s.executionCallback = callback
}

// StopLossParams contains parameters for creating a stop-loss order.
type StopLossParams struct {
	PositionID   string
	Symbol       string
	Exchange     string
	Side         string // "long" or "short"
	EntryPrice   decimal.Decimal
	PositionSize decimal.Decimal
	StopLossPct  decimal.Decimal // Optional: use default if zero
	IsTrailing   bool
}

// validateParams validates stop-loss parameters.
func (s *StopLossService) validateParams(params StopLossParams) error {
	if params.PositionID == "" {
		return fmt.Errorf("position_id is required")
	}
	if params.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if params.Exchange == "" {
		return fmt.Errorf("exchange is required")
	}
	if params.Side != "long" && params.Side != "short" {
		return fmt.Errorf("side must be 'long' or 'short'")
	}
	if params.EntryPrice.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("entry_price must be greater than zero")
	}
	if params.PositionSize.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("position_size must be greater than zero")
	}
	return nil
}

// calculateStopPrice calculates the stop price based on parameters.
func (s *StopLossService) calculateStopPrice(params StopLossParams) decimal.Decimal {
	stopLossPct := params.StopLossPct
	if stopLossPct.IsZero() {
		stopLossPct = s.config.DefaultStopLossPct
	}

	if params.Side == "long" {
		return params.EntryPrice.Sub(params.EntryPrice.Mul(stopLossPct))
	}
	return params.EntryPrice.Add(params.EntryPrice.Mul(stopLossPct))
}

// checkRiskLimits validates that the stop-loss meets risk management criteria.
func (s *StopLossService) checkRiskLimits(params StopLossParams, stopPrice decimal.Decimal) error {
	// Check stop-loss percentage is within bounds
	stopLossPct := params.StopLossPct
	if stopLossPct.IsZero() {
		stopLossPct = s.config.DefaultStopLossPct
	}

	if stopLossPct.LessThan(s.config.MinStopLossPct) {
		return fmt.Errorf("stop-loss percentage %.4f is below minimum %.4f",
			stopLossPct.InexactFloat64(), s.config.MinStopLossPct.InexactFloat64())
	}
	if stopLossPct.GreaterThan(s.config.MaxStopLossPct) {
		return fmt.Errorf("stop-loss percentage %.4f exceeds maximum %.4f",
			stopLossPct.InexactFloat64(), s.config.MaxStopLossPct.InexactFloat64())
	}

	// Calculate position risk
	priceDiff := params.EntryPrice.Sub(stopPrice).Abs()
	positionRisk := priceDiff.Mul(params.PositionSize)

	if positionRisk.GreaterThan(s.config.MaxPositionRiskUSD) {
		return fmt.Errorf("position risk $%.2f exceeds maximum $%.2f",
			positionRisk.InexactFloat64(), s.config.MaxPositionRiskUSD.InexactFloat64())
	}

	return nil
}

// simulateExecution simulates stop-loss execution for testing/paper trading.
func (s *StopLossService) simulateExecution(order *StopLossOrder, triggerPrice decimal.Decimal) *StopLossExecutionResult {
	// Simulate some slippage
	slippagePct := decimal.NewFromFloat(0.1) // 0.1% slippage
	executionPrice := triggerPrice

	if order.Side == "long" {
		// For long positions, sell below trigger (worse price)
		executionPrice = triggerPrice.Sub(triggerPrice.Mul(slippagePct.Div(decimal.NewFromInt(100))))
	} else {
		// For short positions, buy above trigger (worse price)
		executionPrice = triggerPrice.Add(triggerPrice.Mul(slippagePct.Div(decimal.NewFromInt(100))))
	}

	// Calculate PnL
	var realizedPnL decimal.Decimal
	if order.Side == "long" {
		realizedPnL = executionPrice.Sub(order.EntryPrice).Mul(order.PositionSize)
	} else {
		realizedPnL = order.EntryPrice.Sub(executionPrice).Mul(order.PositionSize)
	}

	return &StopLossExecutionResult{
		OrderID:        order.ID,
		Success:        true,
		ExecutionPrice: executionPrice,
		RealizedPnL:    realizedPnL,
		SlippagePct:    slippagePct,
		Timestamp:      time.Now().UTC(),
	}
}
