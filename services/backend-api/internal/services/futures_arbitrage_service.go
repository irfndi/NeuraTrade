package services

import (
	"context"
	"encoding/json"
	"fmt"

	// removed: "log"

	"sync"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// FuturesArbitrageService manages futures arbitrage opportunity calculation and storage.
type FuturesArbitrageService struct {
	db          DBPool
	redisClient *redis.Client
	calculator  *FuturesArbitrageCalculator
	config      *config.Config
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	running     bool
	mu          sync.RWMutex

	// Error recovery components
	errorRecoveryManager *ErrorRecoveryManager
	resourceManager      *ResourceManager
	performanceMonitor   *PerformanceMonitor
	logger               logging.Logger
}

// NewFuturesArbitrageService creates a new futures arbitrage service.
//
// Parameters:
//
//	db: Database connection.
//	redisClient: Redis client.
//	cfg: Configuration.
//	errorRecoveryManager: Error recovery manager.
//	resourceManager: Resource manager.
//	performanceMonitor: Performance monitor.
//
// Returns:
//
//	*FuturesArbitrageService: Initialized service.
func NewFuturesArbitrageService(
	db DBPool,
	redisClient *redis.Client,
	cfg *config.Config,
	errorRecoveryManager *ErrorRecoveryManager,
	resourceManager *ResourceManager,
	performanceMonitor *PerformanceMonitor,
	logger logging.Logger,
) *FuturesArbitrageService {
	return &FuturesArbitrageService{
		db:                   db,
		redisClient:          redisClient,
		calculator:           NewFuturesArbitrageCalculator(),
		config:               cfg,
		errorRecoveryManager: errorRecoveryManager,
		resourceManager:      resourceManager,
		performanceMonitor:   performanceMonitor,
		logger:               logger,
	}
}

// Start begins the futures arbitrage opportunity calculation service.
//
// Returns:
//
//	error: Error if service is already running.
func (s *FuturesArbitrageService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("futures arbitrage service is already running")
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true

	// Start the opportunity calculation worker
	s.wg.Add(1)
	go s.runOpportunityCalculator()

	s.logger.Info("Futures arbitrage service started")
	observability.AddBreadcrumb(s.ctx, "futures_arbitrage", "Futures arbitrage service started", sentry.LevelInfo)
	return nil
}

// Stop gracefully stops the futures arbitrage service.
func (s *FuturesArbitrageService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.cancel()
	s.wg.Wait()
	s.running = false

	s.logger.Info("Futures arbitrage service stopped")
}

// runOpportunityCalculator runs the periodic opportunity calculation
func (s *FuturesArbitrageService) runOpportunityCalculator() {
	defer s.wg.Done()

	// Register this operation with resource manager
	operationID := fmt.Sprintf("futures_arbitrage_calculator_%d", time.Now().UnixNano())
	s.resourceManager.RegisterResource(operationID, GoroutineResource, func() error {
		return nil
	}, map[string]interface{}{"operation": "periodic_calculation", "service": "futures_arbitrage"})
	defer func() {
		if err := s.resourceManager.CleanupResource(operationID); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"operation_id": operationID,
			}).WithError(err).Error("Failed to cleanup resource")
		}
	}()

	ticker := time.NewTicker(30 * time.Second) // Calculate every 30 seconds
	defer ticker.Stop()

	s.logger.Info("Futures arbitrage opportunity calculator started")

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Futures arbitrage opportunity calculator stopped")
			return
		case <-ticker.C:
			// Create timeout context for each calculation cycle
			calcCtx, calcCancel := context.WithTimeout(s.ctx, 25*time.Second)

			// Execute with error recovery
			err := s.errorRecoveryManager.ExecuteWithRetry(calcCtx, "calculate_opportunities", func() error {
				return s.calculateAndStoreOpportunities(calcCtx)
			})

			if err != nil {
				s.logger.WithError(err).Error("Error calculating opportunities")
				// Record failure in performance monitor
				if s.performanceMonitor != nil {
					metrics := s.performanceMonitor.GetApplicationMetrics().CollectorMetrics
					metrics.FailedCollections++
					s.performanceMonitor.UpdateCollectorMetrics(metrics)
				}
			}

			calcCancel()
		}
	}
}

