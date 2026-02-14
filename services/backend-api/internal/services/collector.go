package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/redis/go-redis/v9"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/logging"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
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

// NewSymbolCache creates a new symbol cache with specified TTL.
//
// Parameters:
//
//	ttl: Time to live for cache entries.
//
// Returns:
//
//	*SymbolCache: Initialized cache.
func NewSymbolCache(ttl time.Duration, logger logging.Logger) *SymbolCache {
	return &SymbolCache{
		cache:  make(map[string]*SymbolCacheEntry),
		ttl:    ttl,
		logger: logger,
	}
}

// Get retrieves symbols from cache if not expired.
//
// Parameters:
//
//	exchangeID: Exchange identifier.
//
// Returns:
//
//	[]string: List of symbols.
//	bool: True if found and valid.
func (sc *SymbolCache) Get(exchangeID string) ([]string, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	entry, exists := sc.cache[exchangeID]
	if !exists {
		sc.stats.Misses++
		sc.logger.Debug("Cache MISS", "exchange", exchangeID)
		return nil, false
	}

	// Check if entry has expired
	if time.Now().After(entry.ExpiresAt) {
		// Entry expired, treat as cache miss
		sc.stats.Misses++
		sc.logger.Debug("Cache MISS (expired)", "exchange", exchangeID)
		return nil, false
	}

	sc.stats.Hits++
	sc.logger.Debug("Cache HIT", "exchange", exchangeID, "symbols", len(entry.Symbols))

	return entry.Symbols, true
}

// Set stores symbols in cache with TTL.
//
// Parameters:
//
//	exchangeID: Exchange identifier.
//	symbols: List of symbols to cache.
func (sc *SymbolCache) Set(exchangeID string, symbols []string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.stats.Sets++
	sc.cache[exchangeID] = &SymbolCacheEntry{
		Symbols:   symbols,
		ExpiresAt: time.Now().Add(sc.ttl),
	}
	sc.logger.Debug("Cache SET", "exchange", exchangeID, "symbols", len(symbols))
}

// GetStats returns current cache statistics.
//
// Returns:
//
//	cache.SymbolCacheStats: Cache statistics.
func (sc *SymbolCache) GetStats() cache.SymbolCacheStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return cache.SymbolCacheStats{
		Hits:   sc.stats.Hits,
		Misses: sc.stats.Misses,
		Sets:   sc.stats.Sets,
	}
}

// LogStats logs current cache performance statistics.
func (sc *SymbolCache) LogStats() {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	total := sc.stats.Hits + sc.stats.Misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(sc.stats.Hits) / float64(total) * 100
	}

	sc.logger.WithFields(map[string]interface{}{
		"hits":             sc.stats.Hits,
		"misses":           sc.stats.Misses,
		"sets":             sc.stats.Sets,
		"hit_rate_percent": hitRate,
	}).Info("Symbol Cache Stats")
}

// NewExchangeCapabilityCache creates a new exchange capability cache with specified TTL.
//
// Parameters:
//
//	ttl: Time to live.
//
// Returns:
//
//	*ExchangeCapabilityCache: Initialized cache.
func NewExchangeCapabilityCache(ttl time.Duration, logger logging.Logger) *ExchangeCapabilityCache {
	return &ExchangeCapabilityCache{
		cache:  make(map[string]*ExchangeCapabilityEntry),
		ttl:    ttl,
		logger: logger,
	}
}

// SupportsFundingRates checks if an exchange supports funding rates.
//
// Parameters:
//
//	exchange: Exchange name.
//
// Returns:
//
//	bool: True if supported.
//	bool: True if info is found in cache.
func (ecc *ExchangeCapabilityCache) SupportsFundingRates(exchange string) (bool, bool) {
	ecc.mu.RLock()
	defer ecc.mu.RUnlock()

	entry, exists := ecc.cache[exchange]
	if !exists {
		return false, false // unknown capability
	}

	// Check if entry has expired
	if time.Now().After(entry.ExpiresAt) {
		return false, false // expired, need to recheck
	}

	return entry.SupportsFundingRates, true // known capability
}

// SetFundingRateSupport sets the funding rate support capability for an exchange.
//
// Parameters:
//
//	exchange: Exchange name.
//	supports: Whether funding rates are supported.
func (ecc *ExchangeCapabilityCache) SetFundingRateSupport(exchange string, supports bool) {
	ecc.mu.Lock()
	defer ecc.mu.Unlock()

	ecc.cache[exchange] = &ExchangeCapabilityEntry{
		SupportsFundingRates: supports,
		LastChecked:          time.Now(),
		ExpiresAt:            time.Now().Add(ecc.ttl),
	}
	ecc.logger.WithFields(map[string]interface{}{
		"exchange":               exchange,
		"supports_funding_rates": supports,
	}).Debug("Exchange capability cached")
}

// isBlacklistableError checks if an error indicates a symbol should be blacklisted
func isBlacklistableError(err error) (bool, string) {
	if err == nil {
		return false, ""
	}

	errorMsg := err.Error()

	// Coinbase delisted products
	if strings.Contains(errorMsg, "Not allowed for delisted products") {
		return true, "coinbase_delisted"
	}

	// Binance missing market symbols
	if strings.Contains(errorMsg, "does not have market symbol") {
		return true, "binance_missing_symbol"
	}

	// General delisted indicators
	if strings.Contains(errorMsg, "delisted") {
		return true, "delisted"
	}

	// Inactive symbols
	if strings.Contains(errorMsg, "inactive") {
		return true, "inactive"
	}

	// Symbol not found or unavailable
	if strings.Contains(errorMsg, "symbol not found") ||
		strings.Contains(errorMsg, "symbol unavailable") ||
		strings.Contains(errorMsg, "market not found") {
		return true, "symbol_not_found"
	}

	// Exchange-specific error patterns
	if strings.Contains(errorMsg, "CCXT service error (500)") {
		// Check for specific 500 error patterns that indicate permanent issues
		if strings.Contains(errorMsg, "Not allowed") ||
			strings.Contains(errorMsg, "does not have") ||
			strings.Contains(errorMsg, "delisted") {
			return true, "ccxt_500_permanent"
		}
	}

	return false, ""
}

// isFundingRateUnsupportedError checks if an error indicates the exchange doesn't support funding rates
func isFundingRateUnsupportedError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := strings.ToLower(err.Error())

	// Check for funding rate unsupported error patterns
	return strings.Contains(errorMsg, "does not support funding rates") ||
		strings.Contains(errorMsg, "funding rates not supported") ||
		strings.Contains(errorMsg, "no funding rates") ||
		(strings.Contains(errorMsg, "ccxt service error (400)") && strings.Contains(errorMsg, "funding"))
}

// CollectorService handles market data collection from exchanges.
type CollectorService struct {
	db              DBPool
	ccxtService     ccxt.CCXTService
	config          *config.Config
	collectorConfig CollectorConfig
	backfillConfig  BackfillConfig
	workers         map[string]*Worker
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	// Caching and timing controls
	symbolCache             SymbolCacheInterface
	blacklistCache          cache.BlacklistCache
	exchangeCapabilityCache *ExchangeCapabilityCache
	redisClient             *redis.Client
	lastSymbolRefresh       map[string]time.Time
	lastFundingCollection   map[string]time.Time
	symbolRefreshMu         sync.RWMutex
	fundingCollectionMu     sync.RWMutex
	// Anti-manipulation filters
	lastPrice   sync.Map // map[string]priceCacheEntry
	volumeStats sync.Map // map[string]volumeStatsEntry
	// Separate intervals
	tickerInterval        time.Duration
	symbolRefreshInterval time.Duration
	fundingRateInterval   time.Duration
	// Readiness state
	isInitialized    bool
	isReady          bool
	hasCollectedData bool          // Tracks if first data has been collected
	dataReadyChan    chan struct{} // Signals when first data collection completes
	readinessMu      sync.RWMutex
	// Error recovery components
	circuitBreakerManager *CircuitBreakerManager
	errorRecoveryManager  *ErrorRecoveryManager
	timeoutManager        *TimeoutManager
	resourceManager       *ResourceManager
	performanceMonitor    *PerformanceMonitor
	// Resource optimization
	resourceOptimizer *ResourceOptimizer
	// Logging
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
}

// initializeSymbolCache creates either Redis-based or in-memory symbol cache
func initializeSymbolCache(redisClient *redis.Client, logger logging.Logger) SymbolCacheInterface {
	if redisClient != nil {
		logger.Info("Initializing Redis-based symbol cache")
		return cache.NewRedisSymbolCache(redisClient, 1*time.Hour)
	}
	logger.Info("Redis client not available, using in-memory symbol cache")
	return NewSymbolCache(1*time.Hour, logger)
}

// getExchangeCCXTCircuitBreaker returns a per-exchange circuit breaker for CCXT operations.
// This prevents failures on one exchange from affecting all other exchanges.
func (c *CollectorService) getExchangeCCXTCircuitBreaker(exchange string) *CircuitBreaker {
	name := fmt.Sprintf("ccxt:%s", exchange)
	config := CircuitBreakerConfig{
		FailureThreshold: 20,               // Allow more failures per exchange before opening
		SuccessThreshold: 3,                // Require 3 successes to close from half-open
		Timeout:          30 * time.Second, // Wait 30s before trying half-open
		MaxRequests:      10,               // Allow 10 requests in half-open state
		ResetTimeout:     60 * time.Second, // Reset failure count after 60s of no failures
	}
	return c.circuitBreakerManager.GetOrCreate(name, config)
}

// NewCollectorService creates a new market data collector service.
//
// Parameters:
//
//	db: Database connection.
//	ccxtService: CCXT service.
//	cfg: Configuration.
//	redisClient: Redis client.
//	blacklistCache: Blacklist cache.
//
// Returns:
//
//	*CollectorService: Initialized service.
func NewCollectorService(db DBPool, ccxtService ccxt.CCXTService, cfg *config.Config, redisClient *redis.Client, blacklistCache cache.BlacklistCache) *CollectorService {
	ctx, cancel := context.WithCancel(context.Background())

	// Parse collection interval from config
	intervalSeconds := 300 // Default 5 minutes
	if cfg.MarketData.CollectionInterval != "" {
		if duration, err := time.ParseDuration(cfg.MarketData.CollectionInterval); err == nil {
			intervalSeconds = int(duration.Seconds())
		}
	}

	collectorConfig := CollectorConfig{
		IntervalSeconds: intervalSeconds,
		MaxErrors:       5, // Default 5 max errors
	}

	// Initialize backfill configuration from config
	backfillConfig := BackfillConfig{
		Enabled:               cfg.Backfill.Enabled,
		BackfillHours:         cfg.Backfill.BackfillHours,
		MinDataThresholdHours: cfg.Backfill.MinDataThresholdHours,
		BatchSize:             cfg.Backfill.BatchSize,
		DelayBetweenBatches:   cfg.Backfill.DelayBetweenBatches,
	}

	// Initialize separate intervals for different operations
	tickerInterval := time.Duration(intervalSeconds) * time.Second // 5 minutes (from config)
	symbolRefreshInterval := 1 * time.Hour                         // 1 hour for symbol refresh
	fundingRateInterval := 15 * time.Minute                        // 15 minutes for funding rates

	// Initialize logger with config-provided log level
	logLevel := cfg.Telemetry.LogLevel
	if logLevel == "" {
		logLevel = cfg.LogLevel
	}
	if logLevel == "" {
		logLevel = "info" // fallback default
	}
	logger := logging.NewStandardLogger(logLevel, cfg.Environment)

	// Initialize error recovery components
	logrusLogger := zaplogrus.New()
	circuitBreakerManager := NewCircuitBreakerManager(logrusLogger)
	errorRecoveryManager := NewErrorRecoveryManager(logrusLogger)
	timeoutConfig := &TimeoutConfig{
		APICall:        10 * time.Second,
		DatabaseQuery:  5 * time.Second,
		RedisOperation: 2 * time.Second,
		ConcurrentOp:   15 * time.Second,
		HealthCheck:    3 * time.Second,
		Backfill:       60 * time.Second,
		SymbolFetch:    20 * time.Second,
		MarketData:     8 * time.Second,
	}
	timeoutManager := NewTimeoutManager(timeoutConfig, logrusLogger)
	resourceManager := NewResourceManager(logrusLogger)
	// Note: PerformanceMonitor expects go-redis/redis/v8 client, but we have redis/go-redis/v9
	// For now, we'll pass nil and handle Redis operations separately
	performanceMonitor := NewPerformanceMonitor(logrusLogger, nil, ctx)

	// Initialize resource optimizer
	resourceOptimizerConfig := ResourceOptimizerConfig{
		OptimizationInterval: 5 * time.Minute,
		AdaptiveMode:         true,
		MaxHistorySize:       100,
		CPUThreshold:         80.0,
		MemoryThreshold:      85.0,
		MinWorkers:           2,
		MaxWorkers:           20,
	}
	resourceOptimizer := NewResourceOptimizer(resourceOptimizerConfig)

	// Get optimal concurrency settings
	optimalConcurrency := resourceOptimizer.GetOptimalConcurrency()

	// Configure circuit breakers with appropriate thresholds
	// Note: CCXT uses per-exchange circuit breakers (see getExchangeCCXTCircuitBreaker),
	// but we keep a global fallback with higher thresholds for safety
	ccxtConfig := CircuitBreakerConfig{
		FailureThreshold: 50, // High threshold - per-exchange breakers handle individual exchanges
		SuccessThreshold: 3,
		Timeout:          30 * time.Second,
		MaxRequests:      optimalConcurrency.MaxCircuitBreakerCalls,
		ResetTimeout:     60 * time.Second,
	}
	redisConfig := CircuitBreakerConfig{
		FailureThreshold: 10, // Redis should have higher tolerance
		SuccessThreshold: 3,
		Timeout:          15 * time.Second,
		MaxRequests:      optimalConcurrency.MaxCircuitBreakerCalls / 2,
		ResetTimeout:     30 * time.Second,
	}

	circuitBreakerManager.GetOrCreate("ccxt", ccxtConfig)
	circuitBreakerManager.GetOrCreate("redis", redisConfig)

	return &CollectorService{
		db:              db,
		ccxtService:     ccxtService,
		config:          cfg,
		collectorConfig: collectorConfig,
		backfillConfig:  backfillConfig,
		workers:         make(map[string]*Worker),
		ctx:             ctx,
		cancel:          cancel,
		// Initialize caching and timing controls
		symbolCache:             initializeSymbolCache(redisClient, logger), // Redis or in-memory cache
		blacklistCache:          blacklistCache,                             // Use the provided blacklist cache with database persistence
		exchangeCapabilityCache: NewExchangeCapabilityCache(24*time.Hour, logger),
		redisClient:             redisClient,
		lastSymbolRefresh:       make(map[string]time.Time),
		lastFundingCollection:   make(map[string]time.Time),
		// Set separate intervals
		tickerInterval:        tickerInterval,
		symbolRefreshInterval: symbolRefreshInterval,
		fundingRateInterval:   fundingRateInterval,
		// Initialize data readiness signaling
		dataReadyChan: make(chan struct{}),
		// Initialize error recovery components
		circuitBreakerManager: circuitBreakerManager,
		errorRecoveryManager:  errorRecoveryManager,
		timeoutManager:        timeoutManager,
		resourceManager:       resourceManager,
		performanceMonitor:    performanceMonitor,
		// Initialize resource optimization
		resourceOptimizer: resourceOptimizer,
		// Initialize logging
		logger: logger,
	}
}

