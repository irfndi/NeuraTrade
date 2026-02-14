package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// MarketDataQualityConfig holds configuration for market data quality filters
type MarketDataQualityConfig struct {
	// Price outlier detection
	PriceMoveThresholdPercent decimal.Decimal // Default: 50% (0.5)
	PriceMoveWindowMinutes    int             // Default: 1 minute

	// Volume anomaly detection
	VolumeAnomalyThresholdPercent decimal.Decimal // Default: 300% (3.0) - 3x average
	VolumeWindowMinutes           int             // Default: 60 minutes
	MinVolumeSamples              int             // Default: 10 samples needed

	// Stale data detection
	StaleDataThresholdSeconds int // Default: 60 seconds

	// Cross-exchange validation
	CrossExchangeMaxDiffPercent decimal.Decimal // Default: 5% (0.05)
	ReferenceExchange           string          // Default: "binance"
}

// DefaultMarketDataQualityConfig returns default configuration
func DefaultMarketDataQualityConfig() MarketDataQualityConfig {
	return MarketDataQualityConfig{
		PriceMoveThresholdPercent:     decimal.NewFromFloat(0.5), // 50%
		PriceMoveWindowMinutes:        1,
		VolumeAnomalyThresholdPercent: decimal.NewFromFloat(3.0), // 300%
		VolumeWindowMinutes:           60,
		MinVolumeSamples:              10,
		StaleDataThresholdSeconds:     60,
		CrossExchangeMaxDiffPercent:   decimal.NewFromFloat(0.05), // 5%
		ReferenceExchange:             "binance",
	}
}

// QualityFlag represents types of quality issues detected
type QualityFlag string

const (
	QualityFlagOK                QualityFlag = "ok"
	QualityFlagPriceOutlier      QualityFlag = "price_outlier"
	QualityFlagVolumeAnomaly     QualityFlag = "volume_anomaly"
	QualityFlagStaleData         QualityFlag = "stale_data"
	QualityFlagCrossExchangeDiff QualityFlag = "cross_exchange_diff"
	QualityFlagInsufficientData  QualityFlag = "insufficient_data"
)

// MarketDataQualityResult contains the result of quality checks
type MarketDataQualityResult struct {
	Symbol        string              `json:"symbol"`
	Exchange      string              `json:"exchange"`
	Timestamp     time.Time           `json:"timestamp"`
	Flags         []QualityFlag       `json:"flags"`
	PriceChange   *decimal.Decimal    `json:"price_change,omitempty"`
	VolumeRatio   *decimal.Decimal    `json:"volume_ratio,omitempty"`
	AgeSeconds    int64               `json:"age_seconds"`
	CrossExchange *CrossExchangeCheck `json:"cross_exchange,omitempty"`
	Message       string              `json:"message,omitempty"`
}

// CrossExchangeCheck contains cross-exchange validation results
type CrossExchangeCheck struct {
	ReferencePrice decimal.Decimal `json:"reference_price"`
	TargetPrice    decimal.Decimal `json:"target_price"`
	DiffPercent    decimal.Decimal `json:"diff_percent"`
	IsValid        bool            `json:"is_valid"`
}

// MarketDataQualityFilter handles quality checks on market data
type MarketDataQualityFilter struct {
	config MarketDataQualityConfig
	logger *zap.Logger

	mu           sync.RWMutex
	priceHistory map[string][]PricePoint // symbol -> recent prices
	volumeStats  map[string]*VolumeStats // symbol -> volume statistics
	lastTick     map[string]time.Time    // symbol -> last tick time
}

// PricePoint represents a price at a point in time
type PricePoint struct {
	Timestamp time.Time
	Price     decimal.Decimal
}

// VolumeStats tracks volume statistics for anomaly detection
type VolumeStats struct {
	mu           sync.Mutex
	samples      []VolumeSample
	sampleWindow time.Duration
}

// VolumeSample represents a volume observation
type VolumeSample struct {
	Timestamp time.Time
	Volume    decimal.Decimal
}