// calculateAndStoreOpportunities fetches funding rates and calculates arbitrage opportunities
func (s *FuturesArbitrageService) calculateAndStoreOpportunities(ctx context.Context) error {
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpArbitrage, "FuturesArbitrageService.calculateAndStoreOpportunities")
	defer func() {
		observability.RecoverAndCapture(spanCtx, "calculateAndStoreOpportunities")
	}()

	s.logger.WithFields(map[string]interface{}{
		"operation": "arbitrage_detection",
		"service":   "futures_arbitrage",
	}).Info("Starting futures arbitrage opportunity calculation")
	observability.AddBreadcrumb(spanCtx, "arbitrage", "Starting opportunity calculation", sentry.LevelInfo)

	// Clean up expired opportunities first with error recovery
	err := s.errorRecoveryManager.ExecuteWithRetry(ctx, "cleanup_opportunities", func() error {
		return s.cleanupExpiredOpportunities(ctx)
	})
	if err != nil {
		s.logger.WithError(err).Warn("Failed to cleanup expired opportunities")
	}

	// Get latest funding rates grouped by symbol with error recovery
	var fundingRateMap map[string][]FundingRateData
	err = s.errorRecoveryManager.ExecuteWithRetry(ctx, "get_funding_rates", func() error {
		var retryErr error
		fundingRateMap, retryErr = s.getLatestFundingRates(ctx)
		return retryErr
	})
	if err != nil {
		s.logger.WithError(err).Error("Error getting funding rates")
		return fmt.Errorf("failed to get funding rates: %w", err)
	}

	if len(fundingRateMap) == 0 {
		s.logger.Info("No funding rates available for opportunity calculation")
		return nil
	}

	opportunitiesCalculated := 0
	opportunitiesStored := 0

	// Calculate opportunities for each symbol
	for symbol, exchangeRates := range fundingRateMap {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if len(exchangeRates) < 2 {
			continue // Need at least 2 exchanges for arbitrage
		}

		// Find all possible arbitrage pairs
		for i, longRate := range exchangeRates {
			for j, shortRate := range exchangeRates {
				if i == j {
					continue // Same exchange
				}

				// Calculate net funding rate (short - long)
				netFundingRate := shortRate.Rate.Sub(longRate.Rate)

				// Only consider profitable opportunities (positive net funding rate)
				if netFundingRate.LessThanOrEqual(decimal.NewFromFloat(0.0001)) {
					continue // Minimum 0.01% threshold
				}

				opportunitiesCalculated++

				// Create calculation input
				input := models.FuturesArbitrageCalculationInput{
					Symbol:           symbol,
					LongExchange:     longRate.Exchange,
					ShortExchange:    shortRate.Exchange,
					LongFundingRate:  longRate.Rate,
					ShortFundingRate: shortRate.Rate,
					LongMarkPrice:    longRate.MarkPrice,
					ShortMarkPrice:   shortRate.MarkPrice,
					FundingInterval:  8, // Default 8 hours
				}

				// Calculate opportunity
				opportunity, err := s.calculator.CalculateFuturesArbitrage(input)
				if err != nil {
					s.logger.WithFields(map[string]interface{}{
						"symbol":         symbol,
						"long_exchange":  longRate.Exchange,
						"short_exchange": shortRate.Exchange,
					}).WithError(err).Error("Failed to calculate opportunity")
					continue
				}

				// Only store active opportunities with error recovery
				if opportunity.IsActive {
					err := s.errorRecoveryManager.ExecuteWithRetry(ctx, "store_opportunity", func() error {
						return s.storeOpportunity(ctx, opportunity)
					})
					if err != nil {
						s.logger.WithFields(map[string]interface{}{
							"symbol":         symbol,
							"long_exchange":  longRate.Exchange,
							"short_exchange": shortRate.Exchange,
						}).WithError(err).Error("Failed to store opportunity")
						continue
					}
					opportunitiesStored++
				}
			}
		}
	}

	s.logger.WithFields(map[string]interface{}{
		"calculated_count": opportunitiesCalculated,
		"stored_count":     opportunitiesStored,
		"symbol_count":     len(fundingRateMap),
	}).Info("Opportunity calculation completed")

	span.SetData("opportunities_calculated", opportunitiesCalculated)
	span.SetData("opportunities_stored", opportunitiesStored)
	span.SetData("symbol_count", len(fundingRateMap))
	observability.FinishSpan(span, nil)

	return nil
}

// FundingRateData represents funding rate data for opportunity calculation
type FundingRateData struct {
	Exchange  string
	Symbol    string
	Rate      decimal.Decimal
	MarkPrice decimal.Decimal
	Timestamp time.Time
}