// Start initializes and starts all collection workers asynchronously.
//
// Returns:
//
//	error: Error if initialization fails.
func (c *CollectorService) Start() error {
	ctx, span := observability.StartSpan(c.ctx, observability.SpanOpMarketData, "CollectorService.Start")
	defer observability.FinishSpan(span, nil)

	c.logger.Info("Starting market data collector service...")
	observability.AddBreadcrumb(ctx, "collector", "Starting market data collector service", sentry.LevelInfo)

	// Initialize CCXT service
	if err := c.ccxtService.Initialize(ctx); err != nil {
		observability.CaptureExceptionWithContext(ctx, err, "ccxt_initialization", map[string]interface{}{
			"service": "collector",
		})
		return fmt.Errorf("failed to initialize CCXT service: %w", err)
	}

	// Mark as initialized
	c.readinessMu.Lock()
	c.isInitialized = true
	c.readinessMu.Unlock()

	// Start symbol collection and worker creation asynchronously
	go c.initializeWorkersAsync()

	c.logger.Info("Market data collector service started", "workers_initializing", true)
	observability.AddBreadcrumb(ctx, "collector", "Market data collector service started", sentry.LevelInfo)
	return nil
}

// getPrioritizedExchanges returns exchanges ordered by priority field from database
func (c *CollectorService) getPrioritizedExchanges() []string {
	// Get all supported exchanges from CCXT
	allExchanges := c.ccxtService.GetSupportedExchanges()

	// If database is not available, return all exchanges
	if isNilDBPool(c.db) {
		c.logger.Warn("Database not available, returning all exchanges")
		return allExchanges
	}

	// Query database to get exchanges with their priorities
	query := `
		SELECT e.name, e.priority, e.is_active, ce.ccxt_id 
		FROM exchanges e 
		LEFT JOIN ccxt_exchanges ce ON e.id = ce.exchange_id 
		WHERE e.name = ANY($1) AND e.is_active = true 
		ORDER BY e.priority ASC, e.name ASC`

	rows, err := c.db.Query(c.ctx, query, allExchanges)
	if err != nil {
		c.logger.WithError(err).Error("Failed to query prioritized exchanges")
		return allExchanges // Fallback to all exchanges
	}
	defer rows.Close()

	var prioritizedExchanges []string
	for rows.Next() {
		var name string
		var priority int
		var isActive bool
		var ccxtID *string

		if err := rows.Scan(&name, &priority, &isActive, &ccxtID); err != nil {
			c.logger.WithError(err).Error("Failed to scan exchange row")
			continue
		}

		// Use CCXT ID if available, otherwise use name
		exchangeID := name
		if ccxtID != nil {
			exchangeID = *ccxtID
		}

		prioritizedExchanges = append(prioritizedExchanges, exchangeID)
		c.logger.WithFields(map[string]interface{}{
			"exchange": exchangeID,
			"priority": priority,
		}).Debug("Added prioritized exchange")
	}

	if len(prioritizedExchanges) == 0 {
		c.logger.Warn("No prioritized exchanges found, using all exchanges")
		return allExchanges
	}

	c.logger.WithFields(map[string]interface{}{
		"total":          len(prioritizedExchanges),
		"priority_count": len(prioritizedExchanges),
	}).Info("Using prioritized exchanges")
	return prioritizedExchanges
}

// initializeWorkersAsync handles symbol collection and worker creation in the background
func (c *CollectorService) initializeWorkersAsync() {
	ctx, span := observability.StartSpan(c.ctx, observability.SpanOpMarketData, "CollectorService.initializeWorkersAsync")
	defer func() {
		observability.RecoverAndCapture(ctx, "initializeWorkersAsync")
		observability.FinishSpan(span, nil)
	}()

	c.logger.Info("Starting background symbol collection and worker initialization...")
	observability.AddBreadcrumb(ctx, "collector", "Starting background initialization", sentry.LevelInfo)

	// Get supported exchanges prioritized by database priority field
	exchanges := c.getPrioritizedExchanges()
	span.SetData("exchange_count", len(exchanges))

	// Get symbols that appear on multiple exchanges for arbitrage
	multiExchangeSymbols, err := c.getMultiExchangeSymbols(exchanges)
	if err != nil {
		c.logger.WithError(err).Warn("Failed to get multi-exchange symbols")
		observability.AddBreadcrumb(ctx, "collector", "Failed to get multi-exchange symbols", sentry.LevelWarning)
		// Continue with individual exchange symbols as fallback
	}

	// Create workers for each exchange
	workersCreated := 0
	for _, exchangeID := range exchanges {
		if err := c.createWorker(exchangeID, multiExchangeSymbols); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"exchange": exchangeID,
			}).WithError(err).Warn("Failed to create worker for exchange")
			observability.AddBreadcrumbWithData(ctx, "collector", "Failed to create worker", sentry.LevelWarning, map[string]interface{}{
				"exchange": exchangeID,
				"error":    err.Error(),
			})
			continue
		}
		workersCreated++
	}

	// Mark as ready
	c.readinessMu.Lock()
	c.isReady = true
	c.readinessMu.Unlock()

	span.SetData("workers_created", workersCreated)
	c.logger.WithFields(map[string]interface{}{
		"workers_started": len(c.workers),
	}).Info("Background initialization complete")
	observability.AddBreadcrumbWithData(ctx, "collector", "Background initialization complete", sentry.LevelInfo, map[string]interface{}{
		"workers_started": len(c.workers),
	})
}

// Stop gracefully stops all collection workers.
func (c *CollectorService) Stop() {
	c.logger.Info("Stopping market data collector service...")
	c.cancel()
	c.wg.Wait()
	c.logger.Info("Market data collector service stopped")
}

// IsInitialized returns true if the collector service has been initialized.
//
// Returns:
//
//	bool: True if initialized.
func (c *CollectorService) IsInitialized() bool {
	c.readinessMu.RLock()
	defer c.readinessMu.RUnlock()
	return c.isInitialized
}

// IsReady returns true if the collector service is fully ready (workers created and running).
//
// Returns:
//
//	bool: True if ready.
func (c *CollectorService) IsReady() bool {
	c.readinessMu.RLock()
	defer c.readinessMu.RUnlock()
	return c.isReady
}

// GetReadinessStatus returns the current readiness status.
//
// Returns:
//
//	initialized: True if initialized.
//	ready: True if ready.
func (c *CollectorService) GetReadinessStatus() (initialized bool, ready bool) {
	c.readinessMu.RLock()
	defer c.readinessMu.RUnlock()
	return c.isInitialized, c.isReady
}

// WaitForFirstData blocks until the first market data is collected or timeout expires.
// This should be called after Start() to ensure data is available before dependent services begin.
//
// Parameters:
//
//	timeout: Maximum time to wait for first data collection.
//
// Returns:
//
//	error: nil if data was collected, or an error if timeout/context cancelled.
func (c *CollectorService) WaitForFirstData(timeout time.Duration) error {
	c.logger.WithFields(map[string]interface{}{"timeout": timeout}).Info("Waiting for first market data collection...")

	select {
	case <-c.dataReadyChan:
		c.logger.Info("First market data collected successfully")
		return nil
	case <-time.After(timeout):
		c.logger.WithFields(map[string]interface{}{"timeout": timeout}).Warn("Timeout waiting for first market data collection")
		return fmt.Errorf("timeout waiting for first data collection after %v", timeout)
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

// HasCollectedData returns true if any market data has been collected.
func (c *CollectorService) HasCollectedData() bool {
	c.readinessMu.RLock()
	defer c.readinessMu.RUnlock()
	return c.hasCollectedData
}

// VerifyDatabaseSeeding checks that the database has the required seed data for
// market data collection and arbitrage calculations to work correctly.
// This includes active exchanges and active trading pairs.
//
// Returns:
//
//	error: nil if database is properly seeded, or descriptive error otherwise.
func (c *CollectorService) VerifyDatabaseSeeding() error {
	var exchangeCount, tradingPairCount int

	// Check exchanges with status='active'
	err := c.db.QueryRow(c.ctx,
		"SELECT COUNT(*) FROM exchanges WHERE status = 'active'").Scan(&exchangeCount)
	if err != nil {
		c.logger.WithError(err).Error("Failed to query active exchanges")
		return fmt.Errorf("failed to query active exchanges: %w", err)
	}

	if exchangeCount == 0 {
		c.logger.WithFields(map[string]interface{}{"count": exchangeCount}).Error("No active exchanges in database - arbitrage will not work")
		return fmt.Errorf("database not seeded: 0 active exchanges (need at least 1)")
	}

	// Check trading_pairs with is_active=true
	err = c.db.QueryRow(c.ctx,
		"SELECT COUNT(*) FROM trading_pairs WHERE is_active = true").Scan(&tradingPairCount)
	if err != nil {
		c.logger.WithError(err).Error("Failed to query active trading pairs")
		return fmt.Errorf("failed to query active trading pairs: %w", err)
	}

	// Trading pairs may be 0 on first startup (they get created during collection)
	// This is a warning, not an error
	if tradingPairCount == 0 {
		c.logger.WithFields(map[string]interface{}{"count": tradingPairCount}).Warn("No active trading pairs in database yet - will be created during collection")
	}

	c.logger.WithFields(map[string]interface{}{
		"active_exchanges":     exchangeCount,
		"active_trading_pairs": tradingPairCount,
	}).Info("Database seeding verified")
	return nil
}

// createWorker creates and starts a worker for a specific exchange
func (c *CollectorService) createWorker(exchangeID string, multiExchangeSymbols map[string]int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to get symbols from cache first (should be populated by getMultiExchangeSymbols)
	var activeSymbols []string

	if cached, found := c.symbolCache.Get(exchangeID); found {
		activeSymbols = cached
		c.logger.WithFields(map[string]interface{}{
			"exchange":     exchangeID,
			"symbol_count": len(activeSymbols),
		}).Info("Using cached symbols for exchange")
	} else {
		// Cache should always be populated during startup, log error if not found
		c.logger.WithFields(map[string]interface{}{"exchange": exchangeID}).Error("No cached symbols found during worker creation")
		return fmt.Errorf("no cached symbols found for exchange %s - cache should be populated during startup", exchangeID)
	}

	// Filter out invalid symbols (options, derivatives, etc.)
	validSymbols := c.filterValidSymbols(activeSymbols)

	// Further filter to only include symbols that appear on multiple exchanges (for arbitrage)
	arbitrageSymbols := c.filterArbitrageSymbols(validSymbols, multiExchangeSymbols)
	c.logger.WithFields(map[string]interface{}{
		"exchange":          exchangeID,
		"arbitrage_symbols": len(arbitrageSymbols),
		"valid_symbols":     len(validSymbols),
	}).Info("Filtered arbitrage symbols")

	// Use arbitrage symbols if available, otherwise fall back to valid active symbols
	finalSymbols := arbitrageSymbols
	if len(finalSymbols) == 0 {
		c.logger.WithFields(map[string]interface{}{"exchange": exchangeID}).Info("No arbitrage symbols found, using all valid active symbols")
		finalSymbols = validSymbols
	}

	// Get exchange ID for database operations
	exchangeDBID, err := c.getOrCreateExchange(exchangeID)
	if err != nil {
		return fmt.Errorf("failed to get or create exchange %s: %w", exchangeID, err)
	}

	// Ensure all trading pairs exist in database
	for _, symbol := range finalSymbols {
		if err := c.ensureTradingPairExists(exchangeDBID, symbol); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"symbol": symbol,
			}).WithError(err).Warn("Failed to ensure trading pair exists")
		}
	}

	if len(finalSymbols) == 0 {
		c.logger.WithFields(map[string]interface{}{"exchange": exchangeID}).Info("No valid active trading pairs found, skipping worker creation")
		return nil
	}

	// Create worker with filtered active symbols
	worker := &Worker{
		Exchange:  exchangeID,
		Symbols:   finalSymbols,
		Interval:  time.Duration(c.collectorConfig.IntervalSeconds) * time.Second,
		MaxErrors: c.collectorConfig.MaxErrors,
		IsRunning: true,
	}

	c.workers[exchangeID] = worker

	// Start worker goroutine
	c.wg.Add(1)
	c.wg.Add(1)
	go c.runWorker(worker)

	c.logger.WithFields(map[string]interface{}{
		"exchange":     exchangeID,
		"symbol_count": len(finalSymbols),
	}).Info("Created worker")
	return nil
}

