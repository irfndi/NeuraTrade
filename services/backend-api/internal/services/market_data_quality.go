package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/irfndi/neuratrade/internal/logging"
)

// QualityEventType represents the type of quality event detected.
type QualityEventType string

const (
	// QualityEventPriceOutlier indicates a price outlier was detected.
	QualityEventPriceOutlier QualityEventType = "price_outlier"
	// QualityEventVolumeAnomaly indicates a volume anomaly was detected.
	QualityEventVolumeAnomaly QualityEventType = "volume_anomaly"
	// QualityEventStaleData indicates stale data was detected.
	QualityEventStaleData QualityEventType = "stale_data"
	// QualityEventCrossExchange indicates a cross-exchange validation failure.
	QualityEventCrossExchange QualityEventType = "cross_exchange_mismatch"
)

// QualityEvent represents a detected quality issue with market data.
type QualityEvent struct {
	Type       QualityEventType       `json:"type"`
	Symbol     string                 `json:"symbol"`
	Exchange   string                 `json:"exchange"`
	Message    string                 `json:"message"`
	Severity   string                 `json:"severity"` // low, medium, high, critical
	Details    map[string]interface{} `json:"details"`
	Timestamp  time.Time              `json:"timestamp"`
	IsBlocking bool                   `json:"is_blocking"` // true if data should be rejected
}

// MarketDataQualityConfig holds configuration for market data quality filters.
type MarketDataQualityConfig struct {
	// Price outlier detection
	PriceChangeThresholdPercent float64       `mapstructure:"price_change_threshold_percent" json:"price_change_threshold_percent"` // default 50%
	PriceChangeWindow           time.Duration `mapstructure:"price_change_window" json:"price_change_window"`                       // default 1min

	// Volume anomaly detection
	VolumeSpikeThresholdPercent float64 `mapstructure:"volume_spike_threshold_percent" json:"volume_spike_threshold_percent"` // default 500%
	VolumeDropThresholdPercent  float64 `mapstructure:"volume_drop_threshold_percent" json:"volume_drop_threshold_percent"`   // default 90%

	// Stale data detection
	StaleDataThreshold time.Duration `mapstructure:"stale_data_threshold" json:"stale_data_threshold"` // default 60s

	// Cross-exchange validation
	CrossExchangeMaxSpreadPercent float64 `mapstructure:"cross_exchange_max_spread_percent" json:"cross_exchange_max_spread_percent"` // default 5%

	// Exchanges for cross-validation
	PrimaryExchange    string   `mapstructure:"primary_exchange" json:"primary_exchange"`       // e.g., "binance"
	SecondaryExchanges []string `mapstructure:"secondary_exchanges" json:"secondary_exchanges"` // e.g., ["bybit", "okx"]
}

// DefaultMarketDataQualityConfig returns the default configuration.
func DefaultMarketDataQualityConfig() *MarketDataQualityConfig {
	return &MarketDataQualityConfig{
		PriceChangeThresholdPercent:   50.0,
		PriceChangeWindow:             1 * time.Minute,
		VolumeSpikeThresholdPercent:   500.0,
		VolumeDropThresholdPercent:    90.0,
		StaleDataThreshold:            60 * time.Second,
		CrossExchangeMaxSpreadPercent: 5.0,
		PrimaryExchange:               "binance",
		SecondaryExchanges:            []string{"bybit"},
	}
}

// MarketDataQualityService handles market data quality filtering and validation.
type MarketDataQualityService struct {
	config *MarketDataQualityConfig
	logger logging.Logger
	mu     sync.RWMutex
	// priceHistory stores the last known price for each symbol/exchange
	priceHistory map[string]QualityPriceData
	// volumeHistory stores the last known volume for each symbol/exchange
	volumeHistory map[string]QualityVolumeData
	// lastUpdate stores the last update time for each symbol/exchange
	lastUpdate map[string]time.Time
	// crossExchangePrices stores prices from different exchanges for cross-validation
	crossExchangePrices map[string]map[string]decimal.Decimal
}

// QualityPriceData holds price information for outlier detection.
type QualityPriceData struct {
	Price     decimal.Decimal
	Exchange  string
	Symbol    string
	Timestamp time.Time
}

// QualityVolumeData holds volume information for anomaly detection.
type QualityVolumeData struct {
	Volume    decimal.Decimal
	Exchange  string
	Symbol    string
	Timestamp time.Time
}

