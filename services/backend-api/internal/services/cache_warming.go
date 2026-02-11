package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
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
	c.logger.Info("Starting cache warming")
	start := time.Now()

	// Warm exchange configuration cache
	if err := c.warmExchangeConfig(ctx); err != nil {
		c.logger.Warn("Failed to warm exchange config cache", "error", err)
	}

	// Warm supported exchanges cache
	if err := c.warmSupportedExchanges(ctx); err != nil {
		c.logger.Warn("Failed to warm supported exchanges cache", "error", err)
	}

	// Warm trading pairs cache
	if err := c.warmTradingPairs(ctx); err != nil {
		c.logger.Warn("Failed to warm trading pairs cache", "error", err)
	}

	// Warm exchange data cache
	if err := c.warmExchanges(ctx); err != nil {
		c.logger.Warn("Failed to warm exchanges cache", "error", err)
	}

	// Warm funding rates cache
	if err := c.warmFundingRates(ctx); err != nil {
		c.logger.Warn("Failed to warm funding rates cache", "error", err)
	}

	duration := time.Since(start)
	c.logger.Info("Cache warming completed", "duration_ms", duration.Milliseconds())
	return nil
}

// warmExchangeConfig warms the exchange configuration cache
func (c *CacheWarmingService) warmExchangeConfig(ctx context.Context) error {
	c.logger.Info("Warming exchange configuration cache")

	// Check for nil dependencies
	if c.ccxtService == nil {
		return fmt.Errorf("ccxt service is nil")
	}
	if c.redisClient == nil {
		return fmt.Errorf("redis client is nil")
	}

	// Get exchange config from CCXT service
	config, err := c.ccxtService.GetExchangeConfig(ctx)
	if err != nil {
		return err
	}

	// Marshal and cache the config
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}

	// Cache with 1 hour TTL
	err = c.redisClient.Set(ctx, "exchange:config", configJSON, time.Hour).Err()
	if err != nil {
		return err
	}

	c.logger.Info("Exchange configuration cache warmed successfully")
	return nil
}

// warmSupportedExchanges warms the supported exchanges cache
func (c *CacheWarmingService) warmSupportedExchanges(ctx context.Context) error {
	c.logger.Info("Warming supported exchanges cache")

	// Check for nil dependencies
	if c.ccxtService == nil {
		return fmt.Errorf("ccxt service is nil")
	}
	if c.redisClient == nil {
		return fmt.Errorf("redis client is nil")
	}

	// Get supported exchanges from CCXT service
	exchanges := c.ccxtService.GetSupportedExchanges()

	// Marshal and cache the exchanges
	exchangesJSON, err := json.Marshal(exchanges)
	if err != nil {
		return err
	}

	// Cache with 30 minutes TTL
	err = c.redisClient.Set(ctx, "exchange:supported", exchangesJSON, 30*time.Minute).Err()
	if err != nil {
		return err
	}

	c.logger.Info("Supported exchanges cache warmed successfully", "count", len(exchanges))
	return nil
}

// warmTradingPairs warms the trading pairs cache
func (c *CacheWarmingService) warmTradingPairs(ctx context.Context) error {
	c.logger.Info("Warming trading pairs cache")

	// Check for nil dependencies
	if isNilDBPool(c.db) {
		return fmt.Errorf("database is nil")
	}
	if c.redisClient == nil {
		return fmt.Errorf("redis client is nil")
	}

	// Get all trading pairs from database
	query := `SELECT id, symbol, base_currency, quote_currency FROM trading_pairs LIMIT 1000`
	rows, err := c.db.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int
		var symbol, baseCurrency, quoteCurrency string
		if scanErr := rows.Scan(&id, &symbol, &baseCurrency, &quoteCurrency); scanErr != nil {
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
			continue
		}

		setErr := c.redisClient.Set(ctx, cacheKey, pairJSON, 24*time.Hour).Err()
		if setErr != nil {
			continue
		}

		count++
	}

	c.logger.Info("Trading pairs cache warmed successfully", "count", count)
	return nil
}

// warmExchanges warms the exchanges cache
func (c *CacheWarmingService) warmExchanges(ctx context.Context) error {
	c.logger.Info("Warming exchanges cache")

	// Check for nil dependencies
	if isNilDBPool(c.db) {
		return fmt.Errorf("database is nil")
	}
	if c.redisClient == nil {
		return fmt.Errorf("redis client is nil")
	}

	// Get all exchanges from database
	query := `SELECT id, name FROM exchanges LIMIT 100`
	rows, err := c.db.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int
		var name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			continue
		}

		// Cache exchange ID by name with 24 hour TTL
		cacheKey := "exchange:" + name
		setErr := c.redisClient.Set(ctx, cacheKey, id, 24*time.Hour).Err()
		if setErr != nil {
			continue
		}

		count++
	}

	c.logger.Info("Exchanges cache warmed successfully", "count", count)
	return nil
}

// warmFundingRates warms the funding rates cache
func (c *CacheWarmingService) warmFundingRates(ctx context.Context) error {
	c.logger.Info("Warming funding rates cache")

	// Check for nil dependencies
	if isNilDBPool(c.db) {
		return fmt.Errorf("database is nil")
	}
	if c.redisClient == nil {
		return fmt.Errorf("redis client is nil")
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
	for rows.Next() {
		var exchangeName, symbol string
		var fundingRate float64
		var nextFundingTime, timestamp time.Time

		if scanErr := rows.Scan(&exchangeName, &symbol, &fundingRate, &nextFundingTime, &timestamp); scanErr != nil {
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
			continue
		}

		setErr := c.redisClient.Set(ctx, cacheKey, fundingJSON, time.Minute).Err()
		if setErr != nil {
			continue
		}

		count++
	}

	c.logger.Info("Funding rates cache warmed successfully", "count", count)
	return nil
}