// runWorker runs the collection loop for a specific worker with separate intervals
func (c *CollectorService) runWorker(worker *Worker) {
	defer c.wg.Done()

	// Register worker with resource manager for cleanup
	senderID := fmt.Sprintf("worker_%s_%d", worker.Exchange, time.Now().UnixNano())
	c.resourceManager.RegisterResource(senderID, GoroutineResource, func() error {
		c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Cleaning up worker")
		worker.IsRunning = false
		return nil
	}, map[string]interface{}{
		"exchange":    worker.Exchange,
		"worker_type": "ticker_collector",
	})
	defer func() {
		if err := c.resourceManager.CleanupResource(senderID); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"worker_id": senderID,
			}).WithError(err).Error("Failed to cleanup resource")
		}
	}()

	// Use ticker interval for main ticker data collection
	ticker := time.NewTicker(c.tickerInterval)
	defer ticker.Stop()

	// Add cache statistics logging every 10 minutes
	cacheStatsTicker := time.NewTicker(10 * time.Minute)
	defer cacheStatsTicker.Stop()

	// Add health check ticker for monitoring worker status
	healthCheckTicker := time.NewTicker(5 * time.Minute)
	defer healthCheckTicker.Stop()

	// Add resource optimization ticker for adaptive scaling
	resourceOptimizationTicker := time.NewTicker(2 * time.Minute)
	defer resourceOptimizationTicker.Stop()

	c.logger.WithFields(map[string]interface{}{
		"exchange":         worker.Exchange,
		"symbols":          len(worker.Symbols),
		"ticker_interval":  c.tickerInterval,
		"funding_interval": c.fundingRateInterval,
	}).Info("Worker started")

	// Track consecutive failures for graceful degradation
	consecutiveFailures := 0
	maxConsecutiveFailures := 3
	intervalIncreased := false          // Track if interval was doubled due to failures
	degradationStartTime := time.Time{} // Track when degradation started

	for {
		select {
		case <-c.ctx.Done():
			c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Worker stopping due to context cancellation")
			return
		case <-cacheStatsTicker.C:
			// Log cache statistics periodically
			c.symbolCache.LogStats()
			c.blacklistCache.LogStats()
		case <-healthCheckTicker.C:
			// Perform health check and report status
			c.performanceMonitor.RecordWorkerHealth(worker.Exchange, worker.IsRunning, worker.ErrorCount)
			c.logger.WithFields(map[string]interface{}{
				"exchange":    worker.Exchange,
				"running":     worker.IsRunning,
				"errors":      worker.ErrorCount,
				"last_update": worker.LastUpdate,
			}).Info("Health check")
		case <-resourceOptimizationTicker.C:
			// Update system metrics and trigger adaptive optimization
			if err := c.resourceOptimizer.UpdateSystemMetrics(c.ctx); err != nil {
				c.logger.WithError(err).Error("Failed to update system metrics")
			}
			// Check if optimization is needed
			if c.resourceOptimizer.OptimizeIfNeeded(ResourceOptimizerConfig{
				OptimizationInterval: 5 * time.Minute,
				AdaptiveMode:         true,
				MaxHistorySize:       100,
				CPUThreshold:         80.0,
				MemoryThreshold:      85.0,
				MinWorkers:           2,
				MaxWorkers:           20,
			}) {
				c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Resource optimization applied")
			}
			c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Resource optimization triggered")
		case <-ticker.C:
			c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Worker tick - starting collection cycle")

			// Create operation context with timeout
			operationID := fmt.Sprintf("worker_collection_%s_%d", worker.Exchange, time.Now().UnixNano())
			operationCtx := c.timeoutManager.CreateOperationContext("worker_collection", operationID)
			cancel := operationCtx.Cancel
			ctx := operationCtx.Ctx

			// Collect market data for active trading pairs with error recovery
			err := c.errorRecoveryManager.ExecuteWithRetry(ctx, "worker_collection", func() error {
				return c.collectTickerDataOnly(worker)
			})

			cancel() // Clean up operation context

			if err != nil {
				worker.ErrorCount++
				consecutiveFailures++
				c.logger.WithFields(map[string]interface{}{
					"exchange":             worker.Exchange,
					"error_count":          worker.ErrorCount,
					"consecutive_failures": consecutiveFailures,
				}).WithError(err).Error("Error collecting ticker data")

				// Implement graceful degradation for consecutive failures
				if consecutiveFailures >= maxConsecutiveFailures {
					c.logger.WithFields(map[string]interface{}{
						"exchange":             worker.Exchange,
						"consecutive_failures": consecutiveFailures,
					}).Warn("Worker has consecutive failures, implementing graceful degradation")

					// Increase interval temporarily to reduce load
					ticker.Stop()
					ticker = time.NewTicker(c.tickerInterval * 2) // Double the interval
					intervalIncreased = true
					degradationStartTime = time.Now()
					c.logger.WithFields(map[string]interface{}{
						"exchange":     worker.Exchange,
						"new_interval": c.tickerInterval * 2,
					}).Info("Temporarily increased collection interval")
				}

				if worker.ErrorCount >= worker.MaxErrors {
					c.logger.WithFields(map[string]interface{}{
						"exchange":   worker.Exchange,
						"max_errors": worker.MaxErrors,
					}).Error("Worker exceeded max errors, stopping")
					worker.IsRunning = false
					return
				}
			} else {
				// Reset error counts on successful collection
				worker.ErrorCount = 0
				consecutiveFailures = 0
				worker.LastUpdate = time.Now()

				// Record performance snapshot for resource optimization
				c.resourceOptimizer.RecordPerformanceSnapshot(
					1,                            // activeOps
					float64(len(worker.Symbols)), // throughput
					float64(worker.ErrorCount)/float64(worker.ErrorCount+1), // errorRate
					float64(time.Since(worker.LastUpdate).Milliseconds()),   // responseTime
				)

				// Restore normal interval if it was increased due to failures
				if intervalIncreased {
					ticker.Stop()
					ticker = time.NewTicker(c.tickerInterval)
					intervalIncreased = false
					degradationStartTime = time.Time{}
					c.logger.WithFields(map[string]interface{}{
						"exchange": worker.Exchange,
						"interval": c.tickerInterval,
					}).Info("Restored normal collection interval")
				}
			}

			// Check if we've been degraded for too long (> 5 minutes) and force restore
			if intervalIncreased && !degradationStartTime.IsZero() && time.Since(degradationStartTime) > 5*time.Minute {
				ticker.Stop()
				ticker = time.NewTicker(c.tickerInterval)
				intervalIncreased = false
				degradationStartTime = time.Time{}
				c.logger.WithFields(map[string]interface{}{
					"exchange":          worker.Exchange,
					"degraded_duration": time.Since(degradationStartTime),
				}).Warn("Degradation timeout reached, forcing interval restoration")
			}

			// Check if it's time to collect funding rates (separate interval)
			c.fundingCollectionMu.RLock()
			lastFundingCollection, exists := c.lastFundingCollection[worker.Exchange]
			c.fundingCollectionMu.RUnlock()

			if !exists || time.Since(lastFundingCollection) >= c.fundingRateInterval {
				c.logger.WithFields(map[string]interface{}{
					"exchange": worker.Exchange,
					"interval": c.fundingRateInterval,
				}).Info("Collecting funding rates")

				// Create separate context for funding rate collection
				fundingOperationID := fmt.Sprintf("funding_collection_%s_%d", worker.Exchange, time.Now().UnixNano())
				fundingCtx := c.timeoutManager.CreateOperationContext("funding_collection", fundingOperationID)
				fundingCancel := fundingCtx.Cancel
				fundingContext := fundingCtx.Ctx

				err := c.errorRecoveryManager.ExecuteWithRetry(fundingContext, "funding_collection", func() error {
					return c.collectFundingRates(worker)
				})

				fundingCancel() // Clean up funding context

				if err != nil {
					c.logger.WithFields(map[string]interface{}{
						"exchange": worker.Exchange,
					}).WithError(err).Warn("Failed to collect funding rates")
				} else {
					// Update last funding collection time
					c.fundingCollectionMu.Lock()
					c.lastFundingCollection[worker.Exchange] = time.Now()
					c.fundingCollectionMu.Unlock()
				}
			}
		}
	}
}

// collectTickerDataOnly collects only ticker data for worker symbols (no funding rates)
func (c *CollectorService) collectTickerDataOnly(worker *Worker) error {
	// Track performance metrics
	startTime := time.Now()
	var collectionMethod string
	defer func() {
		duration := time.Since(startTime)
		c.logger.WithFields(map[string]interface{}{
			"exchange":     worker.Exchange,
			"method":       collectionMethod,
			"duration_ms":  duration.Milliseconds(),
			"symbol_count": len(worker.Symbols),
		}).Info("Ticker collection completed")

		// Cache performance metrics in Redis for monitoring
		if c.redisClient != nil {
			metricsKey := fmt.Sprintf("metrics:collection:%s", worker.Exchange)
			metrics := map[string]interface{}{
				"duration_ms":       duration.Milliseconds(),
				"symbol_count":      len(worker.Symbols),
				"method":            collectionMethod,
				"timestamp":         time.Now().Unix(),
				"performance_ratio": float64(len(worker.Symbols)) / float64(duration.Milliseconds()), // symbols per ms
			}
			if metricsJSON, err := json.Marshal(metrics); err == nil {
				c.redisClient.Set(c.ctx, metricsKey, string(metricsJSON), 5*time.Minute)
			}
		}
	}()

	// Try bulk collection first, fallback to sequential if it fails
	collectionMethod = "bulk"
	if err := c.collectTickerDataBulk(worker); err != nil {
		c.logger.WithFields(map[string]interface{}{
			"exchange": worker.Exchange,
		}).WithError(err).Warn("Bulk ticker collection failed, falling back to sequential")
		collectionMethod = "sequential"
		return c.collectTickerDataSequential(worker)
	}
	return nil
}

// collectTickerDataBulk collects ticker data using bulk FetchMarketData for optimal performance
func (c *CollectorService) collectTickerDataBulk(worker *Worker) error {
	spanCtx, span := observability.StartSpanWithTags(c.ctx, observability.SpanOpMarketData, "CollectorService.collectTickerDataBulk", map[string]string{
		"exchange":     worker.Exchange,
		"symbol_count": fmt.Sprintf("%d", len(worker.Symbols)),
	})
	defer observability.FinishSpan(span, nil)

	defer observability.FinishSpan(span, nil)

	c.logger.WithFields(map[string]interface{}{
		"exchange": worker.Exchange,
		"symbols":  len(worker.Symbols),
	}).Info("Collecting ticker data (bulk)")

	// Filter out blacklisted symbols before making the bulk request
	validSymbols := make([]string, 0, len(worker.Symbols))
	for _, symbol := range worker.Symbols {
		symbolKey := fmt.Sprintf("%s:%s", worker.Exchange, symbol)
		if isBlacklisted, reason := c.blacklistCache.IsBlacklisted(symbolKey); !isBlacklisted {
			validSymbols = append(validSymbols, symbol)
		} else {
			c.logger.WithFields(map[string]interface{}{
				"symbol": symbolKey,
				"reason": reason,
			}).Info("Skipping blacklisted symbol")
		}
	}

	span.SetData("valid_symbols", len(validSymbols))

	if len(validSymbols) == 0 {
		c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("No valid symbols to fetch")
		return nil
	}

	// Create timeout context for the operation
	operationID := fmt.Sprintf("ccxt_bulk_fetch_%s_%d", worker.Exchange, time.Now().UnixNano())
	operationCtx := c.timeoutManager.CreateOperationContext("ccxt_bulk_fetch", operationID)
	cancel := operationCtx.Cancel
	ctx := operationCtx.Ctx
	defer cancel()

	// Use the span context for better tracing
	_ = spanCtx

	// Register the operation with resource manager
	resourceID := fmt.Sprintf("bulk_fetch_%s_%d", worker.Exchange, time.Now().UnixNano())
	c.resourceManager.RegisterResource(resourceID, GoroutineResource, func() error {
		cancel()
		return nil
	}, map[string]interface{}{"exchange": worker.Exchange, "operation": "bulk_fetch"})
	defer func() {
		if err := c.resourceManager.CleanupResource(resourceID); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"resource": resourceID,
			}).WithError(err).Error("Failed to cleanup resource")
		}
	}()

	// Use per-exchange circuit breaker for CCXT service call with retry logic
	// This prevents failures on one exchange from blocking all other exchanges
	var marketData []models.MarketPrice
	err := c.getExchangeCCXTCircuitBreaker(worker.Exchange).Execute(ctx, func(ctx context.Context) error {
		return c.errorRecoveryManager.ExecuteWithRetry(ctx, "ccxt_bulk_fetch", func() error {
			// Fetch bulk market data for a single exchange across symbols
			var fetchErr error
			var resp []ccxt.MarketPriceInterface
			resp, fetchErr = c.ccxtService.FetchMarketData(ctx, []string{worker.Exchange}, validSymbols)
			if fetchErr != nil {
				return fetchErr
			}
			// Convert interface slice to models for downstream processing
			marketData = c.convertMarketPriceInterfacesToModels(resp)
			return nil
		})
	})

	if err != nil {
		return fmt.Errorf("failed to fetch bulk ticker data with circuit breaker: %w", err)
	}

	// Channels to track async save results
	successChan := make(chan bool, len(marketData))
	errorChan := make(chan error, len(marketData))
	successCount := 0

	// Use goroutines for concurrent processing of ticker data
	for _, ticker := range marketData {
		go func(t models.MarketPrice) {
			select {
			case <-c.ctx.Done():
				errorChan <- c.ctx.Err()
				return
			default:
			}

			// Validate and save ticker data
			if err := c.saveBulkTickerData(t); err != nil {
				c.logger.WithFields(map[string]interface{}{
					"exchange": t.ExchangeName,
					"symbol":   t.Symbol,
				}).WithError(err).Error("Failed to save ticker data")
				errorChan <- err
			} else {
				successChan <- true
			}
		}(ticker)
	}

	// Wait for all goroutines to complete
	for i := 0; i < len(marketData); i++ {
		select {
		case <-successChan:
			successCount++
		case <-errorChan:
			// Error already logged, continue processing
		case <-c.ctx.Done():
			return c.ctx.Err()
		}
	}

	// Cache bulk results for fast API responses (best-effort)
	c.cacheBulkTickerData(worker.Exchange, marketData)

	c.logger.WithFields(map[string]interface{}{
		"exchange":      worker.Exchange,
		"success_count": successCount,
		"total_count":   len(marketData),
	}).Info("Successfully saved tickers")
	return nil
}

