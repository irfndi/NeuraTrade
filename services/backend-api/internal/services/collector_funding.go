package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
)

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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (exchange_id, trading_pair_id, funding_time)
		 DO UPDATE SET
			funding_rate = EXCLUDED.funding_rate,
			next_funding_time = EXCLUDED.next_funding_time,
			mark_price = EXCLUDED.mark_price,
			index_price = EXCLUDED.index_price,
			timestamp = EXCLUDED.timestamp,
			updated_at = CURRENT_TIMESTAMP`,
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