// getLatestFundingRates retrieves the latest funding rates grouped by symbol
func (s *FuturesArbitrageService) getLatestFundingRates(ctx context.Context) (map[string][]FundingRateData, error) {
	s.logger.WithFields(map[string]interface{}{"operation": "data_retrieval"}).Info("Getting latest funding rates")

	cacheKey := "funding_rates:latest:all"
	s.logger.WithFields(map[string]interface{}{"cache_key": cacheKey}).Debug("Using cache key")

	// Check Redis cache first
	if s.redisClient != nil {
		if cachedData, err := s.redisClient.Get(ctx, cacheKey).Result(); err == nil {
			var fundingRateMap map[string][]FundingRateData
			if json.Unmarshal([]byte(cachedData), &fundingRateMap) == nil {
				s.logger.WithFields(map[string]interface{}{"symbol_count": len(fundingRateMap)}).Debug("Cache hit")
				return fundingRateMap, nil
			}
		}
		s.logger.Debug("Cache miss - querying database")
	}

	query := `
		SELECT DISTINCT ON (e.name, tp.symbol) 
			e.name as exchange,
			tp.symbol,
			fr.funding_rate,
			fr.mark_price,
			fr.timestamp
		FROM funding_rates fr
		JOIN exchanges e ON fr.exchange_id = e.id
		JOIN trading_pairs tp ON fr.trading_pair_id = tp.id
		WHERE fr.timestamp > NOW() - INTERVAL '1 hour'
		  AND fr.funding_rate IS NOT NULL
		  AND fr.mark_price IS NOT NULL
		  AND fr.mark_price > 0
		ORDER BY e.name, tp.symbol, fr.timestamp DESC
	`

	if isNilDBPool(s.db) {
		return nil, fmt.Errorf("database pool is not available")
	}

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		s.logger.WithError(err).Error("Database query failed")
		return nil, fmt.Errorf("failed to query funding rates: %w", err)
	}
	defer rows.Close()

	fundingRateMap := make(map[string][]FundingRateData)

	for rows.Next() {
		var data FundingRateData
		if err := rows.Scan(
			&data.Exchange,
			&data.Symbol,
			&data.Rate,
			&data.MarkPrice,
			&data.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("failed to scan funding rate: %w", err)
		}

		fundingRateMap[data.Symbol] = append(fundingRateMap[data.Symbol], data)
	}

	if err := rows.Err(); err != nil {
		s.logger.WithError(err).Error("Error iterating funding rates")
		return nil, fmt.Errorf("error iterating funding rates: %w", err)
	}

	s.logger.WithFields(map[string]interface{}{"symbol_count": len(fundingRateMap)}).Debug("Retrieved symbols from database")

	// Cache the result with 1-minute TTL using error recovery
	if s.redisClient != nil {
		if jsonData, err := json.Marshal(fundingRateMap); err == nil {
			err := s.errorRecoveryManager.ExecuteWithRetry(ctx, "redis_set_funding_rates", func() error {
				return s.redisClient.Set(ctx, cacheKey, jsonData, time.Minute).Err()
			})
			if err != nil {
				s.logger.WithError(err).Error("Failed to cache funding rates")
			} else {
				s.logger.WithFields(map[string]interface{}{
					"symbol_count": len(fundingRateMap),
					"ttl_minutes":  1,
				}).Debug("Cached funding rates")
			}
		}
	}

	return fundingRateMap, nil
}