// collectTickerDataSequential collects ticker data sequentially (fallback method)
func (c *CollectorService) collectTickerDataSequential(worker *Worker) error {
	// Filter out blacklisted symbols before sequential processing
	validSymbols := make([]string, 0, len(worker.Symbols))
	for _, symbol := range worker.Symbols {
		symbolKey := fmt.Sprintf("%s:%s", worker.Exchange, symbol)
		if isBlacklisted, reason := c.blacklistCache.IsBlacklisted(symbolKey); !isBlacklisted {
			validSymbols = append(validSymbols, symbol)
		} else {
			c.logger.WithFields(map[string]interface{}{
				"symbol": symbolKey,
				"reason": reason,
			}).Info("Skipping blacklisted symbol")
		}
	}

	if len(validSymbols) == 0 {
		c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("No valid symbols to fetch sequentially")
		return nil
	}

	c.logger.WithFields(map[string]interface{}{
		"exchange": worker.Exchange,
		"symbols":  len(validSymbols),
	}).Info("Collecting ticker data (sequential)")

	// Create timeout context for the entire sequential operation
	operationID := fmt.Sprintf("sequential_collection_%s_%d", worker.Exchange, time.Now().UnixNano())
	operationCtx := c.timeoutManager.CreateOperationContext("sequential_collection", operationID)
	cancel := operationCtx.Cancel
	ctx := operationCtx.Ctx
	defer cancel()

	// Register the operation with resource manager
	resourceID := fmt.Sprintf("sequential_%s_%d", worker.Exchange, time.Now().UnixNano())
	c.resourceManager.RegisterResource(resourceID, GoroutineResource, func() error {
		cancel()
		return nil
	}, map[string]interface{}{"exchange": worker.Exchange, "operation": "sequential_collection"})
	defer func() {
		if err := c.resourceManager.CleanupResource(resourceID); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"resource": resourceID,
			}).WithError(err).Error("Failed to cleanup resource")
		}
	}()

	// Collect ticker data for all valid symbols with rate limiting and error recovery
	successCount := 0
	for i, symbol := range validSymbols {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
		}

		// Use per-exchange circuit breaker for direct collection with retry logic
		err := c.getExchangeCCXTCircuitBreaker(worker.Exchange).Execute(ctx, func(ctx context.Context) error {
			return c.errorRecoveryManager.ExecuteWithRetry(ctx, "sequential_ticker_fetch", func() error {
				return c.collectTickerDataDirect(worker.Exchange, symbol)
			})
		})

		if err != nil {
			c.logger.WithFields(map[string]interface{}{
				"exchange": worker.Exchange,
				"symbol":   symbol,
			}).WithError(err).Error("Failed to collect ticker data with error recovery")
			// Continue with other symbols even if one fails
			continue
		} else {
			successCount++
		}

		// Add rate limiting delay between requests (aggressive mode: 30ms)
		if i < len(validSymbols)-1 {
			time.Sleep(30 * time.Millisecond)
		}
	}

	c.logger.WithFields(map[string]interface{}{
		"exchange":   worker.Exchange,
		"successful": successCount,
		"total":      len(validSymbols),
	}).Info("Sequential collection completed")
	return nil
}

// cacheBulkTickerData caches bulk ticker data in Redis for API performance
func (c *CollectorService) cacheBulkTickerData(exchange string, marketData []models.MarketPrice) {
	if c.redisClient == nil {
		return
	}

	cacheKey := fmt.Sprintf("bulk_tickers:%s", exchange)
	dataJSON, err := json.Marshal(marketData)
	if err != nil {
		c.logger.WithError(err).Error("Failed to marshal bulk ticker data for caching")
		return
	}

	// Create timeout context for Redis operations
	operationID := fmt.Sprintf("redis_cache_%s_%d", exchange, time.Now().UnixNano())
	operationCtx := c.timeoutManager.CreateOperationContext("redis_cache", operationID)
	cancel := operationCtx.Cancel
	ctx := operationCtx.Ctx
	defer cancel()

	// Use circuit breaker for Redis operations
	err = c.circuitBreakerManager.GetOrCreate("redis", CircuitBreakerConfig{}).Execute(ctx, func(ctx context.Context) error {
		return c.errorRecoveryManager.ExecuteWithRetry(ctx, "redis_bulk_cache", func() error {
			return c.redisClient.Set(ctx, cacheKey, string(dataJSON), 10*time.Second).Err()
		})
	})

	if err != nil {
		c.logger.WithFields(map[string]interface{}{"exchange": exchange}).WithError(err).Error("Failed to cache bulk ticker data with error recovery")
	} else {
		c.logger.WithFields(map[string]interface{}{
			"count":    len(marketData),
			"exchange": exchange,
			"ttl":      "10s",
		}).Info("Cached tickers in Redis")
	}

	// Also cache individual ticker data for quick lookups with error recovery
	for _, ticker := range marketData {
		individualKey := fmt.Sprintf("ticker:%s:%s", ticker.ExchangeName, ticker.Symbol)
		tickerJSON, err := json.Marshal(ticker)
		if err != nil {
			continue
		}

		// Use circuit breaker for individual ticker caching
		if err := c.circuitBreakerManager.GetOrCreate("redis", CircuitBreakerConfig{}).Execute(ctx, func(ctx context.Context) error {
			return c.errorRecoveryManager.ExecuteWithRetry(ctx, "redis_individual_cache", func() error {
				return c.redisClient.Set(ctx, individualKey, string(tickerJSON), 10*time.Second).Err()
			})
		}); err != nil {
			// Log circuit breaker or caching failure but continue
			c.logger.WithFields(map[string]interface{}{
				"key": individualKey,
			}).WithError(err).Error("Failed to cache individual ticker")
		}
	}
}

// saveBulkTickerData validates and saves ticker data from bulk fetch to database
func (c *CollectorService) saveBulkTickerData(ticker models.MarketPrice) error {
	// Early validation: skip malformed symbols before any processing
	if !isValidSymbolFormat(ticker.Symbol) {
		c.logger.WithFields(map[string]interface{}{
			"symbol":   ticker.Symbol,
			"exchange": ticker.ExchangeName,
		}).Debug("Skipping malformed symbol")
		return nil
	}

	// Check if symbol should be blacklisted based on data quality
	if shouldBlacklist, reason := c.shouldBlacklistTicker(ticker); shouldBlacklist {
		symbolKey := fmt.Sprintf("%s:%s", ticker.ExchangeName, ticker.Symbol)
		ttl, _ := time.ParseDuration(c.config.Blacklist.TTL)
		c.blacklistCache.Add(symbolKey, reason, ttl)
		c.logger.WithFields(map[string]interface{}{
			"symbol": symbolKey,
			"reason": reason,
		}).Info("Added symbol to blacklist")
		return nil
	}

	// Ensure exchange exists and get its ID
	exchangeID, err := c.getOrCreateExchange(ticker.ExchangeName)
	if err != nil {
		return fmt.Errorf("failed to get or create exchange: %w", err)
	}

	// Ensure trading pair exists and get its ID
	tradingPairID, pairErr := c.getOrCreateTradingPair(exchangeID, ticker.Symbol)
	if pairErr != nil {
		return fmt.Errorf("failed to get or create trading pair: %w", pairErr)
	}

	// Validate price data before saving to database
	if validationErr := c.validateMarketData(&ticker, ticker.ExchangeName, ticker.Symbol); validationErr != nil {
		c.logger.WithFields(map[string]interface{}{
			"exchange": ticker.ExchangeName,
			"symbol":   ticker.Symbol,
		}).WithError(validationErr).Warn("Invalid market data")
		return nil // Don't save invalid data, but don't fail the collection
	}

	// Save market data to database with proper column mapping (including bid/ask for arbitrage)
	// NOTE: BidVolume and AskVolume are currently set to zero because CCXT ticker endpoint
	// does not provide these values. To get actual bid/ask volumes, the order book would need
	// to be fetched separately, which would significantly increase API calls and rate limits.
	// These fields are reserved for future implementation when order book data is integrated.
	_, err = c.db.Exec(c.ctx,
		`INSERT INTO market_data (
			exchange_id, trading_pair_id,
			bid, bid_volume, ask, ask_volume,
			last_price, volume_24h,
			timestamp, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		exchangeID, tradingPairID,
		ticker.Bid, ticker.BidVolume, ticker.Ask, ticker.AskVolume,
		ticker.Price, ticker.Volume,
		ticker.Timestamp, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save market data: %w", err)
	}

	// Signal first data collected (only once) - allows dependent services to start
	c.readinessMu.Lock()
	if !c.hasCollectedData {
		c.hasCollectedData = true
		close(c.dataReadyChan) // Signal all waiting goroutines
		c.logger.WithFields(map[string]interface{}{
			"exchange": ticker.ExchangeName,
			"symbol":   ticker.Symbol,
			"price":    ticker.Price,
		}).Info("First market data saved successfully")
	}
	c.readinessMu.Unlock()

	return nil
}

// shouldBlacklistTicker determines if a ticker should be blacklisted based on data quality
func (c *CollectorService) shouldBlacklistTicker(ticker models.MarketPrice) (bool, string) {
	// Check for zero or negative price
	if ticker.Price.IsZero() || ticker.Price.IsNegative() {
		return true, "invalid_price"
	}

	// Check for extremely high prices (potential data corruption)
	maxPrice := decimal.NewFromFloat(10000000) // 10 million
	if ticker.Price.GreaterThan(maxPrice) {
		return true, "price_too_high"
	}

	// Check for negative volume
	if ticker.Volume.IsNegative() {
		return true, "negative_volume"
	}

	// Check for stale timestamp (older than 1 hour)
	if time.Since(ticker.Timestamp) > time.Hour {
		return true, "stale_data"
	}

	return false, ""
}

// collectTickerDataDirect collects ticker data without checking symbol activity (for worker symbols)
func (c *CollectorService) collectTickerDataDirect(exchange, symbol string) error {
	// Early validation: skip malformed symbols before any processing
	if !isValidSymbolFormat(symbol) {
		c.logger.WithFields(map[string]interface{}{
			"symbol":   symbol,
			"exchange": exchange,
		}).Debug("Skipping malformed symbol")
		return nil
	}

	// Check if symbol is blacklisted before making API call
	symbolKey := fmt.Sprintf("%s:%s", exchange, symbol)
	if isBlacklisted, reason := c.blacklistCache.IsBlacklisted(symbolKey); isBlacklisted {
		c.logger.WithFields(map[string]interface{}{
			"symbol": symbolKey,
			"reason": reason,
		}).Info("Skipping blacklisted symbol")
		return nil
	}

	// Create timeout context for the operation
	operationID := fmt.Sprintf("ccxt_single_fetch_%s_%s_%d", exchange, symbol, time.Now().UnixNano())
	operationCtx := c.timeoutManager.CreateOperationContext("ccxt_single_fetch", operationID)
	cancel := operationCtx.Cancel
	ctx := operationCtx.Ctx
	defer cancel()

	// Register the operation with resource manager
	resourceID := fmt.Sprintf("single_fetch_%s_%s_%d", exchange, symbol, time.Now().UnixNano())
	c.resourceManager.RegisterResource(resourceID, GoroutineResource, func() error {
		cancel()
		return nil
	}, map[string]interface{}{"exchange": exchange, "symbol": symbol, "operation": "single_fetch"})
	defer func() {
		if err := c.resourceManager.CleanupResource(resourceID); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"resource": resourceID,
			}).WithError(err).Error("Failed to cleanup resource")
		}
	}()

	// Use per-exchange circuit breaker for CCXT service call with retry logic
	var ticker *models.MarketPrice
	cbErr := c.getExchangeCCXTCircuitBreaker(exchange).Execute(ctx, func(ctx context.Context) error {
		return c.errorRecoveryManager.ExecuteWithRetry(ctx, "ccxt_single_fetch", func() error {
			var retryErr error
			var resp ccxt.MarketPriceInterface
			resp, retryErr = c.ccxtService.FetchSingleTicker(ctx, exchange, symbol)
			if retryErr != nil {
				return retryErr
			}
			// Convert interface response to models.MarketPrice for downstream processing
			ticker = c.convertMarketPriceInterfaceToModel(resp)
			return nil
		})
	})

	if cbErr != nil {
		// Check if the error indicates a symbol that should be blacklisted
		if shouldBlacklist, reason := isBlacklistableError(cbErr); shouldBlacklist {
			symbolKey := fmt.Sprintf("%s:%s", exchange, symbol)
			ttl, _ := time.ParseDuration(c.config.Blacklist.TTL)
			c.blacklistCache.Add(symbolKey, reason, ttl)
			c.logger.WithFields(map[string]interface{}{
				"symbol": symbolKey,
				"reason": reason,
				"error":  cbErr,
			}).Info("Added symbol to blacklist with error")
			return nil
		}
		return fmt.Errorf("failed to fetch ticker data with circuit breaker: %w", cbErr)
	}

	// Ensure exchange exists and get its ID
	exchangeID, err := c.getOrCreateExchange(exchange)
	if err != nil {
		return fmt.Errorf("failed to get or create exchange: %w", err)
	}

	// Ensure trading pair exists and get its ID
	tradingPairID, pairErr := c.getOrCreateTradingPair(exchangeID, symbol)
	if pairErr != nil {
		return fmt.Errorf("failed to get or create trading pair: %w", pairErr)
	}

	// Validate price data before saving to database
	if validateErr := c.validateMarketData(ticker, exchange, symbol); validateErr != nil {
		c.logger.WithFields(map[string]interface{}{
			"exchange": exchange,
			"symbol":   symbol,
		}).WithError(validateErr).Warn("Invalid market data")
		return nil // Don't save invalid data, but don't fail the collection
	}

	// Save market data to database with proper column mapping
	_, err = c.db.Exec(c.ctx,
		`INSERT INTO market_data (exchange_id, trading_pair_id, last_price, volume_24h, timestamp, created_at) 
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		exchangeID, tradingPairID, ticker.Price, ticker.Volume, ticker.Timestamp, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save market data: %w", err)
	}

	return nil
}

// collectFundingRates collects funding rates for futures markets (legacy sequential version)
func (c *CollectorService) collectFundingRates(worker *Worker) error {
	return c.collectFundingRatesBulk(worker)
}

