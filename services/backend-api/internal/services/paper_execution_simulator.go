package services

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// PaperExecutionConfig holds configuration for paper trading simulation.
type PaperExecutionConfig struct {
	// SlippagePercentage is the maximum slippage to apply (e.g., 0.001 = 0.1%)
	SlippagePercentage decimal.Decimal
	// PartialFillProbability is the probability of a partial fill (0-1)
	PartialFillProbability decimal.Decimal
	// MinFillPercentage is the minimum percentage of order that can be filled
	MinFillPercentage decimal.Decimal
	// MaxFillPercentage is the maximum percentage of order that can be filled
	MaxFillPercentage decimal.Decimal
	// RejectionProbability is the probability of order rejection (0-1)
	RejectionProbability decimal.Decimal
	// ExecutionDelayMs is the simulated execution delay in milliseconds
	ExecutionDelayMs int
	// EnableRandomness enables random simulation features
	EnableRandomness bool
}

// DefaultPaperExecutionConfig returns default configuration.
func DefaultPaperExecutionConfig() PaperExecutionConfig {
	return PaperExecutionConfig{
		SlippagePercentage:     decimal.NewFromFloat(0.001), // 0.1%
		PartialFillProbability: decimal.NewFromFloat(0.1),   // 10%
		MinFillPercentage:      decimal.NewFromFloat(0.5),   // 50%
		MaxFillPercentage:      decimal.NewFromFloat(1.0),   // 100%
		RejectionProbability:   decimal.NewFromFloat(0.01),  // 1%
		ExecutionDelayMs:       100,                         // 100ms
		EnableRandomness:       true,
	}
}

// PaperOrderType represents the type of paper order.
type PaperOrderType string

const (
	PaperOrderTypeMarket PaperOrderType = "market"
	PaperOrderTypeLimit  PaperOrderType = "limit"
	PaperOrderTypeStop   PaperOrderType = "stop"
	PaperOrderTypeIOC    PaperOrderType = "ioc"
	PaperOrderTypeFOK    PaperOrderType = "fok"
)

// PaperOrderSide represents the side of a paper order.
type PaperOrderSide string

const (
	PaperOrderSideBuy  PaperOrderSide = "buy"
	PaperOrderSideSell PaperOrderSide = "sell"
)

// PaperOrderStatus represents the status of a paper order.
type PaperOrderStatus string

const (
	PaperOrderStatusPending   PaperOrderStatus = "pending"
	PaperOrderStatusFilled    PaperOrderStatus = "filled"
	PaperOrderStatusPartial   PaperOrderStatus = "partial"
	PaperOrderStatusCancelled PaperOrderStatus = "cancelled"
	PaperOrderStatusRejected  PaperOrderStatus = "rejected"
	PaperOrderStatusExpired   PaperOrderStatus = "expired"
)

