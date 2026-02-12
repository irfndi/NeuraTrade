package services

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/logging"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

type FundingRateClient interface {
	GetFundingRates(ctx context.Context, exchange string, symbols []string) ([]ccxt.FundingRate, error)
}

// FundingRateCollector handles collection and storage of historical funding rate data.
type FundingRateCollector struct {
	db          DBPool
	redisClient *redis.Client
	ccxtClient  FundingRateClient
	config      *config.Config
	logger      logging.Logger

	// Service lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mu      sync.RWMutex

	// Configuration
	retentionDays      int           // How long to keep historical data
	collectionInterval time.Duration // How often to collect funding rates
	targetExchanges    []string      // Exchanges to collect from
}

// FundingRateCollectorConfig contains configuration for the collector.
type FundingRateCollectorConfig struct {
	RetentionDays      int
	CollectionInterval time.Duration
	TargetExchanges    []string
}

// NewFundingRateCollector creates a new funding rate collector.
func NewFundingRateCollector(
	db any,
	redisClient *redis.Client,
	ccxtClient FundingRateClient,
	cfg *config.Config,
	collectorCfg *FundingRateCollectorConfig,
	logger logging.Logger,
) *FundingRateCollector {
	var dbPool DBPool
	switch typed := db.(type) {
	case nil:
		dbPool = nil
	case DBPool:
		dbPool = typed
	case database.LegacyDBPool:
		dbPool = database.WrapLegacyDBPool(typed)
	case database.DatabasePool:
		dbPool = readOnlyDBPoolAdapter{pool: typed}
	case database.LegacyQuerier:
		dbPool = readOnlyDBPoolAdapter{pool: database.WrapLegacyQuerier(typed)}
	default:
		dbPool = nil
	}

	if collectorCfg == nil {
		collectorCfg = &FundingRateCollectorConfig{
			RetentionDays:      90,
			CollectionInterval: 15 * time.Minute,
			TargetExchanges:    []string{"binance", "bybit"},
		}
	}

	return &FundingRateCollector{
		db:                 dbPool,
		redisClient:        redisClient,
		ccxtClient:         ccxtClient,
		config:             cfg,
		retentionDays:      collectorCfg.RetentionDays,
		collectionInterval: collectorCfg.CollectionInterval,
		targetExchanges:    collectorCfg.TargetExchanges,
		logger:             logger,
	}
}

// Start begins the funding rate collection service.
func (c *FundingRateCollector) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("funding rate collector is already running")
	}

	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.running = true

	observability.AddBreadcrumb(c.ctx, "funding_rate_collector", fmt.Sprintf("Starting funding rate collector (interval: %v, exchanges: %v)", c.collectionInterval, c.targetExchanges), sentry.LevelInfo)

	c.wg.Add(1)
	go c.runCollector()

	c.wg.Add(1)
	go c.runCleanup()

	c.logger.WithFields(map[string]interface{}{
		"retention_days":      c.retentionDays,
		"collection_interval": c.collectionInterval,
		"target_exchanges":    c.targetExchanges,
	}).Info("Funding rate collector started")

	return nil
}

// Stop gracefully stops the funding rate collector.
func (c *FundingRateCollector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return
	}

	c.cancel()
	c.wg.Wait()
	c.running = false

	c.logger.Info("Funding rate collector stopped")
}

// runCollector runs the periodic funding rate collection.
func (c *FundingRateCollector) runCollector() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.collectionInterval)
	defer ticker.Stop()

	// Run initial collection immediately
	if err := c.collectFundingRates(c.ctx); err != nil {
		c.logger.WithError(err).Error("Initial funding rate collection failed")
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.collectFundingRates(c.ctx); err != nil {
				c.logger.WithError(err).Error("Funding rate collection failed")
			}
		}
	}
}

// runCleanup runs periodic cleanup of old data.
func (c *FundingRateCollector) runCleanup() {
	defer c.wg.Done()

	// Run cleanup every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.cleanupOldData(c.ctx); err != nil {
				c.logger.WithError(err).Error("Funding rate cleanup failed")
			}
		}
	}
}

