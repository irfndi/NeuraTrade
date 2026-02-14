package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

// OrderBookImbalanceConfig holds configuration for order book imbalance detection.
type OrderBookImbalanceConfig struct {
	// Detection thresholds
	ImbalanceThreshold decimal.Decimal // Minimum imbalance to trigger signal (e.g., 0.2 = 20%)
	MinDepthUSD        decimal.Decimal // Minimum order book depth in USD to consider valid
	MaxSpreadPct       decimal.Decimal // Maximum spread % to consider valid signal

	// Signal scoring weights
	ImbalanceWeight decimal.Decimal // Weight for imbalance in signal strength (0-1)
	DepthWeight     decimal.Decimal // Weight for depth in signal strength (0-1)
	SpreadWeight    decimal.Decimal // Weight for spread in signal strength (0-1)

	// Time windows
	LookbackWindow       time.Duration // How long to look back for signal persistence
	MinSignalPersistence time.Duration // Minimum time imbalance must persist

	// Rate limiting
	MinSignalInterval time.Duration // Minimum time between signals for same symbol

	// Symbols to monitor (empty = all)
	MonitoredSymbols []string
}

// DefaultOrderBookImbalanceConfig returns default configuration.
func DefaultOrderBookImbalanceConfig() OrderBookImbalanceConfig {
	return OrderBookImbalanceConfig{
		ImbalanceThreshold:   decimal.NewFromFloat(0.2),  // 20% imbalance
		MinDepthUSD:          decimal.NewFromInt(100000), // $100k minimum depth
		MaxSpreadPct:         decimal.NewFromFloat(0.5),  // 0.5% max spread
		ImbalanceWeight:      decimal.NewFromFloat(0.5),  // 50% weight
		DepthWeight:          decimal.NewFromFloat(0.3),  // 30% weight
		SpreadWeight:         decimal.NewFromFloat(0.2),  // 20% weight
		LookbackWindow:       5 * time.Minute,
		MinSignalPersistence: 10 * time.Second,
		MinSignalInterval:    1 * time.Minute,
		MonitoredSymbols:     []string{},
	}
}

// OrderBookImbalanceSignal represents a detected imbalance signal.
type OrderBookImbalanceSignal struct {
	ID           string                 `json:"id"`
	Symbol       string                 `json:"symbol"`
	Exchange     string                 `json:"exchange"`
	Direction    string                 `json:"direction"`     // "bullish" or "bearish"
	Strength     string                 `json:"strength"`      // "weak", "medium", "strong"
	ImbalancePct decimal.Decimal        `json:"imbalance_pct"` // Raw imbalance percentage
	BidDepthUSD  decimal.Decimal        `json:"bid_depth_usd"`
	AskDepthUSD  decimal.Decimal        `json:"ask_depth_usd"`
	SpreadPct    decimal.Decimal        `json:"spread_pct"`
	Score        decimal.Decimal        `json:"score"`      // 0-100 signal score
	Confidence   decimal.Decimal        `json:"confidence"` // 0-1 confidence level
	Metrics      *ccxt.OrderBookMetrics `json:"metrics"`
	DetectedAt   time.Time              `json:"detected_at"`
	ExpiresAt    time.Time              `json:"expires_at"`
}

// IsValid checks if the signal is still valid (not expired).
func (s *OrderBookImbalanceSignal) IsValid() bool {
	return time.Now().Before(s.ExpiresAt)
}

// OrderBookImbalanceDetector detects order book imbalances and generates trading signals.
type OrderBookImbalanceDetector struct {
	config      OrderBookImbalanceConfig
	ccxtService ccxt.CCXTService
	logger      *zaplogrus.Logger

	// Signal history for persistence tracking
	signalHistory map[string][]*OrderBookImbalanceSignal // symbol -> signals
	historyMu     sync.RWMutex

	// Last signal time per symbol for rate limiting
	lastSignalTime map[string]time.Time
	rateMu         sync.RWMutex
}

// NewOrderBookImbalanceDetector creates a new detector.
func NewOrderBookImbalanceDetector(
	config OrderBookImbalanceConfig,
	ccxtService ccxt.CCXTService,
	logger *zaplogrus.Logger,
) *OrderBookImbalanceDetector {
	return &OrderBookImbalanceDetector{
		config:         config,
		ccxtService:    ccxtService,
		logger:         logger,
		signalHistory:  make(map[string][]*OrderBookImbalanceSignal),
		lastSignalTime: make(map[string]time.Time),
	}
}

