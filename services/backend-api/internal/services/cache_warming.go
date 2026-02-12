package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/irfandi/celebrum-ai-go/internal/telemetry"
	"github.com/redis/go-redis/v9"
)

// CacheWarmingService handles cache warming on application startup.
type CacheWarmingService struct {
	redisClient *redis.Client
	ccxtService ccxt.CCXTService
	db          DBPool
	logger      *slog.Logger
}

// NewCacheWarmingService creates a new cache warming service.
//
// Parameters:
//
//	redisClient: Redis client.
//	ccxtService: CCXT service.
//	db: Database connection.
//
// Returns:
//
//	*CacheWarmingService: Initialized service.
func NewCacheWarmingService(redisClient *redis.Client, ccxtService ccxt.CCXTService, db DBPool) *CacheWarmingService {
	// Initialize logger with fallback for tests
	logger := telemetry.Logger()

	return &CacheWarmingService{
		redisClient: redisClient,
		ccxtService: ccxtService,
		db:          db,
		logger:      logger,
	}
}

// WarmCache performs cache warming for frequently accessed data.
// It populates exchange configs, supported exchanges, trading pairs, and funding rates.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	error: Error if warming fails (partially or fully).
func (c *CacheWarmingService) WarmCache(ctx context.Context) error {
	spanCtx, span := observability.StartSpan(ctx, "cache.warm", "CacheWarmingService.WarmCache")
	defer observability.FinishSpan(span, nil)

	c.logger.Info("Starting cache warming")
	observability.AddBreadcrumb(spanCtx, "cache_warming", "Starting cache warming", sentry.LevelInfo)
	start := time.Now()

	// Warm exchange configuration cache
	if err := c.warmExchangeConfig(spanCtx); err != nil {
		c.logger.Warn("Failed to warm exchange config cache", "error", err)
		observability.CaptureExceptionWithContext(spanCtx, err, "warm_exchange_config", map[string]interface{}{
			"step": "exchange_config",
		})
	}

	// Warm supported exchanges cache
	if err := c.warmSupportedExchanges(spanCtx); err != nil {
		c.logger.Warn("Failed to warm supported exchanges cache", "error", err)
		observability.CaptureExceptionWithContext(spanCtx, err, "warm_supported_exchanges", map[string]interface{}{
			"step": "supported_exchanges",
		})
	}

	// Warm trading pairs cache
	if err := c.warmTradingPairs(spanCtx); err != nil {
		c.logger.Warn("Failed to warm trading pairs cache", "error", err)
		observability.CaptureExceptionWithContext(spanCtx, err, "warm_trading_pairs", map[string]interface{}{
			"step": "trading_pairs",
		})
	}

	// Warm exchange data cache
	if err := c.warmExchanges(spanCtx); err != nil {
		c.logger.Warn("Failed to warm exchanges cache", "error", err)
		observability.CaptureExceptionWithContext(spanCtx, err, "warm_exchanges", map[string]interface{}{
			"step": "exchanges",
		})
	}

	// Warm funding rates cache
	if err := c.warmFundingRates(spanCtx); err != nil {
		c.logger.Warn("Failed to warm funding rates cache", "error", err)
		observability.CaptureExceptionWithContext(spanCtx, err, "warm_funding_rates", map[string]interface{}{
			"step": "funding_rates",
		})
	}

	duration := time.Since(start)
	c.logger.Info("Cache warming completed", "duration_ms", duration.Milliseconds())
	span.SetData("duration_ms", duration.Milliseconds())
	observability.AddBreadcrumbWithData(spanCtx, "cache_warming", "Cache warming completed", sentry.LevelInfo, map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
	})
	return nil
}

// warmExchangeConfig warms the exchange configuration cache with retry logic
func (c *CacheWarmingService) warmExchangeConfig(ctx context.Context) (err error) {
	spanCtx, span := observability.StartSpan(ctx, "cache.warm.exchange_config", "CacheWarmingService.warmExchangeConfig")
	defer func() {
		observability.FinishSpan(span, err)
	}()

	c.logger.Info("Warming exchange configuration cache")

	// Check for nil dependencies
	if c.ccxtService == nil {
		err = fmt.Errorf("ccxt service is nil")
		return err
	}
	if c.redisClient == nil {
		err = fmt.Errorf("redis client is nil")
		return err
	}

	// Retry logic for fetching exchange config
	const maxRetries = 3
	var config *ccxt.ExchangeConfigResponse
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		config, lastErr = c.ccxtService.GetExchangeConfig(spanCtx)
		if lastErr == nil {
			break
		}

		c.logger.Warn("Failed to get exchange config, retrying",
			"attempt", attempt,
			"max_retries", maxRetries,
			"error", lastErr)

		if attempt < maxRetries {
			// Exponential backoff: 1s, 2s, 4s
			backoffDuration := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoffDuration):
				// Continue to next attempt
			}
		}
	}

	if lastErr != nil {
		c.logger.Warn("Failed to warm exchange config cache after retries",
			"attempts", maxRetries,
			"error", lastErr)
		// Return nil to allow startup to continue (non-fatal)
		// The error is already logged and tracked
		return nil
	}

	// Marshal and cache the config
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}

	// Cache with 1 hour TTL
	err = c.redisClient.Set(spanCtx, "exchange:config", configJSON, time.Hour).Err()
	if err != nil {
		return err
	}

	c.logger.Info("Exchange configuration cache warmed successfully")
	return nil
}