// NewMarketDataQualityService creates a new market data quality service.
func NewMarketDataQualityService(config *MarketDataQualityConfig, logger logging.Logger) *MarketDataQualityService {
	if config == nil {
		config = DefaultMarketDataQualityConfig()
	}
	svc := &MarketDataQualityService{
		config:              config,
		priceHistory:        make(map[string]QualityPriceData),
		volumeHistory:       make(map[string]QualityVolumeData),
		lastUpdate:          make(map[string]time.Time),
		crossExchangePrices: make(map[string]map[string]decimal.Decimal),
	}
	// Handle nil logger gracefully
	if logger != nil {
		svc.logger = logger
	} else {
		svc.logger = &nopLogger{}
	}
	return svc
}

// nopLogger is a no-op logger implementation.
type nopLogger struct{}

func (n *nopLogger) WithService(string) logging.Logger                    { return n }
func (n *nopLogger) WithComponent(string) logging.Logger                  { return n }
func (n *nopLogger) WithOperation(string) logging.Logger                  { return n }
func (n *nopLogger) WithRequestID(string) logging.Logger                  { return n }
func (n *nopLogger) WithUserID(string) logging.Logger                     { return n }
func (n *nopLogger) WithExchange(string) logging.Logger                   { return n }
func (n *nopLogger) WithSymbol(string) logging.Logger                     { return n }
func (n *nopLogger) WithError(error) logging.Logger                       { return n }
func (n *nopLogger) WithMetrics(map[string]interface{}) logging.Logger    { return n }
func (n *nopLogger) WithFields(map[string]interface{}) logging.Logger     { return n }
func (n *nopLogger) Info(string, ...interface{})                          {}
func (n *nopLogger) Warn(string, ...interface{})                          {}
func (n *nopLogger) Error(string, ...interface{})                         {}
func (n *nopLogger) Debug(string, ...interface{})                         {}
func (n *nopLogger) Fatal(string, ...interface{})                         {}
func (n *nopLogger) LogStartup(string, string, int)                       {}
func (n *nopLogger) LogShutdown(string, string)                           {}
func (n *nopLogger) LogPerformanceMetrics(string, map[string]interface{}) {}
func (n *nopLogger) LogResourceStats(string, map[string]interface{})      {}
func (n *nopLogger) LogCacheOperation(string, string, bool, int64)        {}
func (n *nopLogger) LogDatabaseOperation(string, string, int64, int64)    {}
func (n *nopLogger) LogAPIRequest(string, string, int, int64, string)     {}
func (n *nopLogger) LogSecurityEvent(string, map[string]interface{})      {}
func (n *nopLogger) LogTradeExecution(string, string, string, decimal.Decimal, decimal.Decimal, decimal.Decimal, string) {
}
func (n *nopLogger) LogArbitrageOpportunity(string, decimal.Decimal, []string) {}
func (n *nopLogger) LogSignalGeneration(string, string, float64)               {}
func (n *nopLogger) LogNotification(string, string, bool)                      {}
func (n *nopLogger) LogTaskExecution(string, string, string, int64)            {}
func (n *nopLogger) LogWebSocketEvent(string, string, map[string]interface{})  {}
func (n *nopLogger) LogCronJobExecution(string, string, int64, error)          {}
func (n *nopLogger) LogRetry(string, int, int, error)                          {}
func (n *nopLogger) LogCircuitBreakerState(string, string, int64, int64)       {}
func (n *nopLogger) LogBusinessEvent(string, map[string]interface{})           {}
func (n *nopLogger) Logger() *zap.Logger                                       { return nil }
func (n *nopLogger) SetLevel(string)                                           {}

// FilterResult represents the result of filtering market data.
type FilterResult struct {
	Passed       bool           `json:"passed"`
	Events       []QualityEvent `json:"events"`
	ShouldReject bool           `json:"should_reject"`
}