// PaperOrder represents a paper trading order.
type PaperOrder struct {
	ID           string           `json:"id"`
	UserID       string           `json:"user_id"`
	Exchange     string           `json:"exchange"`
	Symbol       string           `json:"symbol"`
	Type         PaperOrderType   `json:"type"`
	Side         PaperOrderSide   `json:"side"`
	Size         decimal.Decimal  `json:"size"`
	FilledSize   decimal.Decimal  `json:"filled_size"`
	Price        decimal.Decimal  `json:"price"`          // Limit price
	StopPrice    decimal.Decimal  `json:"stop_price"`     // Stop price
	AvgFillPrice decimal.Decimal  `json:"avg_fill_price"` // Average fill price
	Slippage     decimal.Decimal  `json:"slippage"`       // Actual slippage applied
	Status       PaperOrderStatus `json:"status"`
	RejectReason string           `json:"reject_reason,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	ExpiresAt    *time.Time       `json:"expires_at,omitempty"`
}

// PaperExecutionSimulator simulates order execution for paper trading.
type PaperExecutionSimulator struct {
	config PaperExecutionConfig
	clock  Clock
}

// Clock interface for time dependency injection.
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock with real time.
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

// NewPaperExecutionSimulator creates a new paper execution simulator.
func NewPaperExecutionSimulator(config PaperExecutionConfig) *PaperExecutionSimulator {
	return &PaperExecutionSimulator{
		config: config,
		clock:  RealClock{},
	}
}

// NewPaperExecutionSimulatorWithClock creates a new paper execution simulator with custom clock.
func NewPaperExecutionSimulatorWithClock(config PaperExecutionConfig, clock Clock) *PaperExecutionSimulator {
	return &PaperExecutionSimulator{
		config: config,
		clock:  clock,
	}
}

// Helper functions for crypto-secure random numbers
func (s *PaperExecutionSimulator) randomFloat64() float64 {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return float64(int64(b[0])<<56|int64(b[1])<<48|int64(b[2])<<40|int64(b[3])<<32|int64(b[4])<<24|int64(b[5])<<16|int64(b[6])<<8|int64(b[7])) / (1 << 64)
}

func (s *PaperExecutionSimulator) randomFloat64Between(min, max float64) float64 {
	return min + s.randomFloat64()*(max-min)
}

func (s *PaperExecutionSimulator) randomInt63() int64 {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return int64(b[0])<<56 | int64(b[1])<<48 | int64(b[2])<<40 | int64(b[3])<<32 | int64(b[4])<<24 | int64(b[5])<<16 | int64(b[6])<<8 | int64(b[7])
}

// SimulateFill simulates filling a paper order at the given market price.
func (s *PaperExecutionSimulator) SimulateFill(ctx context.Context, order *PaperOrder, marketPrice decimal.Decimal) (*PaperOrder, error) {
	if order.Status != PaperOrderStatusPending {
		return nil, fmt.Errorf("order is not pending: %s", order.Status)
	}

	// Check for order rejection
	if s.config.EnableRandomness && s.shouldReject() {
		order.Status = PaperOrderStatusRejected
		order.RejectReason = "Simulated rejection"
		order.UpdatedAt = s.clock.Now()
		return order, nil
	}

	// Check expiration
	if order.ExpiresAt != nil && s.clock.Now().After(*order.ExpiresAt) {
		order.Status = PaperOrderStatusExpired
		order.UpdatedAt = s.clock.Now()
		return order, nil
	}

	// Simulate execution delay
	if s.config.ExecutionDelayMs > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(s.config.ExecutionDelayMs) * time.Millisecond):
		}
	}

	// Calculate fill price based on order type
	var fillPrice decimal.Decimal
	switch order.Type {
	case PaperOrderTypeMarket:
		fillPrice = s.calculateMarketFillPrice(marketPrice, order.Side)
	case PaperOrderTypeLimit:
		// Limit order fills if market price crosses limit
		if order.Side == PaperOrderSideBuy && marketPrice.LessThanOrEqual(order.Price) {
			fillPrice = order.Price
		} else if order.Side == PaperOrderSideSell && marketPrice.GreaterThanOrEqual(order.Price) {
			fillPrice = order.Price
		} else {
			// Limit order not filled - return as pending
			return order, nil
		}
	case PaperOrderTypeStop:
		// Stop order triggers when market crosses stop price
		if order.Side == PaperOrderSideBuy && marketPrice.GreaterThanOrEqual(order.StopPrice) {
			fillPrice = s.calculateMarketFillPrice(marketPrice, order.Side)
		} else if order.Side == PaperOrderSideSell && marketPrice.LessThanOrEqual(order.StopPrice) {
			fillPrice = s.calculateMarketFillPrice(marketPrice, order.Side)
		} else {
			// Stop not triggered
			return order, nil
		}
	case PaperOrderTypeIOC:
		// Immediate or Cancel - fill what's available now
		fillPrice = s.calculateMarketFillPrice(marketPrice, order.Side)
		order.Size = s.calculatePartialFill(order.Size)
		if order.Size.IsZero() {
			order.Status = PaperOrderStatusCancelled
			order.RejectReason = "IOC could not fill any quantity"
			order.UpdatedAt = s.clock.Now()
			return order, nil
		}
	case PaperOrderTypeFOK:
		// Fill or Kill - must fill entire order
		fillPrice = s.calculateMarketFillPrice(marketPrice, order.Side)
		// FOK always fills full size
	default:
		return nil, fmt.Errorf("unknown order type: %s", order.Type)
	}

	// Apply partial fill if enabled
	var filledSize decimal.Decimal
	if s.config.EnableRandomness && s.shouldPartialFill() {
		filledSize = s.calculatePartialFill(order.Size)
		order.Status = PaperOrderStatusPartial
	} else {
		filledSize = order.Size
		order.Status = PaperOrderStatusFilled
	}

	// Calculate slippage
	slippage := fillPrice.Sub(marketPrice).Abs().Div(marketPrice)
	order.Slippage = slippage
	order.FilledSize = filledSize
	order.AvgFillPrice = fillPrice
	order.UpdatedAt = s.clock.Now()

	return order, nil
}

// calculateMarketFillPrice applies slippage to the market price.
func (s *PaperExecutionSimulator) calculateMarketFillPrice(marketPrice decimal.Decimal, side PaperOrderSide) decimal.Decimal {
	one := decimal.NewFromInt(1)
	if !s.config.EnableRandomness {
		// Use fixed slippage
		if side == PaperOrderSideBuy {
			return marketPrice.Mul(one.Add(s.config.SlippagePercentage))
		}
		return marketPrice.Mul(one.Sub(s.config.SlippagePercentage))
	}

	// Random slippage up to configured maximum
	maxSlippage := s.config.SlippagePercentage
	randomSlippage := decimal.NewFromFloat(s.randomFloat64()).Mul(maxSlippage)

	if side == PaperOrderSideBuy {
		// Buy orders execute at higher price
		return marketPrice.Mul(one.Add(randomSlippage))
	}
	// Sell orders execute at lower price
	return marketPrice.Mul(one.Sub(randomSlippage))
}

// shouldReject determines if an order should be rejected.
func (s *PaperExecutionSimulator) shouldReject() bool {
	return s.randomFloat64() < s.config.RejectionProbability.InexactFloat64()
}

// shouldPartialFill determines if an order should be partially filled.
func (s *PaperExecutionSimulator) shouldPartialFill() bool {
	return s.randomFloat64() < s.config.PartialFillProbability.InexactFloat64()
}

// calculatePartialFill calculates a random partial fill amount.
func (s *PaperExecutionSimulator) calculatePartialFill(size decimal.Decimal) decimal.Decimal {
	minPct := s.config.MinFillPercentage
	maxPct := s.config.MaxFillPercentage

	// Random percentage between min and max
	randomPct := minPct.Add(
		decimal.NewFromFloat(s.randomFloat64()).Mul(maxPct.Sub(minPct)),
	)

	return size.Mul(randomPct)
}

// CreateOrder creates a new paper order.
func (s *PaperExecutionSimulator) CreateOrder(req PaperOrderRequest) (*PaperOrder, error) {
	if req.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if req.Exchange == "" {
		return nil, fmt.Errorf("exchange is required")
	}
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.Size.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("size must be greater than zero")
	}
	if req.Type != PaperOrderTypeMarket && req.Type != PaperOrderTypeLimit &&
		req.Type != PaperOrderTypeStop && req.Type != PaperOrderTypeIOC && req.Type != PaperOrderTypeFOK {
		return nil, fmt.Errorf("invalid order type: %s", req.Type)
	}
	if req.Side != PaperOrderSideBuy && req.Side != PaperOrderSideSell {
		return nil, fmt.Errorf("invalid order side: %s", req.Side)
	}

	// Validate limit order has price
	if req.Type == PaperOrderTypeLimit && req.Price.IsZero() {
		return nil, fmt.Errorf("limit orders require a price")
	}

	// IOC and FOK are time-in-force modifiers - they don't require a price
	// Validate stop order has stop price
	if req.Type == PaperOrderTypeStop && req.StopPrice.IsZero() {
		return nil, fmt.Errorf("stop orders require a stop price")
	}

	now := s.clock.Now()
	order := &PaperOrder{
		ID:         fmt.Sprintf("paper-%d-%d", now.UnixNano(), s.randomInt63()),
		UserID:     req.UserID,
		Exchange:   req.Exchange,
		Symbol:     req.Symbol,
		Type:       req.Type,
		Side:       req.Side,
		Size:       req.Size,
		FilledSize: decimal.Zero,
		Price:      req.Price,
		StopPrice:  req.StopPrice,
		Status:     PaperOrderStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  req.ExpiresAt,
	}

	return order, nil
}

// PaperOrderRequest represents a request to create a paper order.
type PaperOrderRequest struct {
	UserID    string
	Exchange  string
	Symbol    string
	Type      PaperOrderType
	Side      PaperOrderSide
	Size      decimal.Decimal
	Price     decimal.Decimal
	StopPrice decimal.Decimal
	ExpiresAt *time.Time
}

// PaperPosition represents a paper trading position.
type PaperPosition struct {
	ID            string          `json:"id"`
	UserID        string          `json:"user_id"`
	Exchange      string          `json:"exchange"`
	Symbol        string          `json:"symbol"`
	Side          PaperOrderSide  `json:"side"`
	Size          decimal.Decimal `json:"size"`
	EntryPrice    decimal.Decimal `json:"entry_price"`
	CurrentPnL    decimal.Decimal `json:"current_pnl"`
	UnrealizedPnL decimal.Decimal `json:"unrealized_pnl"`
	OpenedAt      time.Time       `json:"opened_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// CalculateUnrealizedPnL calculates the unrealized PnL for a position.
func (p *PaperPosition) CalculateUnrealizedPnL(currentPrice decimal.Decimal) decimal.Decimal {
	if p.Side == PaperOrderSideBuy {
		// Long position: PnL = (current - entry) * size
		return currentPrice.Sub(p.EntryPrice).Mul(p.Size)
	}
	// Short position: PnL = (entry - current) * size
	return p.EntryPrice.Sub(currentPrice).Mul(p.Size)
}

// UpdatePosition updates a position with new entry price and size.
func (p *PaperPosition) UpdatePosition(newSize, newEntryPrice decimal.Decimal) {
	p.Size = newSize
	p.EntryPrice = newEntryPrice
	p.UpdatedAt = time.Now()
}

// ClosePosition closes a position and returns realized PnL.
func (p *PaperPosition) ClosePosition(exitPrice decimal.Decimal) decimal.Decimal {
	var realizedPnL decimal.Decimal
	if p.Side == PaperOrderSideBuy {
		realizedPnL = exitPrice.Sub(p.EntryPrice).Mul(p.Size)
	} else {
		realizedPnL = p.EntryPrice.Sub(exitPrice).Mul(p.Size)
	}
	p.Size = decimal.Zero
	p.UpdatedAt = time.Now()
	return realizedPnL
}