// warmSupportedExchanges warms the supported exchanges cache
func (c *CacheWarmingService) warmSupportedExchanges(ctx context.Context) (err error) {
	spanCtx, span := observability.StartSpan(ctx, "cache.warm.supported_exchanges", "CacheWarmingService.warmSupportedExchanges")
	defer func() {
		observability.FinishSpan(span, err)
	}()

	c.logger.Info("Warming supported exchanges cache")

	// Check for nil dependencies
	if c.ccxtService == nil {
		err = fmt.Errorf("ccxt service is nil")
		return err
	}
	if c.redisClient == nil {
		err = fmt.Errorf("redis client is nil")
		return err
	}

	// Get supported exchanges from CCXT service
	exchanges := c.ccxtService.GetSupportedExchanges()

	// Marshal and cache the exchanges
	exchangesJSON, err := json.Marshal(exchanges)
	if err != nil {
		return err
	}

	// Cache with 30 minutes TTL
	err = c.redisClient.Set(spanCtx, "exchange:supported", exchangesJSON, 30*time.Minute).Err()
	if err != nil {
		return err
	}

	c.logger.Info("Supported exchanges cache warmed successfully", "count", len(exchanges))
	return nil
}

// warmTradingPairs warms the trading pairs cache
func (c *CacheWarmingService) warmTradingPairs(ctx context.Context) (err error) {
	spanCtx, span := observability.StartSpan(ctx, "cache.warm.trading_pairs", "CacheWarmingService.warmTradingPairs")
	defer func() {
		observability.FinishSpan(span, err)
	}()

	c.logger.Info("Warming trading pairs cache")

	// Check for nil dependencies
	if isNilDBPool(c.db) {
		return fmt.Errorf("database is nil")
	}
	if c.redisClient == nil {
		err = fmt.Errorf("redis client is nil")
		return err
	}

	// Get all trading pairs from database
	query := `SELECT id, symbol, base_currency, quote_currency FROM trading_pairs LIMIT 1000`
	rows, err := c.db.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	scanErrors := 0
	marshalErrors := 0
	setErrors := 0
	for rows.Next() {
		var id int
		var symbol, baseCurrency, quoteCurrency string
		if scanErr := rows.Scan(&id, &symbol, &baseCurrency, &quoteCurrency); scanErr != nil {
			scanErrors++
			continue
		}

		// Cache trading pair by symbol with 24 hour TTL
		cacheKey := "trading_pair:" + symbol
		pairData := map[string]interface{}{
			"id":             id,
			"symbol":         symbol,
			"base_currency":  baseCurrency,
			"quote_currency": quoteCurrency,
		}

		pairJSON, marshalErr := json.Marshal(pairData)
		if marshalErr != nil {
			marshalErrors++
			continue
		}

		setErr := c.redisClient.Set(spanCtx, cacheKey, pairJSON, 24*time.Hour).Err()
		if setErr != nil {
			setErrors++
			continue
		}

		count++
	}

	span.SetData("cached_count", count)
	span.SetData("scan_errors", scanErrors)
	span.SetData("marshal_errors", marshalErrors)
	span.SetData("set_errors", setErrors)
	if scanErrors > 0 || marshalErrors > 0 || setErrors > 0 {
		observability.AddBreadcrumbWithData(spanCtx, "cache_warming", "Trading pairs cache warming encountered errors", sentry.LevelWarning, map[string]interface{}{
			"scan_errors":    scanErrors,
			"marshal_errors": marshalErrors,
			"set_errors":     setErrors,
		})
	}
	c.logger.Info("Trading pairs cache warmed successfully", "count", count)
	return nil
}