// collectFundingRates collects funding rates from all target exchanges.
func (c *FundingRateCollector) collectFundingRates(ctx context.Context) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpMarketData, "FundingRateCollector.collectFundingRates", map[string]string{
		"target_exchanges": fmt.Sprintf("%v", c.targetExchanges),
	})
	defer observability.FinishSpan(span, nil)

	c.logger.Info("Starting funding rate collection")

	var totalCollected int
	for _, exchange := range c.targetExchanges {
		collected, err := c.collectExchangeFundingRates(spanCtx, exchange)
		if err != nil {
			observability.AddBreadcrumb(spanCtx, "funding_rate_collector", fmt.Sprintf("Failed to collect from %s: %v", exchange, err), sentry.LevelError)
			observability.AddBreadcrumb(spanCtx, "funding_rate_collector", fmt.Sprintf("Failed to collect from %s: %v", exchange, err), sentry.LevelError)
			c.logger.WithFields(map[string]interface{}{
				"exchange": exchange,
			}).WithError(err).Error("Failed to collect funding rates")
			continue
		}
		totalCollected += collected
	}

	observability.AddBreadcrumb(spanCtx, "funding_rate_collector", fmt.Sprintf("Collection completed: %d rates collected", totalCollected), sentry.LevelInfo)
	observability.AddBreadcrumb(spanCtx, "funding_rate_collector", fmt.Sprintf("Collection completed: %d rates collected", totalCollected), sentry.LevelInfo)
	c.logger.WithFields(map[string]interface{}{
		"total_collected": totalCollected,
	}).Info("Funding rate collection completed")

	return nil
}

// collectExchangeFundingRates collects funding rates for a specific exchange.
func (c *FundingRateCollector) collectExchangeFundingRates(ctx context.Context, exchange string) (int, error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpMarketData, "FundingRateCollector.collectExchangeFundingRates", map[string]string{
		"exchange": exchange,
	})
	defer observability.FinishSpan(span, nil)

	rates, err := c.ccxtClient.GetFundingRates(spanCtx, exchange, nil)
	if err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "collectExchangeFundingRates", map[string]interface{}{
			"exchange": exchange,
		})
		return 0, fmt.Errorf("failed to get funding rates from %s: %w", exchange, err)
	}

	if len(rates) == 0 {
		return 0, nil
	}

	// Store each funding rate
	stored := 0
	for _, rate := range rates {
		if err := c.storeFundingRate(spanCtx, exchange, &rate); err != nil {
			c.logger.WithFields(map[string]interface{}{
				"exchange": exchange,
				"symbol":   rate.Symbol,
			}).WithError(err).Warn("Failed to store funding rate")
			continue
		}
		stored++
	}

	return stored, nil
}

// storeFundingRate stores a single funding rate in the database.
func (c *FundingRateCollector) storeFundingRate(ctx context.Context, exchange string, rate *ccxt.FundingRate) error {
	if c.db == nil {
		return fmt.Errorf("database pool is not available")
	}

	query := `
		INSERT INTO funding_rate_history (
			symbol, exchange, funding_rate, funding_time, mark_price, index_price, collected_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (symbol, exchange, funding_time) DO UPDATE SET
			funding_rate = EXCLUDED.funding_rate,
			mark_price = EXCLUDED.mark_price,
			index_price = EXCLUDED.index_price,
			collected_at = EXCLUDED.collected_at
	`

	fundingTime := rate.FundingTimestamp.Time()
	if fundingTime.IsZero() {
		fundingTime = time.Now()
	}

	_, err := c.db.Exec(ctx, query,
		rate.Symbol,
		exchange,
		rate.FundingRate,
		fundingTime,
		rate.MarkPrice,
		rate.IndexPrice,
		time.Now(),
	)

	return err
}

// cleanupOldData removes funding rate data older than retention period.
func (c *FundingRateCollector) cleanupOldData(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("database pool is not available")
	}

	query := `
		DELETE FROM funding_rate_history
		WHERE collected_at < NOW() - $1::INTERVAL
	`

	interval := fmt.Sprintf("%d days", c.retentionDays)
	result, err := c.db.Exec(ctx, query, interval)
	if err != nil {
		return fmt.Errorf("failed to cleanup old data: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read cleanup result: %w", err)
	}
	if rowsAffected > 0 {
		c.logger.WithFields(map[string]interface{}{
			"rows_deleted":   rowsAffected,
			"retention_days": c.retentionDays,
		}).Info("Cleaned up old funding rate data")
	}

	return nil
}