// collectFundingRatesBulk collects funding rates for futures markets using concurrent processing
func (c *CollectorService) collectFundingRatesBulk(worker *Worker) error {
	c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Starting concurrent funding rate collection")

	// Check if we already know this exchange doesn't support funding rates
	supports, known := c.exchangeCapabilityCache.SupportsFundingRates(worker.Exchange)
	if known && !supports {
		c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Skipping funding rate collection - exchange does not support funding rates")
		return nil
	}

	// Create timeout context for the operation
	operationID := fmt.Sprintf("ccxt_funding_rates_%s_%d", worker.Exchange, time.Now().UnixNano())
	operationCtx := c.timeoutManager.CreateOperationContext("ccxt_funding_rates", operationID)
	cancel := operationCtx.Cancel
	ctx := operationCtx.Ctx
	defer cancel()

	// Register the operation with resource manager
	resourceID := fmt.Sprintf("funding_rates_%s_%d", worker.Exchange, time.Now().UnixNano())
	c.resourceManager.RegisterResource(resourceID, GoroutineResource, func() error {
		cancel()
		return nil
	}, map[string]interface{}{"exchange": worker.Exchange, "operation": "funding_rates"})
	defer func() {
		if err := c.resourceManager.CleanupResource(resourceID); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"resource": resourceID,
			}).WithError(err).Error("Failed to cleanup resource")
		}
	}()

	// Use per-exchange circuit breaker for CCXT service call with retry logic
	var fundingRates []ccxt.FundingRate
	fetchErr := c.getExchangeCCXTCircuitBreaker(worker.Exchange).Execute(ctx, func(ctx context.Context) error {
		return c.errorRecoveryManager.ExecuteWithRetry(ctx, "ccxt_funding_rates", func() error {
			var retryErr error
			fundingRates, retryErr = c.ccxtService.FetchAllFundingRates(ctx, worker.Exchange)
			return retryErr
		})
	})

	if fetchErr != nil {
		// Check if this is a funding rate unsupported error
		if isFundingRateUnsupportedError(fetchErr) {
			c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Exchange does not support funding rates, caching this information")
			c.exchangeCapabilityCache.SetFundingRateSupport(worker.Exchange, false)
			return nil // Don't treat this as an error
		}
		return fmt.Errorf("failed to fetch funding rates for %s with circuit breaker: %w", worker.Exchange, fetchErr)
	}

	// If we successfully fetched funding rates, cache that this exchange supports them
	if !known {
		c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Exchange supports funding rates, caching this information")
		c.exchangeCapabilityCache.SetFundingRateSupport(worker.Exchange, true)
	}

	c.logger.WithFields(map[string]interface{}{
		"exchange": worker.Exchange,
		"count":    len(fundingRates),
	}).Info("Fetched funding rates")

	if len(fundingRates) == 0 {
		return nil
	}

	// Process funding rates concurrently using worker pool
	// Get dynamic concurrency limit from resource optimizer
	optimalConcurrency := c.resourceOptimizer.GetOptimalConcurrency()
	maxConcurrentWrites := optimalConcurrency.MaxConcurrentWrites // Dynamic limit for concurrent database writes
	semaphore := make(chan struct{}, maxConcurrentWrites)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	errorCount := 0

	for _, rate := range fundingRates {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
			wg.Add(1)
			go func(r ccxt.FundingRate) {
				defer wg.Done()

				// Acquire semaphore
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				if err := c.storeFundingRate(worker.Exchange, r); err != nil {
					c.logger.WithFields(map[string]interface{}{
						"exchange": worker.Exchange,
						"symbol":   r.Symbol,
					}).WithError(err).Error("Failed to store funding rate")
					mu.Lock()
					errorCount++
					mu.Unlock()
				} else {
					c.logger.WithFields(map[string]interface{}{
						"exchange": worker.Exchange,
						"symbol":   r.Symbol,
						"rate":     r.FundingRate,
					}).Info("Successfully stored funding rate")
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}(rate)
		}
	}

	// Wait for all goroutines to complete
	wg.Wait()

	c.logger.WithFields(map[string]interface{}{
		"exchange":   worker.Exchange,
		"successful": successCount,
		"errors":     errorCount,
	}).Info("Completed funding rate collection")
	return nil
}

// storeFundingRate stores funding rate data in the database
func (c *CollectorService) storeFundingRate(exchange string, rate ccxt.FundingRate) error {
	// Ensure exchange exists and get its ID
	exchangeID, err := c.getOrCreateExchange(exchange)
	if err != nil {
		return fmt.Errorf("failed to get or create exchange: %w", err)
	}

	// Ensure trading pair exists and get its ID
	tradingPairID, pairErr := c.getOrCreateTradingPair(exchangeID, rate.Symbol)
	if pairErr != nil {
		return fmt.Errorf("failed to get or create trading pair: %w", pairErr)
	}

	// Convert mark_price and index_price to decimal, handling zero values
	var markPrice, indexPrice *decimal.Decimal
	if rate.MarkPrice > 0 {
		mp := decimal.NewFromFloat(rate.MarkPrice)
		markPrice = &mp
	}
	if rate.IndexPrice > 0 {
		ip := decimal.NewFromFloat(rate.IndexPrice)
		indexPrice = &ip
	}

	// Use rate.Timestamp if available, otherwise use current time
	timestamp := time.Now()
	if !rate.Timestamp.Time().IsZero() {
		timestamp = rate.Timestamp.Time()
	}

	// Save funding rate to database with upsert to handle duplicates
	now := time.Now()
	_, err = c.db.Exec(c.ctx,
		`INSERT INTO funding_rates (exchange_id, trading_pair_id, funding_rate, funding_time, next_funding_time, mark_price, index_price, timestamp, created_at) 
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (exchange_id, trading_pair_id, funding_time) 
		 DO UPDATE SET 
			funding_rate = EXCLUDED.funding_rate,
			next_funding_time = EXCLUDED.next_funding_time,
			mark_price = EXCLUDED.mark_price,
			index_price = EXCLUDED.index_price,
			timestamp = EXCLUDED.timestamp,
			updated_at = NOW()`,
		exchangeID, tradingPairID, rate.FundingRate, rate.FundingTimestamp.Time(), rate.NextFundingTime.Time(), markPrice, indexPrice, timestamp, now)
	if err != nil {
		return fmt.Errorf("failed to save funding rate: %w", err)
	}

	// Invalidate cached funding rates for this exchange and trading pair
	if c.redisClient != nil {
		// Clear funding rates cache for this specific exchange-trading pair combination
		fundingRateKey := fmt.Sprintf("funding_rates:%s:%d", exchange, tradingPairID)
		c.redisClient.Del(c.ctx, fundingRateKey)

		// Clear general funding rates cache for this exchange
		exchangeFundingKey := fmt.Sprintf("funding_rates:%s", exchange)
		c.redisClient.Del(c.ctx, exchangeFundingKey)

		// Clear latest funding rates cache
		latestFundingKey := "latest_funding_rates"
		c.redisClient.Del(c.ctx, latestFundingKey)

		c.logger.WithFields(map[string]interface{}{
			"exchange":        exchange,
			"trading_pair_id": tradingPairID,
		}).Info("Invalidated funding rate caches")
	}

	return nil
}

// GetWorkerStatus returns the status of all workers.
//
// Returns:
//
//	map[string]*Worker: Map of exchange to worker status.
func (c *CollectorService) GetWorkerStatus() map[string]*Worker {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := make(map[string]*Worker)
	for exchange, worker := range c.workers {
		status[exchange] = &Worker{
			Exchange:   worker.Exchange,
			Symbols:    worker.Symbols,
			Interval:   worker.Interval,
			LastUpdate: worker.LastUpdate,
			IsRunning:  worker.IsRunning,
			ErrorCount: worker.ErrorCount,
			MaxErrors:  worker.MaxErrors,
		}
	}
	return status
}

// RestartWorker restarts a specific worker.
//
// Parameters:
//
//	exchangeID: Exchange identifier.
//
// Returns:
//
//	error: Error if worker not found.
func (c *CollectorService) RestartWorker(exchangeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	worker, exists := c.workers[exchangeID]
	if !exists {
		return fmt.Errorf("worker for exchange %s not found", exchangeID)
	}

	// Reset worker state
	worker.ErrorCount = 0
	worker.IsRunning = true

	// Start new worker goroutine
	c.wg.Add(1)
	go c.runWorker(worker)

	c.logger.WithFields(map[string]interface{}{"exchange": exchangeID}).Info("Restarted worker")
	return nil
}

// IsHealthy checks if the collector service is healthy.
//
// Returns:
//
//	bool: True if healthy.
func (c *CollectorService) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.workers) == 0 {
		return false
	}

	// Check if at least 50% of workers are running
	runningWorkers := 0
	for _, worker := range c.workers {
		if worker.IsRunning {
			runningWorkers++
		}
	}

	return float64(runningWorkers)/float64(len(c.workers)) >= 0.5
}

// GetCircuitBreakerStats returns statistics for all circuit breakers.
//
// Returns:
//
//	map[string]CircuitBreakerStats: Map of breaker name to stats.
func (c *CollectorService) GetCircuitBreakerStats() map[string]CircuitBreakerStats {
	if c.circuitBreakerManager == nil {
		return make(map[string]CircuitBreakerStats)
	}
	return c.circuitBreakerManager.GetAllStats()
}

// ResetCircuitBreaker resets a specific circuit breaker by name.
//
// Parameters:
//
//	name: The name of the circuit breaker to reset.
//
// Returns:
//
//	bool: True if the breaker was found and reset.
func (c *CollectorService) ResetCircuitBreaker(name string) bool {
	if c.circuitBreakerManager == nil {
		return false
	}
	c.circuitBreakerManager.mu.RLock()
	breaker, exists := c.circuitBreakerManager.breakers[name]
	c.circuitBreakerManager.mu.RUnlock()
	if !exists {
		return false
	}
	breaker.Reset()
	c.logger.Info("Circuit breaker manually reset", "name", name)
	return true
}

// ResetAllCircuitBreakers resets all circuit breakers.
func (c *CollectorService) ResetAllCircuitBreakers() {
	if c.circuitBreakerManager == nil {
		return
	}
	c.circuitBreakerManager.ResetAll()
	c.logger.Info("All circuit breakers manually reset")
}

// GetCircuitBreakerNames returns the names of all registered circuit breakers.
//
// Returns:
//
//	[]string: List of circuit breaker names.
func (c *CollectorService) GetCircuitBreakerNames() []string {
	if c.circuitBreakerManager == nil {
		return nil
	}
	c.circuitBreakerManager.mu.RLock()
	defer c.circuitBreakerManager.mu.RUnlock()
	names := make([]string, 0, len(c.circuitBreakerManager.breakers))
	for name := range c.circuitBreakerManager.breakers {
		names = append(names, name)
	}
	return names
}

// ensureTradingPairExists ensures a trading pair exists in the database
func (c *CollectorService) ensureTradingPairExists(exchangeID int, symbol string) error {
	_, err := c.getOrCreateTradingPair(exchangeID, symbol)
	return err
}

// getOrCreateTradingPair gets or creates a trading pair and returns its ID
func (c *CollectorService) getOrCreateTradingPair(exchangeID int, symbol string) (int, error) {
	// Try Redis cache first if available
	if c.redisClient != nil {
		cacheKey := fmt.Sprintf("trading_pair:%d:%s", exchangeID, symbol)
		cachedID, err := c.redisClient.Get(c.ctx, cacheKey).Result()
		if err == nil {
			if tradingPairID, parseErr := strconv.Atoi(cachedID); parseErr == nil {
				return tradingPairID, nil
			}
		}
	}

	// First try to get existing trading pair for this exchange and symbol
	var tradingPairID int
	err := c.db.QueryRow(c.ctx, "SELECT id FROM trading_pairs WHERE exchange_id = $1 AND symbol = $2", exchangeID, symbol).Scan(&tradingPairID)
	if err == nil {
		// Cache the result if Redis is available
		if c.redisClient != nil {
			cacheKey := fmt.Sprintf("trading_pair:%d:%s", exchangeID, symbol)
			c.redisClient.Set(c.ctx, cacheKey, tradingPairID, 24*time.Hour) // Cache for 24 hours
		}
		return tradingPairID, nil
	}

	// If not found, create new trading pair
	baseCurrency, quoteCurrency := c.parseSymbol(symbol)
	if baseCurrency == "" || quoteCurrency == "" {
		return 0, fmt.Errorf("failed to parse symbol: %s", symbol)
	}

	// Insert new trading pair with exchange_id and conflict resolution
	err = c.db.QueryRow(c.ctx,
		"INSERT INTO trading_pairs (exchange_id, symbol, base_currency, quote_currency, is_active) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (exchange_id, symbol) DO UPDATE SET base_currency = EXCLUDED.base_currency, quote_currency = EXCLUDED.quote_currency, is_active = EXCLUDED.is_active RETURNING id",
		exchangeID, symbol, baseCurrency, quoteCurrency, true).Scan(&tradingPairID)
	if err != nil {
		return 0, fmt.Errorf("failed to create or update trading pair: %w", err)
	}

	// Cache the newly created trading pair if Redis is available
	if c.redisClient != nil {
		cacheKey := fmt.Sprintf("trading_pair:%d:%s", exchangeID, symbol)
		c.redisClient.Set(c.ctx, cacheKey, tradingPairID, 24*time.Hour) // Cache for 24 hours
	}

	c.logger.WithFields(map[string]interface{}{
		"symbol":          symbol,
		"exchange_id":     exchangeID,
		"trading_pair_id": tradingPairID,
	}).Info("Created new trading pair")
	return tradingPairID, nil
}