// Process processes market data through all quality filters.
func (s *MarketDataQualityService) Process(exchange, symbol string, price, volume decimal.Decimal, timestamp time.Time) *FilterResult {
	result := &FilterResult{
		Passed: true,
		Events: make([]QualityEvent, 0),
	}

	// Check each filter
	if events := s.checkPriceOutlier(exchange, symbol, price, timestamp); len(events) > 0 {
		result.Events = append(result.Events, events...)
		for _, e := range events {
			if e.IsBlocking {
				result.Passed = false
				result.ShouldReject = true
			}
		}
	}

	if events := s.checkVolumeAnomaly(exchange, symbol, volume, timestamp); len(events) > 0 {
		result.Events = append(result.Events, events...)
		// Volume anomalies are warnings, not blocking
	}

	if events := s.checkStaleData(exchange, symbol, timestamp); len(events) > 0 {
		result.Events = append(result.Events, events...)
		for _, e := range events {
			if e.IsBlocking {
				result.Passed = false
				result.ShouldReject = true
			}
		}
	}

	// Update historical data
	s.updatePriceHistory(exchange, symbol, price, timestamp)
	s.updateVolumeHistory(exchange, symbol, volume, timestamp)
	s.updateLastUpdate(exchange, symbol, timestamp)
	s.updateCrossExchangePrice(exchange, symbol, price)

	// Cross-exchange validation (only when we have data from multiple exchanges)
	if events := s.checkCrossExchangeValidation(symbol); len(events) > 0 {
		result.Events = append(result.Events, events...)
		for _, e := range events {
			if e.IsBlocking {
				result.Passed = false
				result.ShouldReject = true
			}
		}
	}

	return result
}

// checkPriceOutlier checks for significant price changes (>50% in 1min).
func (s *MarketDataQualityService) checkPriceOutlier(exchange, symbol string, price decimal.Decimal, timestamp time.Time) []QualityEvent {
	events := make([]QualityEvent, 0)
	key := fmt.Sprintf("%s:%s", exchange, symbol)

	s.mu.RLock()
	prevData, exists := s.priceHistory[key]
	s.mu.RUnlock()

	if !exists {
		// First data point, can't detect outlier
		return events
	}

	// Check if within the time window
	timeDiff := timestamp.Sub(prevData.Timestamp)
	if timeDiff > s.config.PriceChangeWindow {
		// Data is too old, reset history
		return events
	}

	// Calculate percentage change
	if prevData.Price.IsZero() || prevData.Price.IsNegative() {
		return events
	}

	changePercent := price.Sub(prevData.Price).Div(prevData.Price).Abs().Mul(decimal.NewFromInt(100))
	threshold := decimal.NewFromFloat(s.config.PriceChangeThresholdPercent)

	if changePercent.GreaterThan(threshold) {
		event := QualityEvent{
			Type:       QualityEventPriceOutlier,
			Symbol:     symbol,
			Exchange:   exchange,
			Message:    fmt.Sprintf("Price change %.2f%% exceeds threshold %.2f%% in %v", changePercent.InexactFloat64(), s.config.PriceChangeThresholdPercent, timeDiff),
			Severity:   "high",
			Details:    map[string]interface{}{"previous_price": prevData.Price.String(), "current_price": price.String(), "change_percent": changePercent.InexactFloat64()},
			Timestamp:  timestamp,
			IsBlocking: true, // Reject extreme price moves
		}
		events = append(events, event)
		s.logger.WithFields(map[string]interface{}{"exchange": exchange, "symbol": symbol, "change_percent": changePercent.InexactFloat64()}).Warn("Price outlier detected")
	}

	return events
}