// GetFundingRateStats calculates statistical analysis for a symbol's funding rates.
// Note: Forecasting models (e.g., ARIMA/GARCH) are intentionally not implemented; this uses rolling stats.
// See docs/architecture/ADVANCED_ANALYTICS.md.
func (c *FundingRateCollector) GetFundingRateStats(
	ctx context.Context,
	symbol string,
	exchange string,
) (*models.FundingRateStats, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database pool is not available")
	}

	// Get current rate
	currentRate, err := c.getCurrentFundingRate(ctx, symbol, exchange)
	if err != nil {
		return nil, err
	}

	// Get historical data for calculations
	history7d, err := c.getHistoricalRates(ctx, symbol, exchange, 7)
	if err != nil {
		return nil, err
	}

	history30d, err := c.getHistoricalRates(ctx, symbol, exchange, 30)
	if err != nil {
		return nil, err
	}

	stats := &models.FundingRateStats{
		Symbol:      symbol,
		Exchange:    exchange,
		CurrentRate: currentRate,
		DataPoints:  len(history30d),
		LastUpdated: time.Now(),
	}

	// Calculate 7-day statistics
	if len(history7d) > 0 {
		stats.AvgRate7d = c.calculateMean(history7d)
		stats.StdDev7d = c.calculateStdDev(history7d, stats.AvgRate7d)
		stats.MinRate7d, stats.MaxRate7d = c.calculateMinMax(history7d)
	}

	// Calculate 30-day statistics
	if len(history30d) > 0 {
		stats.AvgRate30d = c.calculateMean(history30d)
		stats.StdDev30d = c.calculateStdDev(history30d, stats.AvgRate30d)
	}

	// Calculate trend
	stats.TrendDirection, stats.TrendStrength = c.calculateTrend(history7d)

	// Calculate scores
	stats.VolatilityScore = c.calculateVolatilityScore(stats.StdDev30d)
	stats.StabilityScore = decimal.NewFromInt(100).Sub(stats.VolatilityScore)

	return stats, nil
}

// getCurrentFundingRate gets the most recent funding rate for a symbol.
func (c *FundingRateCollector) getCurrentFundingRate(
	ctx context.Context,
	symbol string,
	exchange string,
) (decimal.Decimal, error) {
	query := `
		SELECT funding_rate FROM funding_rate_history
		WHERE symbol = $1 AND exchange = $2
		ORDER BY funding_time DESC
		LIMIT 1
	`

	var rate decimal.Decimal
	err := c.db.QueryRow(ctx, query, symbol, exchange).Scan(&rate)
	if err != nil {
		return decimal.Zero, err
	}

	return rate, nil
}

// getHistoricalRates retrieves funding rates for the specified number of days.
func (c *FundingRateCollector) getHistoricalRates(
	ctx context.Context,
	symbol string,
	exchange string,
	days int,
) ([]decimal.Decimal, error) {
	query := `
		SELECT funding_rate FROM funding_rate_history
		WHERE symbol = $1 AND exchange = $2
		  AND funding_time > NOW() - $3::INTERVAL
		ORDER BY funding_time ASC
	`

	interval := fmt.Sprintf("%d days", days)
	rows, err := c.db.Query(ctx, query, symbol, exchange, interval)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rates []decimal.Decimal
	for rows.Next() {
		var rate decimal.Decimal
		if err := rows.Scan(&rate); err != nil {
			return nil, err
		}
		rates = append(rates, rate)
	}

	return rates, rows.Err()
}

// GetHistoricalVolatility calculates historical volatility for a symbol.
func (c *FundingRateCollector) GetHistoricalVolatility(
	ctx context.Context,
	symbol string,
	exchange string,
	days int,
) (decimal.Decimal, error) {
	rates, err := c.getHistoricalRates(ctx, symbol, exchange, days)
	if err != nil {
		return decimal.Zero, err
	}

	if len(rates) < 2 {
		return decimal.NewFromInt(50), nil // Default volatility
	}

	mean := c.calculateMean(rates)
	stdDev := c.calculateStdDev(rates, mean)

	// Normalize to 0-100 scale
	volatility := stdDev.Mul(decimal.NewFromInt(10000))
	if volatility.GreaterThan(decimal.NewFromInt(100)) {
		volatility = decimal.NewFromInt(100)
	}

	return volatility, nil
}