// storeOpportunity stores a calculated opportunity in the database
func (s *FuturesArbitrageService) storeOpportunity(ctx context.Context, opportunity *models.FuturesArbitrageOpportunity) error {
	if isNilDBPool(s.db) {
		return fmt.Errorf("database pool is not available")
	}

	s.logger.WithFields(map[string]interface{}{
		"symbol":           opportunity.Symbol,
		"long_exchange":    opportunity.LongExchange,
		"short_exchange":   opportunity.ShortExchange,
		"net_funding_rate": opportunity.NetFundingRate.String(),
		"apy":              opportunity.APY.String(),
		"active":           opportunity.IsActive,
	}).Debug("Storing opportunity")
	query := `
		INSERT INTO futures_arbitrage_opportunities (
			symbol, base_currency, quote_currency,
			long_exchange, short_exchange,
			long_funding_rate, short_funding_rate, net_funding_rate, funding_interval,
			long_mark_price, short_mark_price, price_difference, price_difference_percentage,
			hourly_rate, daily_rate, apy,
			estimated_profit_8h, estimated_profit_daily, estimated_profit_weekly, estimated_profit_monthly,
			risk_score, volatility_score, liquidity_score,
			recommended_position_size, max_leverage, recommended_leverage, stop_loss_percentage,
			min_position_size, max_position_size, optimal_position_size,
			detected_at, expires_at, next_funding_time, time_to_next_funding, is_active
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24, $25, $26, $27, $28, $29, $30,
			$31, $32, $33, $34, $35
		)
		ON CONFLICT (symbol, long_exchange, short_exchange) 
		DO UPDATE SET
			long_funding_rate = EXCLUDED.long_funding_rate,
			short_funding_rate = EXCLUDED.short_funding_rate,
			net_funding_rate = EXCLUDED.net_funding_rate,
			long_mark_price = EXCLUDED.long_mark_price,
			short_mark_price = EXCLUDED.short_mark_price,
			price_difference = EXCLUDED.price_difference,
			price_difference_percentage = EXCLUDED.price_difference_percentage,
			hourly_rate = EXCLUDED.hourly_rate,
			daily_rate = EXCLUDED.daily_rate,
			apy = EXCLUDED.apy,
			estimated_profit_8h = EXCLUDED.estimated_profit_8h,
			estimated_profit_daily = EXCLUDED.estimated_profit_daily,
			estimated_profit_weekly = EXCLUDED.estimated_profit_weekly,
			estimated_profit_monthly = EXCLUDED.estimated_profit_monthly,
			risk_score = EXCLUDED.risk_score,
			volatility_score = EXCLUDED.volatility_score,
			liquidity_score = EXCLUDED.liquidity_score,
			recommended_position_size = EXCLUDED.recommended_position_size,
			max_leverage = EXCLUDED.max_leverage,
			recommended_leverage = EXCLUDED.recommended_leverage,
			stop_loss_percentage = EXCLUDED.stop_loss_percentage,
			min_position_size = EXCLUDED.min_position_size,
			max_position_size = EXCLUDED.max_position_size,
			optimal_position_size = EXCLUDED.optimal_position_size,
			detected_at = EXCLUDED.detected_at,
			expires_at = EXCLUDED.expires_at,
			next_funding_time = EXCLUDED.next_funding_time,
			time_to_next_funding = EXCLUDED.time_to_next_funding,
			is_active = EXCLUDED.is_active
	`

	_, err := s.db.Exec(ctx, query,
		opportunity.Symbol, opportunity.BaseCurrency, opportunity.QuoteCurrency,
		opportunity.LongExchange, opportunity.ShortExchange,
		opportunity.LongFundingRate, opportunity.ShortFundingRate, opportunity.NetFundingRate, opportunity.FundingInterval,
		opportunity.LongMarkPrice, opportunity.ShortMarkPrice, opportunity.PriceDifference, opportunity.PriceDifferencePercentage,
		opportunity.HourlyRate, opportunity.DailyRate, opportunity.APY,
		opportunity.EstimatedProfit8h, opportunity.EstimatedProfitDaily, opportunity.EstimatedProfitWeekly, opportunity.EstimatedProfitMonthly,
		opportunity.RiskScore, opportunity.VolatilityScore, opportunity.LiquidityScore,
		opportunity.RecommendedPositionSize, opportunity.MaxLeverage, opportunity.RecommendedLeverage, opportunity.StopLossPercentage,
		opportunity.MinPositionSize, opportunity.MaxPositionSize, opportunity.OptimalPositionSize,
		opportunity.DetectedAt, opportunity.ExpiresAt, opportunity.NextFundingTime, opportunity.TimeToNextFunding, opportunity.IsActive,
	)

	if err != nil {
		s.logger.WithError(err).Error("Failed to store opportunity")
		return fmt.Errorf("failed to store opportunity: %w", err)
	}

	s.logger.Debug("Opportunity stored successfully")
	return nil
}

// cleanupExpiredOpportunities removes expired opportunities from the database
func (s *FuturesArbitrageService) cleanupExpiredOpportunities(ctx context.Context) error {
	if isNilDBPool(s.db) {
		return fmt.Errorf("database pool is not available")
	}

	s.logger.Info("Starting cleanup of expired opportunities")
	query := `
		DELETE FROM futures_arbitrage_opportunities 
		WHERE expires_at < NOW() OR detected_at < NOW() - INTERVAL '1 hour'
	`

	result, err := s.db.Exec(ctx, query)
	if err != nil {
		s.logger.WithError(err).Error("Failed to cleanup expired opportunities")
		return fmt.Errorf("failed to cleanup expired opportunities: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		telemetry.Logger().Error("Failed to get rows affected", "error", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	telemetry.Logger().Info("Cleanup completed successfully", "opportunities_cleaned", rowsAffected)

	if rowsAffected > 0 {
		s.logger.WithFields(map[string]interface{}{"count": rowsAffected}).Info("Cleaned up expired arbitrage opportunities")
	}

	return nil
}

// IsRunning returns whether the service is currently running
func (s *FuturesArbitrageService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
