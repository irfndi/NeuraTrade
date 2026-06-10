package services

import (
	"context"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

// MaxLossExecutionResult represents the result of executing a max-loss close.
type MaxLossExecutionResult struct {
	Success     bool
	OrderID     string
	ExecutedAt  time.Time
	FillPrice   decimal.Decimal
	ErrorReason string
}

// MaxLossExecutionCallback is invoked by the MaxLossMonitorService when a
// position breaches the max-loss threshold. Implementations should place a
// market order to flatten the position via the TradingGateway or
// ExecutionActor. Returns the result of the execution attempt.
type MaxLossExecutionCallback func(ctx context.Context, exchange, symbol, side string, quantity decimal.Decimal) (*MaxLossExecutionResult, error)

// MaxLossMonitorConfig configures the MaxLossMonitorService.
type MaxLossMonitorConfig struct {
	MaxLossPct    float64
	PollInterval  time.Duration
	MaxStalePrice time.Duration
	ExchangeIDs   []string
}

// DefaultMaxLossMonitorConfig returns sensible defaults.
func DefaultMaxLossMonitorConfig() MaxLossMonitorConfig {
	return MaxLossMonitorConfig{
		MaxLossPct:    risk.MaxLossPctDefault,
		PollInterval:  1 * time.Second,
		MaxStalePrice: 10 * time.Second,
	}
}

// MaxLossMonitorService wraps the risk.MaxLossMonitor and integrates it with
// the live execution pipeline. It polls positions from the CCXT service,
// tracks them in the MaxLossMonitor, and invokes the execution callback when
// a position breaches the max-loss threshold.
type MaxLossMonitorService struct {
	config            MaxLossMonitorConfig
	ccxtService       ccxt.CCXTService
	monitor           *risk.MaxLossMonitor
	logger           *zaplogrus.Logger
	executionCallback MaxLossExecutionCallback

	stopCh  chan struct{}
	mu     sync.Mutex
	stopped bool
}

// NewMaxLossMonitorService creates a new MaxLossMonitorService.
func NewMaxLossMonitorService(
	config MaxLossMonitorConfig,
	ccxtService ccxt.CCXTService,
	logger *zaplogrus.Logger,
	executionCallback MaxLossExecutionCallback,
) *MaxLossMonitorService {
	if config.PollInterval <= 0 {
		config.PollInterval = 1 * time.Second
	}
	priceProvider := func(ctx context.Context, exchange, symbol string) (decimal.Decimal, error) {
		ticker, err := ccxtService.FetchSingleTicker(ctx, exchange, symbol)
		if err != nil {
			return decimal.Zero, err
		}
		if ticker == nil {
			return decimal.Zero, nil
		}
		price := ticker.GetPrice()
		if price.IsZero() {
			return decimal.Zero, nil
		}
		return price, nil
	}
	positionCloser := func(ctx context.Context, pos risk.PositionSnapshot, reason string) error {
		if executionCallback == nil {
			return nil
		}
		oppSide := "sell"
		if pos.Side == "sell" {
			oppSide = "buy"
		}
		_, err := executionCallback(ctx, pos.Exchange, pos.Symbol, oppSide, pos.Quantity)
		if err != nil {
			logger.WithError(err).Warnf("max-loss-monitor: execution callback failed for %s/%s reason=%s",
				pos.Exchange, pos.Symbol, reason)
		}
		return err
	}
	monitor := risk.NewMaxLossMonitor(risk.MaxLossMonitorConfig{
		MaxLossPct:    config.MaxLossPct,
		PollInterval:   config.PollInterval,
		MaxStalePrice: config.MaxStalePrice,
	}, priceProvider, positionCloser)

	return &MaxLossMonitorService{
		config:            config,
		ccxtService:       ccxtService,
		monitor:           monitor,
		logger:            logger,
		executionCallback: executionCallback,
		stopCh:            make(chan struct{}),
	}
}

// Start begins the monitoring loop and the underlying max-loss monitor.
func (s *MaxLossMonitorService) Start(ctx context.Context) {
	go s.monitor.Run(ctx)
	go s.run(ctx)
}

// Stop halts the monitoring loop and the underlying max-loss monitor.
func (s *MaxLossMonitorService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopCh)
}

// Monitor returns the underlying risk.MaxLossMonitor for testing/inspection.
func (s *MaxLossMonitorService) Monitor() *risk.MaxLossMonitor {
	return s.monitor
}

// TrackedCount returns the number of positions currently being monitored.
func (s *MaxLossMonitorService) TrackedCount() int {
	return s.monitor.TrackedCount()
}

// run is the main polling loop.
func (s *MaxLossMonitorService) run(ctx context.Context) {
	if s.config.PollInterval <= 0 {
		s.config.PollInterval = 1 * time.Second
	}
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	s.checkPositions(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkPositions(ctx)
		}
	}
}

// checkPositions fetches current positions from the CCXT service and
// tracks/refreshes them in the MaxLossMonitor.
func (s *MaxLossMonitorService) checkPositions(ctx context.Context) {
	exchanges := s.exchanges()
	for _, exchange := range exchanges {
		positions, err := s.ccxtService.FetchPositions(ctx, exchange)
		if err != nil {
			s.logger.WithError(err).Warnf("max-loss-monitor: failed to fetch positions for %s", exchange)
			continue
		}
		if positions == nil {
			continue
		}
		for i := range positions.Positions {
			pos := &positions.Positions[i]
			if pos.EntryPrice.IsZero() || pos.Size.IsZero() {
				continue
			}
			if pos.Side == "" {
				continue
			}
			side := normalizePositionSide(pos.Side)
			snapshot := risk.PositionSnapshot{
				Symbol:     pos.Symbol,
				Exchange:   positions.Exchange,
				Side:       side,
				EntryPrice: pos.EntryPrice,
				Quantity:   pos.Size,
				OpenedAt:   time.Now().UTC(),
			}
			s.monitor.TrackPosition(snapshot)
		}
	}
}

// exchanges returns the list of exchanges to monitor.
func (s *MaxLossMonitorService) exchanges() []string {
	if len(s.config.ExchangeIDs) > 0 {
		return s.config.ExchangeIDs
	}
	if s.ccxtService == nil {
		return nil
	}
	return s.ccxtService.GetSupportedExchanges()
}

// normalizePositionSide converts CCXT position side vocabulary
// ("long"/"short") to the risk engine vocabulary ("buy"/"sell").
// Unknown values are passed through unchanged.
func normalizePositionSide(side string) string {
	switch side {
	case "long", "buy":
		return "buy"
	case "short", "sell":
		return "sell"
	default:
		return side
	}
}