// Detect analyzes order book and returns imbalance signal if detected.
func (d *OrderBookImbalanceDetector) Detect(ctx context.Context, exchange, symbol string) (*OrderBookImbalanceSignal, error) {
	// Check if symbol should be monitored
	if !d.shouldMonitorSymbol(symbol) {
		return nil, nil
	}

	// Fetch order book metrics
	metrics, err := d.ccxtService.CalculateOrderBookMetrics(ctx, exchange, symbol, 50)
	if err != nil {
		return nil, fmt.Errorf("failed to get order book metrics: %w", err)
	}

	// Validate minimum depth
	if metrics == nil {
		return nil, nil
	}

	totalDepth := metrics.BidDepth1Pct.Add(metrics.AskDepth1Pct)
	if totalDepth.LessThan(d.config.MinDepthUSD) {
		d.logger.Debug("Insufficient order book depth",
			"symbol", symbol,
			"exchange", exchange,
			"depth", totalDepth,
			"min_required", d.config.MinDepthUSD)
		return nil, nil
	}

	// Validate spread
	spreadPct := d.calculateSpread(metrics)
	if spreadPct.GreaterThan(d.config.MaxSpreadPct) {
		d.logger.Debug("Spread too high",
			"symbol", symbol,
			"exchange", exchange,
			"spread_pct", spreadPct,
			"max_allowed", d.config.MaxSpreadPct)
		return nil, nil
	}

	// Check for significant imbalance
	imbalance := metrics.Imbalance1Pct
	absImbalance := imbalance.Abs()

	if absImbalance.LessThan(d.config.ImbalanceThreshold) {
		// No significant imbalance
		return nil, nil
	}

	// Determine direction
	direction := "neutral"
	if imbalance.GreaterThan(decimal.Zero) {
		direction = "bullish"
	} else if imbalance.LessThan(decimal.Zero) {
		direction = "bearish"
	}

	// Calculate signal strength
	strength, score := d.calculateSignalStrength(metrics, imbalance)

	// Check rate limiting
	if !d.canEmitSignal(symbol) {
		d.logger.Debug("Signal rate limited",
			"symbol", symbol,
			"exchange", exchange)
		return nil, nil
	}

	// Create signal
	signal := &OrderBookImbalanceSignal{
		ID:           generateSignalID(exchange, symbol),
		Symbol:       symbol,
		Exchange:     exchange,
		Direction:    direction,
		Strength:     strength,
		ImbalancePct: imbalance,
		BidDepthUSD:  metrics.BidDepth1Pct,
		AskDepthUSD:  metrics.AskDepth1Pct,
		SpreadPct:    spreadPct,
		Score:        score,
		Confidence:   d.calculateConfidence(metrics, score),
		Metrics:      metrics,
		DetectedAt:   time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// Record signal
	d.recordSignal(symbol, signal)
	d.updateLastSignalTime(symbol)

	d.logger.Info("Order book imbalance detected",
		"symbol", symbol,
		"exchange", exchange,
		"direction", direction,
		"strength", strength,
		"imbalance_pct", imbalance,
		"score", score)

	return signal, nil
}

// DetectBatch analyzes multiple symbols for imbalances.
func (d *OrderBookImbalanceDetector) DetectBatch(ctx context.Context, exchange string, symbols []string) ([]*OrderBookImbalanceSignal, error) {
	signals := make([]*OrderBookImbalanceSignal, 0)

	for _, symbol := range symbols {
		signal, err := d.Detect(ctx, exchange, symbol)
		if err != nil {
			d.logger.WithError(err).Warn("Failed to detect imbalance",
				"symbol", symbol,
				"exchange", exchange)
			continue
		}
		if signal != nil {
			signals = append(signals, signal)
		}
	}

	return signals, nil
}

// GetSignalHistory returns signal history for a symbol.
func (d *OrderBookImbalanceDetector) GetSignalHistory(symbol string, limit int) []*OrderBookImbalanceSignal {
	d.historyMu.RLock()
	defer d.historyMu.RUnlock()

	history, exists := d.signalHistory[symbol]
	if !exists {
		return []*OrderBookImbalanceSignal{}
	}

	// Return most recent signals up to limit
	if limit <= 0 || limit > len(history) {
		limit = len(history)
	}

	start := len(history) - limit
	if start < 0 {
		start = 0
	}

	result := make([]*OrderBookImbalanceSignal, limit)
	copy(result, history[start:])

	return result
}

// shouldMonitorSymbol checks if a symbol should be monitored.
func (d *OrderBookImbalanceDetector) shouldMonitorSymbol(symbol string) bool {
	if len(d.config.MonitoredSymbols) == 0 {
		return true // Monitor all symbols
	}

	for _, monitored := range d.config.MonitoredSymbols {
		if monitored == symbol {
			return true
		}
	}

	return false
}

// calculateSignalStrength calculates signal strength based on imbalance, depth, and spread.
func (d *OrderBookImbalanceDetector) calculateSignalStrength(
	metrics *ccxt.OrderBookMetrics,
	imbalance decimal.Decimal,
) (string, decimal.Decimal) {
	absImbalance := imbalance.Abs()

	// Normalize imbalance to 0-100 scale
	// 20% imbalance = 50 score, 40% imbalance = 100 score
	imbalanceScore := absImbalance.Div(decimal.NewFromFloat(0.4)).Mul(decimal.NewFromInt(100))
	if imbalanceScore.GreaterThan(decimal.NewFromInt(100)) {
		imbalanceScore = decimal.NewFromInt(100)
	}

	// Depth score (0-100) based on total depth
	totalDepth := metrics.BidDepth1Pct.Add(metrics.AskDepth1Pct)
	depthScore := totalDepth.Div(decimal.NewFromInt(500000)).Mul(decimal.NewFromInt(100))
	if depthScore.GreaterThan(decimal.NewFromInt(100)) {
		depthScore = decimal.NewFromInt(100)
	}

	// Spread score (0-100) - tighter spread = higher score
	spreadPct := d.calculateSpread(metrics)
	spreadScore := decimal.NewFromInt(100).Sub(spreadPct.Mul(decimal.NewFromInt(200)))
	if spreadScore.LessThan(decimal.Zero) {
		spreadScore = decimal.Zero
	}

	// Weighted composite score
	score := imbalanceScore.Mul(d.config.ImbalanceWeight).
		Add(depthScore.Mul(d.config.DepthWeight)).
		Add(spreadScore.Mul(d.config.SpreadWeight))

	// Determine strength category
	strength := "weak"
	if score.GreaterThanOrEqual(decimal.NewFromInt(70)) {
		strength = "strong"
	} else if score.GreaterThanOrEqual(decimal.NewFromInt(40)) {
		strength = "medium"
	}

	return strength, score
}

// calculateConfidence calculates confidence level based on data quality.
func (d *OrderBookImbalanceDetector) calculateConfidence(
	metrics *ccxt.OrderBookMetrics,
	score decimal.Decimal,
) decimal.Decimal {
	// Base confidence from signal score
	baseConfidence := score.Div(decimal.NewFromInt(100))

	// Adjust based on data quality factors
	qualityFactor := decimal.NewFromInt(1)

	// Reduce confidence if order book is thin
	totalDepth := metrics.BidDepth1Pct.Add(metrics.AskDepth1Pct)
	if totalDepth.LessThan(decimal.NewFromInt(200000)) {
		qualityFactor = qualityFactor.Mul(decimal.NewFromFloat(0.9))
	}

	// Reduce confidence if spread is wide
	spreadPct := d.calculateSpread(metrics)
	if spreadPct.GreaterThan(decimal.NewFromFloat(0.3)) {
		qualityFactor = qualityFactor.Mul(decimal.NewFromFloat(0.9))
	}

	// Reduce confidence if few levels
	if metrics.BidLevels < 5 || metrics.AskLevels < 5 {
		qualityFactor = qualityFactor.Mul(decimal.NewFromFloat(0.95))
	}

	confidence := baseConfidence.Mul(qualityFactor)
	if confidence.GreaterThan(decimal.NewFromInt(1)) {
		confidence = decimal.NewFromInt(1)
	}

	return confidence
}

// calculateSpread calculates the bid-ask spread as a percentage of mid price.
func (d *OrderBookImbalanceDetector) calculateSpread(metrics *ccxt.OrderBookMetrics) decimal.Decimal {
	if metrics.MidPrice.IsZero() {
		return decimal.Zero
	}
	spread := metrics.BestAsk.Sub(metrics.BestBid)
	return spread.Div(metrics.MidPrice).Mul(decimal.NewFromInt(100))
}

// canEmitSignal checks if enough time has passed since last signal.
func (d *OrderBookImbalanceDetector) canEmitSignal(symbol string) bool {
	d.rateMu.RLock()
	lastTime, exists := d.lastSignalTime[symbol]
	d.rateMu.RUnlock()

	if !exists {
		return true
	}

	return time.Since(lastTime) >= d.config.MinSignalInterval
}

// updateLastSignalTime updates the last signal time for a symbol.
func (d *OrderBookImbalanceDetector) updateLastSignalTime(symbol string) {
	d.rateMu.Lock()
	defer d.rateMu.Unlock()
	d.lastSignalTime[symbol] = time.Now()
}

// recordSignal adds a signal to history.
func (d *OrderBookImbalanceDetector) recordSignal(symbol string, signal *OrderBookImbalanceSignal) {
	d.historyMu.Lock()
	defer d.historyMu.Unlock()

	// Initialize history if needed
	if _, exists := d.signalHistory[symbol]; !exists {
		d.signalHistory[symbol] = make([]*OrderBookImbalanceSignal, 0)
	}

	// Add signal
	d.signalHistory[symbol] = append(d.signalHistory[symbol], signal)

	// Trim old signals (keep last 100 per symbol)
	if len(d.signalHistory[symbol]) > 100 {
		d.signalHistory[symbol] = d.signalHistory[symbol][len(d.signalHistory[symbol])-100:]
	}
}

// generateSignalID creates a unique signal ID.
func generateSignalID(exchange, symbol string) string {
	return fmt.Sprintf("obi-%s-%s-%d", exchange, symbol, time.Now().UnixNano())
}