// warmExchanges warms the exchanges cache
func (c *CacheWarmingService) warmExchanges(ctx context.Context) (err error) {
	spanCtx, span := observability.StartSpan(ctx, "cache.warm.exchanges", "CacheWarmingService.warmExchanges")
	defer func() {
		observability.FinishSpan(span, err)
	}()

	c.logger.Info("Warming exchanges cache")

	// Check for nil dependencies
	if isNilDBPool(c.db) {
		return fmt.Errorf("database is nil")
	}
	if c.redisClient == nil {
		err = fmt.Errorf("redis client is nil")
		return err
	}

	// Get all exchanges from database
	query := `SELECT id, name FROM exchanges LIMIT 100`
	rows, err := c.db.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	scanErrors := 0
	setErrors := 0
	for rows.Next() {
		var id int
		var name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			scanErrors++
			continue
		}

		// Cache exchange ID by name with 24 hour TTL
		cacheKey := "exchange:" + name
		setErr := c.redisClient.Set(spanCtx, cacheKey, id, 24*time.Hour).Err()
		if setErr != nil {
			setErrors++
			continue
		}

		count++
	}

	span.SetData("cached_count", count)
	span.SetData("scan_errors", scanErrors)
	span.SetData("set_errors", setErrors)
	if scanErrors > 0 || setErrors > 0 {
		observability.AddBreadcrumbWithData(spanCtx, "cache_warming", "Exchanges cache warming encountered errors", sentry.LevelWarning, map[string]interface{}{
			"scan_errors": scanErrors,
			"set_errors":  setErrors,
		})
	}
	c.logger.Info("Exchanges cache warmed successfully", "count", count)
	return nil
}

// warmFundingRates warms the funding rates cache
func (c *CacheWarmingService) warmFundingRates(ctx context.Context) (err error) {
	spanCtx, span := observability.StartSpan(ctx, "cache.warm.funding_rates", "CacheWarmingService.warmFundingRates")
	defer func() {
		observability.FinishSpan(span, err)
	}()

	c.logger.Info("Warming funding rates cache")

	// Check for nil dependencies
	if isNilDBPool(c.db) {
		return fmt.Errorf("database is nil")
	}
	if c.redisClient == nil {
		err = fmt.Errorf("redis client is nil")
		return err
	}

	// Get latest funding rates from database for each exchange-symbol combination
	query := `
		SELECT DISTINCT ON (e.name, tp.symbol) 
			e.name as exchange_name,
			tp.symbol,
			fr.funding_rate,
			fr.next_funding_time,
			fr.timestamp
		FROM funding_rates fr
		JOIN exchanges e ON fr.exchange_id = e.id
		JOIN trading_pairs tp ON fr.trading_pair_id = tp.id
		WHERE fr.timestamp > NOW() - INTERVAL '24 hours'
		ORDER BY e.name, tp.symbol, fr.timestamp DESC
		LIMIT 1000
	`

	rows, err := c.db.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	scanErrors := 0
	marshalErrors := 0
	setErrors := 0
	for rows.Next() {
		var exchangeName, symbol string
		var fundingRate float64
		var nextFundingTime, timestamp time.Time

		if scanErr := rows.Scan(&exchangeName, &symbol, &fundingRate, &nextFundingTime, &timestamp); scanErr != nil {
			scanErrors++
			continue
		}

		// Cache funding rate data with 1 minute TTL
		cacheKey := "funding_rate:" + exchangeName + ":" + symbol
		fundingData := map[string]interface{}{
			"exchange":          exchangeName,
			"symbol":            symbol,
			"funding_rate":      fundingRate,
			"next_funding_time": nextFundingTime,
			"timestamp":         timestamp,
		}

		fundingJSON, marshalErr := json.Marshal(fundingData)
		if marshalErr != nil {
			marshalErrors++
			continue
		}

		setErr := c.redisClient.Set(spanCtx, cacheKey, fundingJSON, time.Minute).Err()
		if setErr != nil {
			setErrors++
			continue
		}

		count++
	}

	span.SetData("cached_count", count)
	span.SetData("scan_errors", scanErrors)
	span.SetData("marshal_errors", marshalErrors)
	span.SetData("set_errors", setErrors)
	if scanErrors > 0 || marshalErrors > 0 || setErrors > 0 {
		observability.AddBreadcrumbWithData(spanCtx, "cache_warming", "Funding rates cache warming encountered errors", sentry.LevelWarning, map[string]interface{}{
			"scan_errors":    scanErrors,
			"marshal_errors": marshalErrors,
			"set_errors":     setErrors,
		})
	}
	c.logger.Info("Funding rates cache warmed successfully", "count", count)
	return nil
}
