package services

import (
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

// convertMarketPriceInterfacesToModels converts CCXT MarketPriceInterface to models.MarketPrice
func (c *CollectorService) convertMarketPriceInterfacesToModels(interfaceData []ccxt.MarketPriceInterface) []models.MarketPrice {
	if interfaceData == nil {
		return make([]models.MarketPrice, 0)
	}

	marketData := make([]models.MarketPrice, 0, len(interfaceData))
	for _, item := range interfaceData {
		marketData = append(marketData, models.MarketPrice{
			ExchangeID:   0, // Will be filled later
			ExchangeName: item.GetExchangeName(),
			Symbol:       item.GetSymbol(),
			Price:        decimal.NewFromFloat(item.GetPrice()),
			Bid:          decimal.NewFromFloat(item.GetBid()),
			Ask:          decimal.NewFromFloat(item.GetAsk()),
			Volume:       decimal.NewFromFloat(item.GetVolume()),
			Timestamp:    item.GetTimestamp(),
		})
	}
	return marketData
}

// convertMarketPriceInterfaceToModel converts a single CCXT MarketPriceInterface to models.MarketPrice
func (c *CollectorService) convertMarketPriceInterfaceToModel(interfaceData ccxt.MarketPriceInterface) *models.MarketPrice {
	if interfaceData == nil {
		return nil
	}
	return &models.MarketPrice{
		ExchangeID:   0, // Will be filled later
		ExchangeName: interfaceData.GetExchangeName(),
		Symbol:       interfaceData.GetSymbol(),
		Price:        decimal.NewFromFloat(interfaceData.GetPrice()),
		Bid:          decimal.NewFromFloat(interfaceData.GetBid()),
		Ask:          decimal.NewFromFloat(interfaceData.GetAsk()),
		Volume:       decimal.NewFromFloat(interfaceData.GetVolume()),
		Timestamp:    interfaceData.GetTimestamp(),
	}
}

// CollectorConfig holds configuration for the collector service.
type CollectorConfig struct {
	// IntervalSeconds is the data collection interval.
	IntervalSeconds int `mapstructure:"interval_seconds"`
	// MaxErrors is the maximum number of errors before stopping a worker.
	MaxErrors int `mapstructure:"max_errors"`
}

// BackfillConfig holds configuration for historical data backfill.
type BackfillConfig struct {
	// Enabled indicates if backfill is enabled.
	Enabled bool `yaml:"enabled" default:"true"`
	// BackfillHours is the number of hours to backfill.
	BackfillHours int `yaml:"backfill_hours" default:"6"`
	// MinDataThresholdHours is the minimum data required to skip backfill.
	MinDataThresholdHours int `yaml:"min_data_threshold_hours" default:"12"`
	// BatchSize is the number of items per batch.
	BatchSize int `yaml:"batch_size" default:"50"`
	// DelayBetweenBatches is the delay between batches in ms.
	DelayBetweenBatches int `yaml:"delay_between_batches_ms" default:"100"`
}

// SymbolCacheEntry represents a cached entry for exchange symbols.
type SymbolCacheEntry struct {
	// Symbols is the list of symbols.
	Symbols []string
	// ExpiresAt is the expiration time.
	ExpiresAt time.Time
}

// SymbolCacheInterface defines the interface for symbol caching.
type SymbolCacheInterface interface {
	// Get retrieves cached symbols.
	Get(exchangeID string) ([]string, bool)
	// Set caches symbols.
	Set(exchangeID string, symbols []string)
	// GetStats retrieves cache statistics.
	GetStats() cache.SymbolCacheStats
	// LogStats logs cache statistics.
	LogStats()
}

// SymbolCache manages cached active symbols for exchanges.
type SymbolCache struct {
	cache  map[string]*SymbolCacheEntry
	mu     sync.RWMutex
	ttl    time.Duration
	stats  cache.SymbolCacheStats
	logger logging.Logger
}

// BlacklistCacheEntry represents a cached entry for blacklisted symbols

// ExchangeCapabilityEntry represents cached exchange capability information.
type ExchangeCapabilityEntry struct {
	// SupportsFundingRates indicates if the exchange supports funding rates.
	SupportsFundingRates bool
	// LastChecked is the time of the last check.
	LastChecked time.Time
	// ExpiresAt is the expiration time of this capability info.
	ExpiresAt time.Time
}

// ExchangeCapabilityCache manages cached exchange capability information.
type ExchangeCapabilityCache struct {
	cache  map[string]*ExchangeCapabilityEntry // key: exchange name
	mu     sync.RWMutex
	ttl    time.Duration
	logger logging.Logger
}

// Worker represents a background worker for collecting data from a specific exchange.
type Worker struct {
	// Exchange is the exchange name.
	Exchange string
	// Symbols is the list of symbols being monitored.
	Symbols []string
	// Interval is the collection interval.
	Interval time.Duration
	// LastUpdate is the time of the last update.
	LastUpdate time.Time
	// IsRunning indicates if the worker is active.
	IsRunning bool
	// ErrorCount is the consecutive error count.
	ErrorCount int
	// MaxErrors is the maximum allowed consecutive errors.
	MaxErrors int
	// Paused indicates the worker was explicitly paused via PauseExchange.
	Paused bool
}

// BackfillJob represents a single symbol backfill task
type BackfillJob struct {
	ExchangeID string
	Symbol     string
	StartTime  time.Time
}

// BackfillResult represents the result of a backfill operation
type BackfillResult struct {
	ExchangeID string
	Symbol     string
	Success    bool
	Error      error
}

// priceCacheEntry stores last price for outlier detection
type priceCacheEntry struct {
	price     decimal.Decimal
	timestamp time.Time
}

// volumeStatsEntry stores volume statistics for anomaly detection
type volumeStatsEntry struct {
	avgVolume   decimal.Decimal
	sampleCount int
}
