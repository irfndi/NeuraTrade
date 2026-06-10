package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

// MaxLossPctDefault is the default per-position max-loss threshold (1.5%).
// When a position's unrealized loss exceeds this, the monitor fires a close event.
const MaxLossPctDefault = 0.015

// PositionSnapshot is a read-only view of an open position used by the monitor.
type PositionSnapshot struct {
	Symbol     string
	Exchange   string
	Side       string
	EntryPrice decimal.Decimal
	Quantity   decimal.Decimal
	OpenedAt   time.Time
}

// PriceProvider returns the current mark price for a symbol/exchange pair.
// Returns an error if the price cannot be fetched (network failure, etc.).
type PriceProvider func(ctx context.Context, exchange, symbol string) (decimal.Decimal, error)

// PositionCloser is called when a position must be closed due to max-loss.
// Implementations should place a market order to flatten the position.
type PositionCloser func(ctx context.Context, pos PositionSnapshot, reason string) error

// MaxLossMonitorConfig configures the runtime position max-loss monitor.
type MaxLossMonitorConfig struct {
	// MaxLossPct is the per-position loss threshold (e.g. 0.015 for 1.5%).
	// Default: MaxLossPctDefault.
	MaxLossPct float64

	// PollInterval is how often to check positions against mark prices.
	// Default: 1s.
	PollInterval time.Duration

	// MaxStalePrice is how old a price can be before the monitor skips
	// evaluation (avoids acting on stale data). Default: 10s.
	MaxStalePrice time.Duration
}

// MaxLossMonitor watches open positions and fires a close callback when any
// position's unrealized loss exceeds MaxLossPct. This is the live-execution
// equivalent of the 1.5% max-loss stop used in the backtest engine.
type MaxLossMonitor struct {
	config    MaxLossMonitorConfig
	positions sync.Map // key: "exchange|symbol" -> PositionSnapshot
	prices    PriceProvider
	closer    PositionCloser

	lastPriceTime sync.Map // key: "exchange|symbol" -> time.Time
}

// NewMaxLossMonitor creates a new max-loss monitor.
// prices and closer must be non-nil.
func NewMaxLossMonitor(config MaxLossMonitorConfig, prices PriceProvider, closer PositionCloser) *MaxLossMonitor {
	if config.MaxLossPct <= 0 {
		config.MaxLossPct = MaxLossPctDefault
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 1 * time.Second
	}
	if config.MaxStalePrice <= 0 {
		config.MaxStalePrice = 10 * time.Second
	}
	return &MaxLossMonitor{
		config: config,
		prices: prices,
		closer: closer,
	}
}

// TrackPosition registers a position to be monitored.
// Safe to call concurrently.
func (m *MaxLossMonitor) TrackPosition(pos PositionSnapshot) {
	key := positionKey(pos.Exchange, pos.Symbol)
	m.positions.Store(key, pos)
}

// UntrackPosition removes a position from monitoring (call after successful close).
func (m *MaxLossMonitor) UntrackPosition(exchange, symbol string) {
	key := positionKey(exchange, symbol)
	m.positions.Delete(key)
	m.lastPriceTime.Delete(key)
}

// Run starts the monitoring loop. Blocks until ctx is cancelled.
// Suitable for wrapping in a supervisor.Group.
func (m *MaxLossMonitor) Run(ctx context.Context) error {
	zaplogrus.Infof("[max-loss-monitor] starting (poll=%v, threshold=%.2f%%)",
		m.config.PollInterval, m.config.MaxLossPct*100)

	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			zaplogrus.Infof("[max-loss-monitor] stopping: %v", ctx.Err())
			return nil
		case <-ticker.C:
			m.checkAll(ctx)
		}
	}
}

// checkAll iterates all tracked positions and fires closer on max-loss breach.
func (m *MaxLossMonitor) checkAll(ctx context.Context) {
	m.positions.Range(func(key, value any) bool {
		pos := value.(PositionSnapshot)
		m.checkOne(ctx, pos)
		return true
	})
}

// checkOne evaluates a single position against current mark price.
func (m *MaxLossMonitor) checkOne(ctx context.Context, pos PositionSnapshot) {
	if pos.Quantity.IsZero() || pos.EntryPrice.IsZero() {
		return
	}

	price, err := m.prices(ctx, pos.Exchange, pos.Symbol)
	if err != nil {
		zaplogrus.Infof("[max-loss-monitor] price fetch failed for %s/%s: %v",
			pos.Exchange, pos.Symbol, err)
		return
	}

	now := time.Now()
	key := positionKey(pos.Exchange, pos.Symbol)
	if t, ok := m.lastPriceTime.Load(key); ok {
		if now.Sub(t.(time.Time)) > m.config.MaxStalePrice {
			zaplogrus.Infof("[max-loss-monitor] skipping stale price for %s/%s", pos.Exchange, pos.Symbol)
			return
		}
	}
	m.lastPriceTime.Store(key, now)

	maxLossPrice := m.computeMaxLossPrice(pos)
	if maxLossPrice.IsZero() {
		return
	}

	breached := false
	switch pos.Side {
	case "buy":
		breached = price.LessThanOrEqual(maxLossPrice)
	case "sell":
		breached = price.GreaterThanOrEqual(maxLossPrice)
	}

	if !breached {
		return
	}

	lossPct := m.computeLossPct(pos, price)
	zaplogrus.Warnf("[max-loss-monitor] BREACH %s/%s side=%s entry=%s mark=%s loss=%.2f%% threshold=%.2f%%",
		pos.Exchange, pos.Symbol, pos.Side, pos.EntryPrice.String(), price.String(),
		lossPct*100, m.config.MaxLossPct*100)

	if err := m.closer(ctx, pos, fmt.Sprintf("max_loss_%.2f%%", lossPct*100)); err != nil {
		zaplogrus.Infof("[max-loss-monitor] closer failed for %s/%s: %v",
			pos.Exchange, pos.Symbol, err)
		return
	}
	m.UntrackPosition(pos.Exchange, pos.Symbol)
}

func (m *MaxLossMonitor) computeMaxLossPrice(pos PositionSnapshot) decimal.Decimal {
	threshold := decimal.NewFromFloat(m.config.MaxLossPct)
	one := decimal.NewFromInt(1)
	switch pos.Side {
	case "buy":
		return pos.EntryPrice.Mul(one.Sub(threshold))
	case "sell":
		return pos.EntryPrice.Mul(one.Add(threshold))
	default:
		return decimal.Zero
	}
}

func (m *MaxLossMonitor) computeLossPct(pos PositionSnapshot, markPrice decimal.Decimal) float64 {
	if pos.EntryPrice.IsZero() {
		return 0
	}
	var diff decimal.Decimal
	switch pos.Side {
	case "buy":
		diff = pos.EntryPrice.Sub(markPrice)
	case "sell":
		diff = markPrice.Sub(pos.EntryPrice)
	}
	loss := diff.Div(pos.EntryPrice)
	f, _ := loss.Float64()
	return f
}

// TrackedCount returns the number of positions currently being monitored.
// Useful for tests and metrics.
func (m *MaxLossMonitor) TrackedCount() int {
	count := 0
	m.positions.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func positionKey(exchange, symbol string) string {
	return exchange + "|" + symbol
}