// checkVolumeAnomaly checks for suspicious volume patterns.
func (s *MarketDataQualityService) checkVolumeAnomaly(exchange, symbol string, volume decimal.Decimal, timestamp time.Time) []QualityEvent {
	events := make([]QualityEvent, 0)
	key := fmt.Sprintf("%s:%s", exchange, symbol)

	s.mu.RLock()
	prevData, exists := s.volumeHistory[key]
	s.mu.RUnlock()

	if !exists || prevData.Volume.IsZero() || prevData.Volume.IsNegative() {
		// First data point or invalid previous volume
		return events
	}

	// Calculate volume change percentage
	volumeChange := volume.Sub(prevData.Volume).Div(prevData.Volume).Abs().Mul(decimal.NewFromInt(100))
	spikeThreshold := decimal.NewFromFloat(s.config.VolumeSpikeThresholdPercent)
	dropThreshold := decimal.NewFromFloat(s.config.VolumeDropThresholdPercent)

	if volumeChange.GreaterThan(spikeThreshold) {
		// Volume spike - possible wash trading
		event := QualityEvent{
			Type:       QualityEventVolumeAnomaly,
			Symbol:     symbol,
			Exchange:   exchange,
			Message:    fmt.Sprintf("Volume spike %.2f%% exceeds threshold %.2f%% - possible wash trading", volumeChange.InexactFloat64(), s.config.VolumeSpikeThresholdPercent),
			Severity:   "medium",
			Details:    map[string]interface{}{"previous_volume": prevData.Volume.String(), "current_volume": volume.String(), "change_percent": volumeChange.InexactFloat64()},
			Timestamp:  timestamp,
			IsBlocking: false, // Warning only
		}
		events = append(events, event)
		s.logger.WithFields(map[string]interface{}{"exchange": exchange, "symbol": symbol, "volume_change": volumeChange.InexactFloat64()}).Warn("Volume anomaly detected")
	}

	if volumeChange.GreaterThan(dropThreshold) {
		// Volume drop - could be exchange issue
		event := QualityEvent{
			Type:       QualityEventVolumeAnomaly,
			Symbol:     symbol,
			Exchange:   exchange,
			Message:    fmt.Sprintf("Volume drop %.2f%% exceeds threshold %.2f%% - possible data issue", volumeChange.InexactFloat64(), s.config.VolumeDropThresholdPercent),
			Severity:   "low",
			Details:    map[string]interface{}{"previous_volume": prevData.Volume.String(), "current_volume": volume.String(), "change_percent": volumeChange.InexactFloat64()},
			Timestamp:  timestamp,
			IsBlocking: false, // Warning only
		}
		events = append(events, event)
	}

	return events
}

// checkStaleData checks if data is stale.
func (s *MarketDataQualityService) checkStaleData(exchange, symbol string, timestamp time.Time) []QualityEvent {
	events := make([]QualityEvent, 0)
	key := fmt.Sprintf("%s:%s", exchange, symbol)

	s.mu.RLock()
	lastTime, exists := s.lastUpdate[key]
	s.mu.RUnlock()

	now := time.Now()

	if !exists {
		// First data, not stale
		return events
	}

	timeSinceLastUpdate := now.Sub(lastTime)

	if timeSinceLastUpdate > s.config.StaleDataThreshold {
		event := QualityEvent{
			Type:       QualityEventStaleData,
			Symbol:     symbol,
			Exchange:   exchange,
			Message:    fmt.Sprintf("No data received for %v (threshold: %v)", timeSinceLastUpdate, s.config.StaleDataThreshold),
			Severity:   "high",
			Details:    map[string]interface{}{"time_since_last_update": timeSinceLastUpdate.Seconds(), "threshold_seconds": s.config.StaleDataThreshold.Seconds()},
			Timestamp:  now,
			IsBlocking: true, // Block stale data
		}
		events = append(events, event)
		s.logger.WithFields(map[string]interface{}{"exchange": exchange, "symbol": symbol, "stale_duration": timeSinceLastUpdate.Seconds()}).Warn("Stale data detected")
	}

	return events
}

// checkCrossExchangeValidation validates prices across exchanges.
func (s *MarketDataQualityService) checkCrossExchangeValidation(symbol string) []QualityEvent {
	events := make([]QualityEvent, 0)

	s.mu.RLock()
	prices, exists := s.crossExchangePrices[symbol]
	s.mu.RUnlock()

	if !exists || len(prices) < 2 {
		// Need at least 2 exchanges for cross-validation
		return events
	}

	// Get primary and secondary exchange prices
	primaryPrice, primaryExists := prices[s.config.PrimaryExchange]
	if !primaryExists {
		return events
	}

	spreadThreshold := decimal.NewFromFloat(s.config.CrossExchangeMaxSpreadPercent)

	for exchange, price := range prices {
		if exchange == s.config.PrimaryExchange {
			continue
		}

		// Check if this is a secondary exchange we care about
		isSecondary := false
		for _, sec := range s.config.SecondaryExchanges {
			if exchange == sec {
				isSecondary = true
				break
			}
		}
		if !isSecondary {
			continue
		}

		// Calculate spread
		if primaryPrice.IsZero() || price.IsZero() {
			continue
		}

		spread := price.Sub(primaryPrice).Div(primaryPrice).Abs().Mul(decimal.NewFromInt(100))

		if spread.GreaterThan(spreadThreshold) {
			event := QualityEvent{
				Type:       QualityEventCrossExchange,
				Symbol:     symbol,
				Exchange:   fmt.Sprintf("%s vs %s", s.config.PrimaryExchange, exchange),
				Message:    fmt.Sprintf("Price spread %.2f%% between %s (%.8f) and %s (%.8f) exceeds threshold %.2f%%", spread.InexactFloat64(), s.config.PrimaryExchange, primaryPrice.InexactFloat64(), exchange, price.InexactFloat64(), s.config.CrossExchangeMaxSpreadPercent),
				Severity:   "medium",
				Details:    map[string]interface{}{"primary_price": primaryPrice.String(), "secondary_price": price.String(), "spread_percent": spread.InexactFloat64(), "primary_exchange": s.config.PrimaryExchange, "secondary_exchange": exchange},
				Timestamp:  time.Now(),
				IsBlocking: false, // Warning only - could be legitimate arbitrage opportunity
			}
			events = append(events, event)
			s.logger.WithFields(map[string]interface{}{"symbol": symbol, "spread": spread.InexactFloat64()}).Warn("Cross-exchange price mismatch detected")
		}
	}

	return events
}