// getOrCreateExchange gets or creates an exchange and returns its ID
func (c *CollectorService) getOrCreateExchange(ccxtID string) (int, error) {
	// Check Redis cache first
	cacheKey := fmt.Sprintf("exchange:ccxt_id:%s", ccxtID)
	if cachedID, err := c.redisClient.Get(c.ctx, cacheKey).Result(); err == nil {
		if exchangeID, err := strconv.Atoi(cachedID); err == nil {
			return exchangeID, nil
		}
	}

	// First try to get existing exchange by ccxt_id
	var exchangeID int
	err := c.db.QueryRow(c.ctx, "SELECT id FROM exchanges WHERE ccxt_id = $1", ccxtID).Scan(&exchangeID)
	if err == nil {
		// Cache the result
		c.redisClient.Set(c.ctx, cacheKey, exchangeID, 24*time.Hour)
		return exchangeID, nil
	}

	// Also check by name in case exchange exists with different ccxt_id
	name := strings.ToLower(ccxtID)
	err = c.db.QueryRow(c.ctx, "SELECT id FROM exchanges WHERE name = $1", name).Scan(&exchangeID)
	if err == nil {
		c.logger.WithFields(map[string]interface{}{
			"name":        name,
			"exchange_id": exchangeID,
		}).Info("Found existing exchange by name")
		// Cache the result
		c.redisClient.Set(c.ctx, cacheKey, exchangeID, 24*time.Hour)
		return exchangeID, nil
	}

	// If not found, create new exchange with basic information
	caser := cases.Title(language.English)
	displayName := caser.String(ccxtID)

	// Insert new exchange with conflict resolution
	// Use a default API URL based on the exchange name since CCXT doesn't provide this directly
	defaultAPIURL := fmt.Sprintf("https://api.%s.com", strings.ToLower(ccxtID))
	err = c.db.QueryRow(c.ctx,
		"INSERT INTO exchanges (name, display_name, ccxt_id, api_url, status, has_spot, has_futures) VALUES ($1, $2, $3, $4, 'active', true, true) ON CONFLICT (name) DO UPDATE SET ccxt_id = EXCLUDED.ccxt_id, display_name = EXCLUDED.display_name, api_url = EXCLUDED.api_url RETURNING id",
		name, displayName, ccxtID, defaultAPIURL).Scan(&exchangeID)
	if err != nil {
		return 0, fmt.Errorf("failed to create or update exchange: %w", err)
	}

	// Cache the newly created/updated exchange
	c.redisClient.Set(c.ctx, cacheKey, exchangeID, 24*time.Hour)

	c.logger.WithFields(map[string]interface{}{
		"ccxt_id":     ccxtID,
		"exchange_id": exchangeID,
	}).Info("Created or updated exchange")
	return exchangeID, nil
}

// isValidSymbolFormat validates that a symbol has a valid format before processing.
// This filters out malformed symbols like ":" that some exchanges return.
func isValidSymbolFormat(symbol string) bool {
	// Reject empty or whitespace-only symbols
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return false
	}

	// Reject symbols that are just separators (no actual currency data)
	if symbol == ":" || symbol == "/" || symbol == "_" || symbol == "-" {
		return false
	}

	// Check if symbol contains a valid separator with content on both sides
	separators := []string{"/", ":", "_", "-"}
	for _, sep := range separators {
		if strings.Contains(symbol, sep) {
			parts := strings.SplitN(symbol, sep, 2)
			if len(parts) == 2 {
				// Both parts must have content (not just whitespace)
				base := strings.TrimSpace(parts[0])
				quote := strings.TrimSpace(parts[1])
				if len(base) > 0 && len(quote) > 0 {
					return true
				}
				// If separator exists but parts are empty, it's invalid
				return false
			}
		}
	}

	// Allow symbols without separators (some exchanges use concatenated format like BTCUSDT)
	// Must be at least 3 characters (minimum viable symbol length)
	return len(symbol) >= 3
}

// parseSymbol parses a trading symbol into base and quote currencies
// Improved version with more robust parsing logic to handle various symbol formats
func (c *CollectorService) parseSymbol(symbol string) (string, string) {
	// Handle common separators
	if strings.Contains(symbol, "/") {
		parts := strings.Split(symbol, "/")
		if len(parts) >= 2 {
			base := parts[0]
			quote := strings.Split(parts[1], ":")[0] // Remove settlement currency if present
			return base, quote
		}
	}

	// Handle symbols with underscores (some exchanges use this format)
	if strings.Contains(symbol, "_") {
		parts := strings.Split(symbol, "_")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	}

	// Handle symbols with dashes (some exchanges use this format)
	if strings.Contains(symbol, "-") && !c.isOptionsContract(symbol) {
		parts := strings.Split(symbol, "-")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	}

	// Handle symbols without separators (like BTCUSDT)
	// Order matters: longer quotes first to avoid incorrect parsing
	commonQuotes := []string{
		"USDT", "USDC", "BUSD", "TUSD", "FDUSD", // Stablecoins
		"BTC", "ETH", "BNB", "ADA", "DOT", "SOL", // Major cryptos
		"USD", "EUR", "GBP", "JPY", "AUD", "CAD", // Fiat currencies
		"DOGE", "SHIB", "MATIC", "AVAX", "LINK", // Other popular cryptos
	}

	for _, quote := range commonQuotes {
		if strings.HasSuffix(symbol, quote) {
			base := strings.TrimSuffix(symbol, quote)
			// Ensure base currency is not empty and reasonable length
			if len(base) > 0 && len(base) <= 20 {
				return base, quote
			}
		}
	}

	// Last resort: if no pattern matches, return empty strings
	// This prevents incorrect parsing that could cause data corruption
	return "", ""
}

// filterValidSymbols filters out invalid symbols that cause ticker fetch errors
func (c *CollectorService) filterValidSymbols(symbols []string) []string {
	var validSymbols []string

	for _, symbol := range symbols {
		// Skip options contracts (contain dates and strike prices)
		if c.isOptionsContract(symbol) {
			continue
		}

		// Skip symbols with unusual formats that typically cause errors
		if c.isInvalidSymbolFormat(symbol) {
			continue
		}

		validSymbols = append(validSymbols, symbol)
	}

	return validSymbols
}

// isOptionsContract checks if a symbol represents an options contract
func (c *CollectorService) isOptionsContract(symbol string) bool {
	// Options contracts typically have dates and strike prices
	// Examples: SOLUSDT:USDT-250815-180-C, BTC-25DEC20-20000-C
	return strings.Contains(symbol, "-C") || strings.Contains(symbol, "-P") ||
		(strings.Contains(symbol, "-") && (strings.Contains(symbol, "20") || strings.Contains(symbol, "25")))
}

// isInvalidSymbolFormat checks for other invalid symbol formats
func (c *CollectorService) isInvalidSymbolFormat(symbol string) bool {
	// Skip symbols that are too long (align with database VARCHAR(50) limit)
	// Increased from 20 to 50 to match database schema and handle longer derivative symbols
	if len(symbol) > 50 {
		return true
	}

	// Skip symbols with multiple colons (complex derivatives)
	if strings.Count(symbol, ":") > 1 {
		return true
	}

	// Skip symbols with both underscores and dashes (likely complex derivatives)
	// Note: We now handle single underscore or dash as valid separators
	if strings.Contains(symbol, "_") && strings.Contains(symbol, "-") {
		return true
	}

	return false
}

// fetchAndCacheSymbols fetches symbols from CCXT service and populates cache (used during startup)
func (c *CollectorService) fetchAndCacheSymbols(exchangeID string) ([]string, error) {
	c.logger.WithFields(map[string]interface{}{"exchange": exchangeID}).Info("Fetching active markets")

	// Add timeout context for better error handling
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	markets, err := c.ccxtService.FetchMarkets(ctx, exchangeID)
	if err != nil {
		// Log warning but don't fail startup for individual exchange errors
		c.logger.WithFields(map[string]interface{}{
			"exchange": exchangeID,
		}).WithError(err).Warn("Failed to fetch markets - exchange may be unavailable")
		return []string{}, nil // Return empty slice instead of error
	}

	// Since markets.Symbols is a slice of strings, we assume CCXT returns only active symbols
	// Filter out empty strings and invalid formats (like ":" which some exchanges return)
	var activeSymbols []string
	for _, symbol := range markets.Symbols {
		if !isValidSymbolFormat(symbol) {
			continue
		}
		activeSymbols = append(activeSymbols, symbol)
	}

	// Cache the symbols
	c.symbolCache.Set(exchangeID, activeSymbols)

	// Update last refresh time
	c.symbolRefreshMu.Lock()
	c.lastSymbolRefresh[exchangeID] = time.Now()
	c.symbolRefreshMu.Unlock()

	c.logger.WithFields(map[string]interface{}{
		"exchange": exchangeID,
		"count":    len(activeSymbols),
	}).Info("Successfully fetched symbols")
	return activeSymbols, nil
}

// getMultiExchangeSymbols collects symbols from all exchanges and returns those that appear on multiple exchanges
// This function also populates the symbol cache to avoid double API calls during startup
func (c *CollectorService) getMultiExchangeSymbols(exchanges []string) (map[string]int, error) {
	return c.getMultiExchangeSymbolsConcurrent(exchanges)
}

// getMultiExchangeSymbolsConcurrent collects symbols from all exchanges concurrently
// Uses goroutines with semaphore to limit concurrent requests and improve startup performance
// Implements graceful degradation with fallback to sequential processing
func (c *CollectorService) getMultiExchangeSymbolsConcurrent(exchanges []string) (map[string]int, error) {
	symbolCount := make(map[string]int)
	minExchanges := 2 // Minimum number of exchanges a symbol must appear on

	// Get dynamic concurrency limit from resource optimizer
	optimalConcurrency := c.resourceOptimizer.GetOptimalConcurrency()
	maxConcurrent := optimalConcurrency.MaxConcurrentSymbols

	c.logger.WithFields(map[string]interface{}{
		"exchange_count": len(exchanges),
		"max_concurrent": maxConcurrent,
		"purpose":        "arbitrage_filtering",
	}).Info("Collecting symbols from exchanges concurrently")

	// Create timeout context for the entire operation
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Minute)
	defer cancel()

	// Try concurrent approach first
	multiExchangeSymbols, err := c.tryGetSymbolsConcurrent(ctx, exchanges, symbolCount, minExchanges, maxConcurrent)
	if err != nil {
		c.logger.WithError(err).Warn("Concurrent symbol collection failed, falling back to sequential processing")
		// Fallback to sequential processing
		return c.getSymbolsSequential(exchanges, minExchanges)
	}

	return multiExchangeSymbols, nil
}

// tryGetSymbolsConcurrent attempts concurrent symbol collection with error recovery
func (c *CollectorService) tryGetSymbolsConcurrent(ctx context.Context, exchanges []string, symbolCount map[string]int, minExchanges, maxConcurrent int) (map[string]int, error) {
	// Create semaphore to limit concurrent requests
	semaphore := make(chan struct{}, maxConcurrent)

	// Mutex to protect shared symbolCount map
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Channel to collect results from goroutines
	type exchangeResult struct {
		exchangeID string
		symbols    []string
		err        error
	}
	resultChan := make(chan exchangeResult, len(exchanges))

	// Track failed exchanges for fallback decision
	failedExchanges := int64(0)
	maxFailures := int64(len(exchanges) / 2) // Allow up to 50% failures

	// Start goroutines for each exchange
	for _, exchangeID := range exchanges {
		wg.Add(1)
		go func(exID string) {
			defer wg.Done()

			// Check context cancellation
			select {
			case <-ctx.Done():
				resultChan <- exchangeResult{
					exchangeID: exID,
					err:        ctx.Err(),
				}
				return
			default:
			}

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Execute with circuit breaker and retry logic
			var activeSymbols []string
			err := c.errorRecoveryManager.ExecuteWithRetry(ctx, "symbol_fetch", func() error {
				var fetchErr error
				activeSymbols, fetchErr = c.fetchAndCacheSymbols(exID)
				return fetchErr
			})

			if err != nil {
				atomic.AddInt64(&failedExchanges, 1)
			}

			resultChan <- exchangeResult{
				exchangeID: exID,
				symbols:    activeSymbols,
				err:        err,
			}
		}(exchangeID)
	}

	// Close result channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Process results as they come in
	successfulExchanges := 0
	for result := range resultChan {
		// Check if we should abort due to too many failures
		if atomic.LoadInt64(&failedExchanges) > maxFailures {
			return nil, fmt.Errorf("too many exchange failures (%d/%d), aborting concurrent collection",
				atomic.LoadInt64(&failedExchanges), len(exchanges))
		}

		if result.err != nil {
			c.logger.Warn("Failed to fetch active symbols", "exchange", result.exchangeID, "error", result.err)
			continue
		}

		// Filter valid symbols for this exchange
		validSymbols := c.filterValidSymbols(result.symbols)

		// Thread-safe update of symbol count
		mu.Lock()
		for _, symbol := range validSymbols {
			symbolCount[symbol]++
		}
		mu.Unlock()

		successfulExchanges++
		c.logger.WithFields(map[string]interface{}{
			"count":     len(validSymbols),
			"exchange":  result.exchangeID,
			"completed": successfulExchanges,
			"total":     len(exchanges),
		}).Info("Found valid active symbols")
	}

	// Check if we have enough successful exchanges
	if successfulExchanges < 2 {
		return nil, fmt.Errorf("insufficient successful exchanges (%d), need at least 2", successfulExchanges)
	}

	// Filter to only symbols that appear on multiple exchanges
	multiExchangeSymbols := make(map[string]int)
	for symbol, count := range symbolCount {
		if count >= minExchanges {
			multiExchangeSymbols[symbol] = count
		}
	}

	c.logger.WithFields(map[string]interface{}{
		"symbols_on_multiple_exchanges": len(multiExchangeSymbols),
		"min_exchanges":                 minExchanges,
		"total_unique_symbols":          len(symbolCount),
		"successful_exchanges":          successfulExchanges,
	}).Info("Concurrent symbol collection completed")

	return multiExchangeSymbols, nil
}

