package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/redis/go-redis/v9"

	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/logging"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/observability"
)

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
	// Pause/resume state per exchange
	pausedExchanges map[string]bool
	pauseMu         sync.RWMutex
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
		logger:          logger,
		pausedExchanges: make(map[string]bool),
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
	allExchanges := c.ccxtService.GetSupportedExchanges()

	if len(c.config.MarketData.Exchanges) > 0 {
		c.logger.WithFields(map[string]interface{}{
			"enabled_exchanges": c.config.MarketData.Exchanges,
		}).Info("Filtering exchanges by config")

		filtered := make([]string, 0)
		for _, ex := range allExchanges {
			for _, enabled := range c.config.MarketData.Exchanges {
				if ex == enabled {
					filtered = append(filtered, ex)
					break
				}
			}
		}
		if len(filtered) > 0 {
			allExchanges = filtered
		} else {
			c.logger.Warn("No enabled exchanges found in CCXT, using all exchanges")
		}
	}

	if len(allExchanges) == 0 {
		c.logger.Warn("No exchanges available from CCXT, skipping priority query")
		return allExchanges
	}

	if isNilDBPool(c.db) {
		c.logger.Warn("Database not available, returning filtered exchanges")
		return allExchanges
	}

	// Query database to get exchanges with their priorities
	// Use database-specific syntax for array parameter
	var query string
	var args []interface{}

	if database.DetectDBType(c.config.Database.Driver) == database.DBTypeSQLite {
		// For SQLite, build dynamic IN clause since SQLite doesn't support ANY(array)
		placeholders := make([]string, len(allExchanges))
		args = make([]interface{}, len(allExchanges))
		for i, ex := range allExchanges {
			placeholders[i] = "?"
			args[i] = ex
		}
		query = fmt.Sprintf(`
			SELECT e.name, e.priority, e.is_active, ce.ccxt_id
			FROM exchanges e
			LEFT JOIN ccxt_exchanges ce ON e.id = ce.exchange_id
			WHERE e.name IN (%s) AND e.is_active = true
			ORDER BY e.priority ASC, e.name ASC`, strings.Join(placeholders, ","))
	} else {
		// For PostgreSQL, use the original ANY syntax
		query = `
			SELECT e.name, e.priority, e.is_active, ce.ccxt_id
			FROM exchanges e
			LEFT JOIN ccxt_exchanges ce ON e.id = ce.exchange_id
			WHERE e.name = ANY($1) AND e.is_active = true
			ORDER BY e.priority ASC, e.name ASC`
		args = []interface{}{allExchanges}
	}

	rows, err := c.db.Query(c.ctx, query, args...)
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

	// Trigger immediate initial data collection to unblock dependent services
	// Only fetch a few symbols initially to quickly unblock the channel
	go func() {
		log.Printf("Triggering immediate initial data collection for %d workers", len(c.workers))
		for _, worker := range c.workers {
			// Fetch only first 10 symbols to quickly unblock dependent services
			initialSymbols := worker.Symbols
			if len(initialSymbols) > 10 {
				initialSymbols = initialSymbols[:10]
			}
			// Create a temporary worker with reduced symbol list
			tempWorker := &Worker{
				Exchange: worker.Exchange,
				Symbols:  initialSymbols,
			}
			if err := c.collectTickerDataBulk(tempWorker); err != nil {
				log.Printf("Initial collection failed for %s: %v", worker.Exchange, err)
			} else {
				log.Printf("Initial collection succeeded for %s (%d symbols)", worker.Exchange, len(initialSymbols))
			}
		}
	}()
}

// Stop gracefully stops all collection workers.
func (c *CollectorService) Stop() {
	c.logger.Info("Stopping market data collector service...")
	c.cancel()
	c.wg.Wait()
	c.logger.Info("Market data collector service stopped")
}

func (c *CollectorService) PauseExchange(exchangeID string) error {
	c.mu.Lock()
	worker, exists := c.workers[exchangeID]
	if !exists {
		c.mu.Unlock()
		return fmt.Errorf("worker for exchange %s not found", exchangeID)
	}
	worker.Paused = true

	c.pauseMu.Lock()
	c.pausedExchanges[exchangeID] = true
	c.pauseMu.Unlock()

	c.mu.Unlock()

	c.logger.WithFields(map[string]interface{}{"exchange": exchangeID}).Info("Exchange paused")
	return nil
}

func (c *CollectorService) ResumeExchange(exchangeID string) error {
	c.mu.Lock()

	c.pauseMu.RLock()
	paused := c.pausedExchanges[exchangeID]
	c.pauseMu.RUnlock()

	if !paused {
		c.mu.Unlock()
		return fmt.Errorf("exchange %s is not paused", exchangeID)
	}

	worker, exists := c.workers[exchangeID]
	if exists {
		worker.Paused = false
		worker.ErrorCount = 0
	}

	c.pauseMu.Lock()
	delete(c.pausedExchanges, exchangeID)
	c.pauseMu.Unlock()

	c.mu.Unlock()

	c.logger.WithFields(map[string]interface{}{"exchange": exchangeID}).Info("Exchange resumed")
	return nil
}

func (c *CollectorService) IsPaused(exchangeID string) bool {
	c.pauseMu.RLock()
	defer c.pauseMu.RUnlock()
	return c.pausedExchanges[exchangeID]
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