// updatePriceHistory updates the price history for a symbol/exchange.
func (s *MarketDataQualityService) updatePriceHistory(exchange, symbol string, price decimal.Decimal, timestamp time.Time) {
	key := fmt.Sprintf("%s:%s", exchange, symbol)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.priceHistory[key] = QualityPriceData{
		Price:     price,
		Exchange:  exchange,
		Symbol:    symbol,
		Timestamp: timestamp,
	}
}

// updateVolumeHistory updates the volume history for a symbol/exchange.
func (s *MarketDataQualityService) updateVolumeHistory(exchange, symbol string, volume decimal.Decimal, timestamp time.Time) {
	key := fmt.Sprintf("%s:%s", exchange, symbol)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.volumeHistory[key] = QualityVolumeData{
		Volume:    volume,
		Exchange:  exchange,
		Symbol:    symbol,
		Timestamp: timestamp,
	}
}

// updateLastUpdate updates the last update time for a symbol/exchange.
func (s *MarketDataQualityService) updateLastUpdate(exchange, symbol string, timestamp time.Time) {
	key := fmt.Sprintf("%s:%s", exchange, symbol)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUpdate[key] = timestamp
}

// updateCrossExchangePrice updates the cross-exchange price for validation.
func (s *MarketDataQualityService) updateCrossExchangePrice(exchange, symbol string, price decimal.Decimal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.crossExchangePrices[symbol] == nil {
		s.crossExchangePrices[symbol] = make(map[string]decimal.Decimal)
	}
	s.crossExchangePrices[symbol][exchange] = price
}

// GetConfig returns the current configuration.
func (s *MarketDataQualityService) GetConfig() *MarketDataQualityConfig {
	return s.config
}

// UpdateConfig updates the configuration.
func (s *MarketDataQualityService) UpdateConfig(config *MarketDataQualityConfig) {
	if config == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// Reset clears all historical data.
func (s *MarketDataQualityService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.priceHistory = make(map[string]QualityPriceData)
	s.volumeHistory = make(map[string]QualityVolumeData)
	s.lastUpdate = make(map[string]time.Time)
	s.crossExchangePrices = make(map[string]map[string]decimal.Decimal)
}

// GetStats returns current statistics (for monitoring).
func (s *MarketDataQualityService) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"price_history_count":    len(s.priceHistory),
		"volume_history_count":   len(s.volumeHistory),
		"last_update_count":      len(s.lastUpdate),
		"cross_exchange_symbols": len(s.crossExchangePrices),
	}
}

// ProcessFromMarketPrice processes a MarketPrice model through quality filters.
func (s *MarketDataQualityService) ProcessFromMarketPrice(ctx context.Context, price *MarketPrice) *FilterResult {
	if price == nil {
		return &FilterResult{Passed: false, ShouldReject: true, Events: []QualityEvent{{
			Type:       "invalid_input",
			Message:    "nil market price",
			Severity:   "critical",
			IsBlocking: true,
		}}}
	}

	return s.Process(price.ExchangeName, price.Symbol, price.Price, price.Volume, price.Timestamp)
}

// MarketPrice represents the market price model interface.
type MarketPrice struct {
	ExchangeName string
	Symbol       string
	Price        decimal.Decimal
	Volume       decimal.Decimal
	Timestamp    time.Time
}