// getSymbolsSequential fallback method for sequential symbol collection
func (c *CollectorService) getSymbolsSequential(exchanges []string, minExchanges int) (map[string]int, error) {
	c.logger.WithFields(map[string]interface{}{"exchange_count": len(exchanges)}).Info("Starting sequential symbol collection")

	symbolCount := make(map[string]int)
	successfulExchanges := 0

	for _, exchangeID := range exchanges {
		// Execute with retry logic for each exchange
		var activeSymbols []string
		err := c.errorRecoveryManager.ExecuteWithRetry(c.ctx, "symbol_fetch", func() error {
			var fetchErr error
			activeSymbols, fetchErr = c.fetchAndCacheSymbols(exchangeID)
			return fetchErr
		})

		if err != nil {
			c.logger.WithFields(map[string]interface{}{
				"exchange": exchangeID,
			}).WithError(err).Warn("Failed to fetch symbols in sequential mode")
			continue
		}

		// Filter valid symbols for this exchange
		validSymbols := c.filterValidSymbols(activeSymbols)

		// Update symbol count
		for _, symbol := range validSymbols {
			symbolCount[symbol]++
		}

		successfulExchanges++
		c.logger.WithFields(map[string]interface{}{
			"count":               len(validSymbols),
			"exchange":            exchangeID,
			"processed_exchanges": successfulExchanges,
			"total":               len(exchanges),
		}).Info("Sequential: Found valid symbols")
	}

	// Filter to only symbols that appear on multiple exchanges
	multiExchangeSymbols := make(map[string]int)
	for symbol, count := range symbolCount {
		if count >= minExchanges {
			multiExchangeSymbols[symbol] = count
		}
	}

	c.logger.WithFields(map[string]interface{}{
		"symbols_on_multiple_exchanges": len(multiExchangeSymbols),
		"min_exchanges":                 minExchanges,
		"total_unique_symbols":          len(symbolCount),
		"successful_exchanges":          successfulExchanges,
	}).Info("Sequential symbol collection completed")

	return multiExchangeSymbols, nil
}

// filterArbitrageSymbols filters symbols to only include those that appear on multiple exchanges
func (c *CollectorService) filterArbitrageSymbols(symbols []string, multiExchangeSymbols map[string]int) []string {
	if len(multiExchangeSymbols) == 0 {
		return symbols // Return all symbols if no multi-exchange data available
	}

	var arbitrageSymbols []string
	for _, symbol := range symbols {
		if _, exists := multiExchangeSymbols[symbol]; exists {
			arbitrageSymbols = append(arbitrageSymbols, symbol)
		}
	}

	return arbitrageSymbols
}

// validateMarketData validates ticker data before saving to database
func (c *CollectorService) validateMarketData(ticker *models.MarketPrice, exchange, symbol string) error {
	// Check for zero or negative price
	if ticker.Price.IsZero() || ticker.Price.IsNegative() {
		return fmt.Errorf("invalid price: %s for %s on %s", ticker.Price, symbol, exchange)
	}

	// Check for extremely high prices (potential data corruption)
	maxPrice := decimal.NewFromFloat(10000000) // 10 million
	if ticker.Price.GreaterThan(maxPrice) {
		return fmt.Errorf("extremely high price: %s for %s on %s", ticker.Price, symbol, exchange)
	}

	// Check for negative volume
	if ticker.Volume.IsNegative() {
		return fmt.Errorf("negative volume: %s for %s on %s", ticker.Volume, symbol, exchange)
	}

	// Check for invalid timestamp
	timestamp := ticker.Timestamp
	now := time.Now()

	// Check if timestamp is in the future (more than 1 minute)
	if timestamp.After(now.Add(time.Minute)) {
		return fmt.Errorf("future timestamp: %s for %s on %s", timestamp, symbol, exchange)
	}

	// Check if timestamp is too old (more than 24 hours)
	if timestamp.Before(now.Add(-24 * time.Hour)) {
		return fmt.Errorf("old timestamp: %s for %s on %s", timestamp, symbol, exchange)
	}

	// Anti-manipulation: Check for price outliers (>50% move in 1 minute)
	if err := c.checkPriceOutlier(ticker, exchange, symbol); err != nil {
		return err
	}

	// Anti-manipulation: Check for volume anomalies (potential wash trading)
	if err := c.checkVolumeAnomaly(ticker, exchange, symbol); err != nil {
		return err
	}

	return nil
}

// checkPriceOutlier checks if price moved more than 50% in 1 minute (potential manipulation)
func (c *CollectorService) checkPriceOutlier(ticker *models.MarketPrice, exchange, symbol string) error {
	key := fmt.Sprintf("%s:%s", exchange, symbol)

	var prevPrice decimal.Decimal
	var prevTime time.Time

	if cached, ok := c.lastPrice.Load(key); ok {
		entry := cached.(priceCacheEntry)
		prevPrice = entry.price
		prevTime = entry.timestamp
	}

	if prevPrice.IsZero() {
		c.lastPrice.Store(key, priceCacheEntry{
			price:     ticker.Price,
			timestamp: ticker.Timestamp,
		})
		return nil
	}

	timeDiff := ticker.Timestamp.Sub(prevTime)
	if timeDiff < 30*time.Second {
		c.lastPrice.Store(key, priceCacheEntry{
			price:     ticker.Price,
			timestamp: ticker.Timestamp,
		})
		return nil
	}

	priceChange := ticker.Price.Sub(prevPrice).Abs().Div(prevPrice).Mul(decimal.NewFromInt(100))

	maxAllowedChange := decimal.NewFromFloat(50.0)
	if priceChange.GreaterThan(maxAllowedChange) {
		c.logger.WithFields(map[string]interface{}{
			"exchange":       exchange,
			"symbol":         symbol,
			"previous_price": prevPrice,
			"current_price":  ticker.Price,
			"price_change":   priceChange,
			"time_diff_sec":  timeDiff.Seconds(),
		}).Warn("Price outlier detected - potential manipulation")

		c.blacklistCache.Add(
			key,
			fmt.Sprintf("Price moved %.1f%% in %.0fs", priceChange.InexactFloat64(), timeDiff.Seconds()),
			5*time.Minute,
		)

		return fmt.Errorf("price outlier detected: %.1f%% change in %.0fs (max 50%% allowed)", priceChange.InexactFloat64(), timeDiff.Seconds())
	}

	c.lastPrice.Store(key, priceCacheEntry{
		price:     ticker.Price,
		timestamp: ticker.Timestamp,
	})

	return nil
}