// NewMarketDataQualityFilter creates a new quality filter
func NewMarketDataQualityFilter(config MarketDataQualityConfig, logger *zap.Logger) *MarketDataQualityFilter {
	if config.PriceMoveThresholdPercent.IsZero() {
		config.PriceMoveThresholdPercent = decimal.NewFromFloat(0.5)
	}
	if config.VolumeAnomalyThresholdPercent.IsZero() {
		config.VolumeAnomalyThresholdPercent = decimal.NewFromFloat(3.0)
	}
	if config.StaleDataThresholdSeconds == 0 {
		config.StaleDataThresholdSeconds = 60
	}
	if config.CrossExchangeMaxDiffPercent.IsZero() {
		config.CrossExchangeMaxDiffPercent = decimal.NewFromFloat(0.05)
	}

	return &MarketDataQualityFilter{
		config:       config,
		logger:       logger,
		priceHistory: make(map[string][]PricePoint),
		volumeStats:  make(map[string]*VolumeStats),
		lastTick:     make(map[string]time.Time),
	}
}

// CheckQuality performs all quality checks on market data
func (f *MarketDataQualityFilter) CheckQuality(
	ctx context.Context,
	symbol string,
	exchange string,
	price decimal.Decimal,
	volume decimal.Decimal,
	timestamp time.Time,
) *MarketDataQualityResult {
	result := &MarketDataQualityResult{
		Symbol:     symbol,
		Exchange:   exchange,
		Timestamp:  timestamp,
		Flags:      []QualityFlag{QualityFlagOK},
		AgeSeconds: int64(time.Since(timestamp).Seconds()),
	}

	// Check for stale data
	if f.checkStaleData(timestamp) {
		result.Flags = append(result.Flags, QualityFlagStaleData)
		result.Message = "data is stale"
	}

	// Check for price outliers
	if priceChange := f.checkPriceOutlier(symbol, exchange, price, timestamp); priceChange != nil {
		result.PriceChange = priceChange
		result.Flags = append(result.Flags, QualityFlagPriceOutlier)
		if result.Message == "" {
			result.Message = "price outlier detected"
		}
	}

	// Check for volume anomalies
	if volumeRatio := f.checkVolumeAnomaly(symbol, volume, timestamp); volumeRatio != nil {
		result.VolumeRatio = volumeRatio
		result.Flags = append(result.Flags, QualityFlagVolumeAnomaly)
		if result.Message == "" {
			result.Message = "volume anomaly detected"
		}
	}

	// Update tracking state
	f.updatePriceHistory(symbol, exchange, price, timestamp)
	f.updateVolumeStats(symbol, volume, timestamp)
	f.updateLastTick(symbol, timestamp)

	// If there are any flags beyond OK, mark as not OK
	if len(result.Flags) > 1 {
		result.Flags = result.Flags[1:] // Remove the OK flag
	}

	return result
}

// CheckCrossExchange validates price against another exchange
func (f *MarketDataQualityFilter) CheckCrossExchange(
	ctx context.Context,
	symbol string,
	exchange string,
	price decimal.Decimal,
) *CrossExchangeCheck {
	// Get reference price from reference exchange
	refPrice := f.getLastPrice(symbol, f.config.ReferenceExchange)
	if refPrice.IsZero() {
		return nil
	}

	diff := price.Sub(refPrice).Abs().Div(refPrice)
	isValid := diff.LessThan(f.config.CrossExchangeMaxDiffPercent)

	return &CrossExchangeCheck{
		ReferencePrice: refPrice,
		TargetPrice:    price,
		DiffPercent:    diff,
		IsValid:        isValid,
	}
}

// checkStaleData determines if data is stale
func (f *MarketDataQualityFilter) checkStaleData(timestamp time.Time) bool {
	age := time.Since(timestamp)
	return age.Seconds() > float64(f.config.StaleDataThresholdSeconds)
}

// checkPriceOutlier checks for suspicious price movements
func (f *MarketDataQualityFilter) checkPriceOutlier(symbol, exchange string, price decimal.Decimal, timestamp time.Time) *decimal.Decimal {
	f.mu.RLock()
	defer f.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", symbol, exchange)
	history, exists := f.priceHistory[key]
	if !exists || len(history) < 1 {
		return nil
	}

	// Get the most recent price before this one
	var previousPrice decimal.Decimal
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Timestamp.Before(timestamp) {
			previousPrice = history[i].Price
			break
		}
	}

	if previousPrice.IsZero() {
		return nil
	}

	// Calculate percentage change
	change := price.Sub(previousPrice).Abs().Div(previousPrice)

	// If change exceeds threshold, flag as outlier
	if change.GreaterThan(f.config.PriceMoveThresholdPercent) {
		return &change
	}

	return nil
}