// calculateMean calculates the arithmetic mean of rates.
func (c *FundingRateCollector) calculateMean(rates []decimal.Decimal) decimal.Decimal {
	if len(rates) == 0 {
		return decimal.Zero
	}

	sum := decimal.Zero
	for _, rate := range rates {
		sum = sum.Add(rate)
	}

	return sum.Div(decimal.NewFromInt(int64(len(rates))))
}

// calculateStdDev calculates the standard deviation of rates.
func (c *FundingRateCollector) calculateStdDev(rates []decimal.Decimal, mean decimal.Decimal) decimal.Decimal {
	if len(rates) < 2 {
		return decimal.Zero
	}

	var sumSquares float64
	meanFloat, _ := mean.Float64()

	for _, rate := range rates {
		rateFloat, _ := rate.Float64()
		diff := rateFloat - meanFloat
		sumSquares += diff * diff
	}

	variance := sumSquares / float64(len(rates)-1)
	stdDev := math.Sqrt(variance)

	return decimal.NewFromFloat(stdDev)
}

// calculateMinMax returns the minimum and maximum rates.
func (c *FundingRateCollector) calculateMinMax(rates []decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	if len(rates) == 0 {
		return decimal.Zero, decimal.Zero
	}

	minRate := rates[0]
	maxRate := rates[0]

	for _, rate := range rates[1:] {
		if rate.LessThan(minRate) {
			minRate = rate
		}
		if rate.GreaterThan(maxRate) {
			maxRate = rate
		}
	}

	return minRate, maxRate
}

// calculateTrend calculates the trend direction and strength using linear regression.
func (c *FundingRateCollector) calculateTrend(rates []decimal.Decimal) (string, decimal.Decimal) {
	if len(rates) < 3 {
		return "stable", decimal.Zero
	}

	// Simple linear regression
	n := float64(len(rates))
	var sumX, sumY, sumXY, sumX2 float64

	for i, rate := range rates {
		x := float64(i)
		y, _ := rate.Float64()
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Calculate slope
	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return "stable", decimal.Zero
	}

	slope := (n*sumXY - sumX*sumY) / denominator

	// Determine trend direction and strength
	var direction string
	var strength decimal.Decimal

	absSlope := math.Abs(slope)
	strength = decimal.NewFromFloat(math.Min(absSlope*10000, 1.0))

	if slope > 0.00001 {
		direction = "increasing"
	} else if slope < -0.00001 {
		direction = "decreasing"
	} else {
		direction = "stable"
	}

	return direction, strength
}

// calculateVolatilityScore converts standard deviation to a 0-100 score.
func (c *FundingRateCollector) calculateVolatilityScore(stdDev decimal.Decimal) decimal.Decimal {
	// Higher std dev = higher volatility score
	// 0.001 std dev = 50, 0.01 = 100
	score := stdDev.Mul(decimal.NewFromInt(5000))
	if score.GreaterThan(decimal.NewFromInt(100)) {
		score = decimal.NewFromInt(100)
	}
	return score
}

// GetFundingRateHistory returns raw historical funding rates for analysis.
func (c *FundingRateCollector) GetFundingRateHistory(
	ctx context.Context,
	symbol string,
	exchange string,
	days int,
) ([]models.FundingRateHistoryPoint, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database pool is not available")
	}

	query := `
		SELECT funding_time, funding_rate, mark_price
		FROM funding_rate_history
		WHERE symbol = $1 AND exchange = $2
		  AND funding_time > NOW() - $3::INTERVAL
		ORDER BY funding_time ASC
	`

	interval := fmt.Sprintf("%d days", days)
	rows, err := c.db.Query(ctx, query, symbol, exchange, interval)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []models.FundingRateHistoryPoint
	for rows.Next() {
		var point models.FundingRateHistoryPoint
		if err := rows.Scan(&point.Timestamp, &point.FundingRate, &point.MarkPrice); err != nil {
			return nil, err
		}
		history = append(history, point)
	}

	return history, rows.Err()
}

// IsRunning returns whether the collector is currently running.
func (c *FundingRateCollector) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