// checkVolumeAnomaly checks for suspicious volume patterns (potential wash trading)
func (c *CollectorService) checkVolumeAnomaly(ticker *models.MarketPrice, exchange, symbol string) error {
	// Skip if volume is zero (no trading)
	if ticker.Volume.IsZero() {
		return nil
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)

	var avgVolume decimal.Decimal
	var sampleCount int

	if cached, ok := c.volumeStats.Load(key); ok {
		entry := cached.(volumeStatsEntry)
		avgVolume = entry.avgVolume
		sampleCount = entry.sampleCount
	}

	if sampleCount < 5 {
		newAvg := avgVolume.Mul(decimal.NewFromInt(int64(sampleCount))).Add(ticker.Volume).
			Div(decimal.NewFromInt(int64(sampleCount + 1)))

		c.volumeStats.Store(key, volumeStatsEntry{
			avgVolume:   newAvg,
			sampleCount: sampleCount + 1,
		})
		return nil
	}

	volumeRatio := ticker.Volume.Div(avgVolume)
	maxVolumeRatio := decimal.NewFromFloat(10.0)

	if volumeRatio.GreaterThan(maxVolumeRatio) {
		c.logger.WithFields(map[string]interface{}{
			"exchange":       exchange,
			"symbol":         symbol,
			"current_volume": ticker.Volume,
			"avg_volume":     avgVolume,
			"volume_ratio":   volumeRatio,
		}).Debug("High volume detected - possible wash trading")

		newAvg := avgVolume.Mul(decimal.NewFromFloat(0.7)).Add(ticker.Volume.Mul(decimal.NewFromFloat(0.3)))
		c.volumeStats.Store(key, volumeStatsEntry{
			avgVolume:   newAvg,
			sampleCount: sampleCount + 1,
		})

		return nil
	}

	newAvg := avgVolume.Mul(decimal.NewFromFloat(0.9)).Add(ticker.Volume.Mul(decimal.NewFromFloat(0.1)))
	c.volumeStats.Store(key, volumeStatsEntry{
		avgVolume:   newAvg,
		sampleCount: sampleCount + 1,
	})

	return nil
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

// PerformBackfillIfNeeded checks if backfill is needed and performs it
func (c *CollectorService) PerformBackfillIfNeeded() error {
	if !c.backfillConfig.Enabled {
		c.logger.Info("Backfill is disabled in configuration, skipping historical data collection")
		return nil
	}

	c.logger.WithFields(map[string]interface{}{
		"backfill_hours":  c.backfillConfig.BackfillHours,
		"threshold_hours": c.backfillConfig.MinDataThresholdHours,
		"batch_size":      c.backfillConfig.BatchSize,
	}).Info("Checking if historical data backfill is needed")

	// Check if we have sufficient market data
	needsBackfill, err := c.checkIfBackfillNeeded()
	if err != nil {
		return fmt.Errorf("failed to check backfill requirement: %w", err)
	}

	if !needsBackfill {
		c.logger.WithFields(map[string]interface{}{"threshold_hours": c.backfillConfig.MinDataThresholdHours}).Info("Sufficient market data available, skipping backfill")
		return nil
	}

	c.logger.WithFields(map[string]interface{}{"backfill_hours": c.backfillConfig.BackfillHours}).Info("Insufficient market data detected, starting historical backfill")
	startTime := time.Now()
	err = c.performHistoricalBackfill()
	if err != nil {
		return err
	}

	c.logger.WithFields(map[string]interface{}{"duration": time.Since(startTime)}).Info("Historical backfill process completed")
	return nil
}

// checkIfBackfillNeeded determines if backfill is required based on available data.
// It checks if any active exchange has recent market data (within the last hour).
// This is more robust than just counting total records - it ensures each exchange has coverage.
func (c *CollectorService) checkIfBackfillNeeded() (bool, error) {
	thresholdTime := time.Now().Add(-time.Duration(c.backfillConfig.MinDataThresholdHours) * time.Hour)

	var activeExchangeCount int
	err := c.db.QueryRow(c.ctx,
		"SELECT COUNT(*) FROM exchanges WHERE status = 'active'").Scan(&activeExchangeCount)
	if err != nil {
		c.logger.WithError(err).Warn("Failed to count active exchanges, assuming backfill needed")
		return true, nil // Assume backfill needed if we can't check
	}

	if activeExchangeCount == 0 {
		c.logger.Info("No active exchanges in database, backfill will create them")
		return true, nil
	}

	// Check if ANY active exchange has data in the last hour
	// This is more lenient than requiring ALL exchanges to have data
	var exchangesWithRecentData int
	err = c.db.QueryRow(c.ctx, `
		SELECT COUNT(DISTINCT md.exchange_id)
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		WHERE md.timestamp >= NOW() - INTERVAL '1 hour'
		  AND e.status = 'active'
	`).Scan(&exchangesWithRecentData)
	if err != nil {
		c.logger.WithError(err).Warn("Failed to check exchanges with recent data, assuming backfill needed")
		return true, nil
	}

	// Also check total records in threshold period for context
	var totalRecords int
	err = c.db.QueryRow(c.ctx,
		"SELECT COUNT(*) FROM market_data WHERE timestamp >= $1",
		thresholdTime).Scan(&totalRecords)
	if err != nil {
		c.logger.WithError(err).Warn("Failed to count market data records")
		totalRecords = 0
	}

	c.logger.WithFields(map[string]interface{}{
		"active_exchanges":           activeExchangeCount,
		"exchanges_with_recent_data": exchangesWithRecentData,
		"total_records_in_threshold": totalRecords,
		"threshold_hours":            c.backfillConfig.MinDataThresholdHours,
		"threshold_time":             thresholdTime.Format("2006-01-02 15:04"),
	}).Info("Market data availability check")

	// Need backfill if:
	// 1. No exchanges have any recent data (last hour), OR
	// 2. Total records are below minimum threshold (for edge cases)
	minRecordsThreshold := 100
	needsBackfill := exchangesWithRecentData == 0 || totalRecords < minRecordsThreshold

	if needsBackfill {
		reason := "insufficient records"
		if exchangesWithRecentData == 0 {
			reason = "no exchanges with recent data"
		}
		c.logger.WithFields(map[string]interface{}{
			"reason":              reason,
			"exchanges_with_data": exchangesWithRecentData,
			"total_records":       totalRecords,
			"minimum_required":    minRecordsThreshold,
		}).Info("Backfill required")
	} else {
		c.logger.WithFields(map[string]interface{}{
			"exchanges_with_data": exchangesWithRecentData,
			"total_records":       totalRecords,
		}).Info("Sufficient data available - skipping backfill")
	}

	return needsBackfill, nil
}

// performHistoricalBackfill collects historical data for active trading pairs (sequential version)
func (c *CollectorService) performHistoricalBackfill() error {
	return c.performHistoricalBackfillConcurrent()
}

// performHistoricalBackfillConcurrent collects historical data for active trading pairs using concurrent processing
func (c *CollectorService) performHistoricalBackfillConcurrent() error {
	c.logger.WithFields(map[string]interface{}{"backfill_hours": c.backfillConfig.BackfillHours}).Info("Starting concurrent historical data backfill")
	backfillStartTime := time.Now()

	// Get all active exchanges and their symbols
	exchanges := c.ccxtService.GetSupportedExchanges()
	if len(exchanges) == 0 {
		c.logger.Warn("No exchanges available for backfill")
		return nil
	}

	// Get dynamic concurrency limit from resource optimizer
	optimalConcurrency := c.resourceOptimizer.GetOptimalConcurrency()
	maxConcurrentBackfill := optimalConcurrency.MaxConcurrentBackfill

	// Create semaphore to limit concurrent exchanges
	semaphore := make(chan struct{}, maxConcurrentBackfill)
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Shared counters and metrics
	totalSymbols := 0
	successfulBackfills := 0
	successfulExchanges := 0
	failedExchanges := 0
	var totalProcessingTime time.Duration

	c.logger.WithFields(map[string]interface{}{
		"exchange_count": len(exchanges),
		"max_concurrent": maxConcurrentBackfill,
	}).Info("Processing exchanges concurrently")

	// Process exchanges concurrently
	for _, exchangeID := range exchanges {
		wg.Add(1)
		go func(exchange string) {
			defer wg.Done()

			// Acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			c.logger.WithFields(map[string]interface{}{"exchange": exchange}).Info("Starting backfill for exchange")

			// Get cached symbols for this exchange
			symbols, found := c.symbolCache.Get(exchange)
			if !found || len(symbols) == 0 {
				c.logger.WithFields(map[string]interface{}{"exchange": exchange}).Warn("No cached symbols found, skipping backfill")
				mu.Lock()
				failedExchanges++
				mu.Unlock()
				return
			}

			// Filter to valid symbols
			validSymbols := c.filterValidSymbols(symbols)
			if len(validSymbols) == 0 {
				c.logger.WithFields(map[string]interface{}{"exchange": exchange}).Warn("No valid symbols found, skipping backfill")
				mu.Lock()
				failedExchanges++
				mu.Unlock()
				return
			}

			// Limit symbols for backfill to prevent overwhelming the system
			maxSymbolsPerExchange := 20
			if len(validSymbols) > maxSymbolsPerExchange {
				validSymbols = validSymbols[:maxSymbolsPerExchange]
				c.logger.WithFields(map[string]interface{}{
					"exchange":      exchange,
					"limited_count": maxSymbolsPerExchange,
					"total_count":   len(symbols),
				}).Info("Limited backfill symbols")
			}

			c.logger.WithFields(map[string]interface{}{
				"exchange":     exchange,
				"symbol_count": len(validSymbols),
			}).Info("Starting backfill")

			// Track processing time for this exchange
			exchangeStartTime := time.Now()

			// Perform backfill for this exchange
			successCount, err := c.backfillExchangeData(exchange, validSymbols)
			exchangeProcessingTime := time.Since(exchangeStartTime)

			if err != nil {
				c.logger.WithFields(map[string]interface{}{"exchange": exchange}).WithError(err).Error("Error during backfill for exchange")
				mu.Lock()
				failedExchanges++
				mu.Unlock()
				return
			}

			// Update shared counters safely
			mu.Lock()
			totalSymbols += len(validSymbols)
			successfulBackfills += successCount
			successfulExchanges++
			totalProcessingTime += exchangeProcessingTime
			mu.Unlock()

			// Warm cache with successful backfill data
			if successCount > 0 {
				c.warmBackfillCache(exchange, validSymbols[:successCount])
			}

			c.logger.WithFields(map[string]interface{}{
				"exchange":           exchange,
				"successful_symbols": successCount,
				"total_symbols":      len(validSymbols),
				"duration":           exchangeProcessingTime,
				"symbols_per_sec":    float64(successCount) / exchangeProcessingTime.Seconds(),
			}).Info("Completed backfill for exchange")
		}(exchangeID)
	}

	// Wait for all exchanges to complete
	wg.Wait()

	// Calculate performance metrics
	totalBackfillTime := time.Since(backfillStartTime)
	avgProcessingTime := time.Duration(0)
	if successfulExchanges > 0 {
		avgProcessingTime = totalProcessingTime / time.Duration(successfulExchanges)
	}

	overallThroughput := float64(successfulBackfills) / totalBackfillTime.Seconds()
	successRate := float64(successfulBackfills) / float64(totalSymbols) * 100

	c.logger.WithFields(map[string]interface{}{
		"total_duration":                       totalBackfillTime,
		"successful_symbols":                   successfulBackfills,
		"total_symbols":                        totalSymbols,
		"success_rate_percent":                 successRate,
		"successful_exchanges":                 successfulExchanges,
		"failed_exchanges":                     failedExchanges,
		"total_exchanges":                      len(exchanges),
		"overall_throughput_symbols_per_sec":   overallThroughput,
		"avg_processing_time_per_exchange_sec": avgProcessingTime.Seconds(),
		"estimated_improvement_factor":         float64(len(exchanges) * 5),
	}).Info("Concurrent historical backfill completed")

	return nil
}

// warmBackfillCache warms the Redis cache with successful backfill data
func (c *CollectorService) warmBackfillCache(exchangeID string, symbols []string) {
	if c.redisClient == nil {
		return // Redis not available
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Cache the successful symbols for this exchange
	cacheKey := fmt.Sprintf("backfill:success:%s", exchangeID)
	symbolsJSON, err := json.Marshal(symbols)
	if err != nil {
		c.logger.WithError(err).Error("Failed to marshal symbols for cache warming")
		return
	}

	// Cache for 1 hour to help with subsequent operations
	err = c.redisClient.Set(ctx, cacheKey, symbolsJSON, time.Hour).Err()
	if err != nil {
		c.logger.WithFields(map[string]interface{}{
			"exchange": exchangeID,
		}).WithError(err).Error("Failed to warm backfill cache")
		return
	}

	// Also cache individual symbol status
	for _, symbol := range symbols {
		statusKey := fmt.Sprintf("backfill:status:%s:%s", exchangeID, symbol)
		c.redisClient.Set(ctx, statusKey, "completed", time.Hour)
	}

	c.logger.WithFields(map[string]interface{}{
		"symbol_count": len(symbols),
		"exchange":     exchangeID,
	}).Info("Warmed cache with successful backfill symbols")
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

// backfillExchangeData performs backfill for a specific exchange using worker pool pattern
func (c *CollectorService) backfillExchangeData(exchangeID string, symbols []string) (int, error) {
	backfillStartTime := time.Now().Add(-time.Duration(c.backfillConfig.BackfillHours) * time.Hour)

	// Create timeout context for the entire backfill operation
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Minute)
	defer cancel()

	// Register backfill operation with resource manager
	operationID := fmt.Sprintf("backfill_%s_%d", exchangeID, time.Now().Unix())
	c.resourceManager.RegisterResource(operationID, GoroutineResource, func() error {
		cancel()
		return nil
	}, map[string]interface{}{"exchange": exchangeID, "operation": "backfill", "symbol_count": len(symbols)})
	defer func() {
		if err := c.resourceManager.CleanupResource(operationID); err != nil {
			c.logger.Error("Failed to cleanup resource", "operation_id", operationID, "error", err)
		}
	}()

	c.logger.WithFields(map[string]interface{}{
		"exchange":     exchangeID,
		"symbol_count": len(symbols),
		"start_time":   backfillStartTime.Format("2006-01-02 15:04"),
		"end_time":     time.Now().Format("2006-01-02 15:04"),
	}).Info("Starting concurrent backfill for exchange")

	// Get dynamic concurrency limit from resource optimizer
	optimalConcurrency := c.resourceOptimizer.GetOptimalConcurrency()
	maxWorkers := optimalConcurrency.MaxConcurrentBackfill // Dynamic limit for concurrent symbol processing per exchange

	// Create worker pool with semaphore for symbol-level concurrency
	semaphore := make(chan struct{}, maxWorkers)
	jobChan := make(chan BackfillJob, len(symbols))
	resultChan := make(chan BackfillResult, len(symbols))

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobChan {
				// Check context cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Acquire semaphore slot
				semaphore <- struct{}{}

				// Process the job with error recovery
				var result BackfillResult
				err := c.errorRecoveryManager.ExecuteWithRetry(ctx, "backfill", func() error {
					result = c.processBackfillJob(job, workerID, ctx)
					if !result.Success {
						return result.Error
					}
					return nil
				})
				if err != nil {
					result.Success = false
					result.Error = err
				}
				resultChan <- result

				// Release semaphore slot
				<-semaphore
			}
		}(i)
	}

	// Send jobs to workers
	go func() {
		defer close(jobChan)
		for _, symbol := range symbols {
			select {
			case <-ctx.Done():
				return
			case jobChan <- BackfillJob{
				ExchangeID: exchangeID,
				Symbol:     symbol,
				StartTime:  backfillStartTime,
			}:
			}
		}
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Process results and track progress
	successCount := 0
	failedCount := 0
	processedCount := 0
	totalSymbols := len(symbols)
	var errors []error

	for result := range resultChan {
		processedCount++
		if result.Success {
			successCount++
		} else {
			failedCount++
			if result.Error != nil {
				errors = append(errors, fmt.Errorf("%s:%s - %w", result.ExchangeID, result.Symbol, result.Error))
			}
		}

		// Log progress every 25% or every 10 symbols
		if processedCount%10 == 0 || processedCount == totalSymbols {
			progress := float64(processedCount) / float64(totalSymbols) * 100
			c.logger.WithFields(map[string]interface{}{
				"exchange":         exchangeID,
				"progress_percent": progress,
				"processed":        processedCount,
				"total":            totalSymbols,
				"successful":       successCount,
				"failed":           failedCount,
			}).Info("Backfill progress")
		}
	}

	// Log any errors encountered
	if len(errors) > 0 {
		c.logger.WithFields(map[string]interface{}{
			"exchange":    exchangeID,
			"error_count": len(errors),
		}).Warn("Backfill errors encountered")
		for i, err := range errors {
			if i < 5 { // Log first 5 errors to avoid spam
				c.logger.WithFields(map[string]interface{}{
					"exchange": exchangeID,
				}).WithError(err).Error("Backfill error detail")
			} else if i == 5 {
				c.logger.WithFields(map[string]interface{}{
					"exchange":          exchangeID,
					"additional_errors": len(errors) - 5,
				}).Warn("Additional backfill errors suppressed")
				break
			}
		}
	}

	c.logger.WithFields(map[string]interface{}{
		"exchange":      exchangeID,
		"successful":    successCount,
		"failed":        failedCount,
		"total_symbols": totalSymbols,
	}).Info("Concurrent backfill completed for exchange")
	return successCount, nil
}

// processBackfillJob processes a single backfill job
func (c *CollectorService) processBackfillJob(job BackfillJob, workerID int, ctx context.Context) BackfillResult {
	result := BackfillResult{
		ExchangeID: job.ExchangeID,
		Symbol:     job.Symbol,
		Success:    false,
	}

	// Create timeout context for this job (inherit from parent context)
	jobCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Register operation with resource manager
	operationID := fmt.Sprintf("backfill-%s-%s-%d", job.ExchangeID, job.Symbol, workerID)
	c.resourceManager.RegisterResource(operationID, GoroutineResource, func() error {
		cancel()
		return nil
	}, map[string]interface{}{"exchange": job.ExchangeID, "symbol": job.Symbol, "worker_id": workerID, "operation": "backfill_job"})
	defer func() {
		if err := c.resourceManager.CleanupResource(operationID); err != nil {
			c.logger.Error("Failed to cleanup resource", "operation_id", operationID, "error", err)
		}
	}()

	// Check context cancellation
	select {
	case <-jobCtx.Done():
		result.Error = jobCtx.Err()
		return result
	default:
	}

	// Check if symbol is blacklisted
	symbolKey := fmt.Sprintf("%s:%s", job.ExchangeID, job.Symbol)
	if isBlacklisted, reason := c.blacklistCache.IsBlacklisted(symbolKey); isBlacklisted {
		c.logger.WithFields(map[string]interface{}{
			"worker_id": workerID,
			"symbol":    symbolKey,
			"reason":    reason,
		}).Info("Skipping blacklisted symbol")
		result.Error = fmt.Errorf("symbol blacklisted: %s", reason)
		return result
	}

	// Generate historical data points with error recovery
	err := c.errorRecoveryManager.ExecuteWithRetry(jobCtx, "api_call", func() error {
		return c.generateHistoricalDataPoints(jobCtx, job.ExchangeID, job.Symbol, job.StartTime)
	})

	if err != nil {
		c.logger.WithFields(map[string]interface{}{
			"worker_id": workerID,
			"exchange":  job.ExchangeID,
			"symbol":    job.Symbol,
		}).WithError(err).Error("Failed to backfill symbol")
		result.Error = err
		return result
	}

	result.Success = true
	return result
}

// generateHistoricalDataPoints creates synthetic historical data points for backfill
func (c *CollectorService) generateHistoricalDataPoints(ctx context.Context, exchangeID, symbol string, startTime time.Time) error {
	// Get current ticker data as baseline with circuit breaker
	var ticker *models.MarketPrice
	err := c.errorRecoveryManager.ExecuteWithRetry(ctx, "api_call", func() error {
		var fetchErr error
		var resp ccxt.MarketPriceInterface
		resp, fetchErr = c.ccxtService.FetchSingleTicker(ctx, exchangeID, symbol)
		if fetchErr != nil {
			return fetchErr
		}
		ticker = c.convertMarketPriceInterfaceToModel(resp)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to fetch current ticker for baseline: %w", err)
	}

	// Get exchange and trading pair IDs
	exchangeDBID, err := c.getOrCreateExchange(exchangeID)
	if err != nil {
		return fmt.Errorf("failed to get exchange ID: %w", err)
	}

	tradingPairID, err := c.getOrCreateTradingPair(exchangeDBID, symbol)
	if err != nil {
		return fmt.Errorf("failed to get trading pair ID: %w", err)
	}

	// Generate data points every 30 minutes for the backfill period
	interval := 30 * time.Minute
	currentTime := startTime
	basePrice := ticker.Price
	baseVolume := ticker.Volume
	dataPointsGenerated := 0

	c.logger.WithFields(map[string]interface{}{
		"exchange":        exchangeID,
		"symbol":          symbol,
		"start_time":      startTime.Format("2006-01-02 15:04"),
		"baseline_price":  basePrice,
		"baseline_volume": baseVolume,
	}).Info("Generating historical data")

	for currentTime.Before(time.Now().Add(-interval)) {
		// Add some realistic price variation (±2%)
		variation := decimal.NewFromFloat(0.98 + (0.04 * float64(time.Now().UnixNano()%100) / 100))
		historicalPrice := basePrice.Mul(variation)

		// Add some volume variation (±50%)
		volumeVariation := decimal.NewFromFloat(0.5 + (1.0 * float64(time.Now().UnixNano()%100) / 100))
		historicalVolume := baseVolume.Mul(volumeVariation)

		// Insert historical data point with error recovery
		err := c.errorRecoveryManager.ExecuteWithRetry(ctx, "database_operation", func() error {
			_, execErr := c.db.Exec(ctx,
				`INSERT INTO market_data (exchange_id, trading_pair_id, last_price, volume_24h, timestamp, created_at) 
				 VALUES ($1, $2, $3, $4, $5, $6)`,
				exchangeDBID, tradingPairID, historicalPrice, historicalVolume, currentTime, currentTime)
			return execErr
		})
		if err != nil {
			return fmt.Errorf("failed to insert historical data: %w", err)
		}

		dataPointsGenerated++
		currentTime = currentTime.Add(interval)
	}

	c.logger.WithFields(map[string]interface{}{
		"data_points": dataPointsGenerated,
		"exchange":    exchangeID,
		"symbol":      symbol,
		"interval":    "30min",
	}).Info("Generated historical data points")
	return nil
}