// checkVolumeAnomaly checks for suspicious volume
func (f *MarketDataQualityFilter) checkVolumeAnomaly(symbol string, volume decimal.Decimal, timestamp time.Time) *decimal.Decimal {
	f.mu.RLock()
	stats, exists := f.volumeStats[symbol]
	f.mu.RUnlock()
	if !exists {
		return nil
	}

	stats.mu.Lock()
	defer stats.mu.Unlock()

	if len(stats.samples) < f.config.MinVolumeSamples {
		return nil
	}

	// Calculate average volume
	var total decimal.Decimal
	for _, s := range stats.samples {
		total = total.Add(s.Volume)
	}
	avgVolume := total.Div(decimal.NewFromInt(int64(len(stats.samples))))

	if avgVolume.IsZero() {
		return nil
	}

	// Calculate ratio
	ratio := volume.Div(avgVolume)

	// If volume exceeds threshold times average, flag as anomaly
	if ratio.GreaterThan(f.config.VolumeAnomalyThresholdPercent) {
		return &ratio
	}

	return nil
}

// updatePriceHistory adds a new price point to history
func (f *MarketDataQualityFilter) updatePriceHistory(symbol, exchange string, price decimal.Decimal, timestamp time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fmt.Sprintf("%s:%s", symbol, exchange)
	window := time.Duration(f.config.PriceMoveWindowMinutes) * time.Minute
	cutoff := timestamp.Add(-window)

	history := f.priceHistory[key]
	var newHistory []PricePoint

	for _, pp := range history {
		if pp.Timestamp.After(cutoff) {
			newHistory = append(newHistory, pp)
		}
	}

	newHistory = append(newHistory, PricePoint{
		Timestamp: timestamp,
		Price:     price,
	})

	// Keep only last 1000 points to prevent memory issues
	if len(newHistory) > 1000 {
		newHistory = newHistory[len(newHistory)-1000:]
	}

	f.priceHistory[key] = newHistory
}

// updateVolumeStats adds a new volume sample
func (f *MarketDataQualityFilter) updateVolumeStats(symbol string, volume decimal.Decimal, timestamp time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	stats, exists := f.volumeStats[symbol]
	if !exists {
		stats = &VolumeStats{
			sampleWindow: time.Duration(f.config.VolumeWindowMinutes) * time.Minute,
		}
		f.volumeStats[symbol] = stats
	}

	stats.mu.Lock()
	defer stats.mu.Unlock()

	window := timestamp.Add(-stats.sampleWindow)
	cutoff := window

	var newSamples []VolumeSample
	for _, s := range stats.samples {
		if s.Timestamp.After(cutoff) {
			newSamples = append(newSamples, s)
		}
	}

	newSamples = append(newSamples, VolumeSample{
		Timestamp: timestamp,
		Volume:    volume,
	})

	// Keep only last 1000 samples
	if len(newSamples) > 1000 {
		newSamples = newSamples[len(newSamples)-1000:]
	}

	stats.samples = newSamples
}

// updateLastTick records the last tick time
func (f *MarketDataQualityFilter) updateLastTick(symbol string, timestamp time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastTick[symbol] = timestamp
}

// getLastPrice gets the last recorded price for a symbol on an exchange
func (f *MarketDataQualityFilter) getLastPrice(symbol string, exchange string) decimal.Decimal {
	f.mu.RLock()
	defer f.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", symbol, exchange)
	history, exists := f.priceHistory[key]
	if !exists || len(history) == 0 {
		// Try without exchange suffix
		history, exists = f.priceHistory[symbol]
		if !exists || len(history) == 0 {
			return decimal.Zero
		}
		return history[len(history)-1].Price
	}

	return history[len(history)-1].Price
}

// GetStats returns current filter statistics
func (f *MarketDataQualityFilter) GetStats() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	stats := map[string]interface{}{
		"tracked_symbols": len(f.priceHistory),
		"volume_stats":    len(f.volumeStats),
	}

	symbolsWithData := 0
	for _, tick := range f.lastTick {
		if time.Since(tick) < time.Duration(f.config.StaleDataThresholdSeconds)*time.Second {
			symbolsWithData++
		}
	}
	stats["active_symbols"] = symbolsWithData

	return stats
}

// Reset clears all tracked data
func (f *MarketDataQualityFilter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.priceHistory = make(map[string][]PricePoint)
	f.volumeStats = make(map[string]*VolumeStats)
	f.lastTick = make(map[string]time.Time)
}
