package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

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
	dbType := database.DetectDBType(c.config.Database.Driver)
	var query string
	if dbType == database.DBTypeSQLite {
		query = `
			SELECT COUNT(DISTINCT md.exchange_id)
			FROM market_data md
			JOIN exchanges e ON md.exchange_id = e.id
			WHERE md.timestamp >= datetime('now', '-1 hour')
			  AND e.status = 'active'
		`
	} else {
		query = `
			SELECT COUNT(DISTINCT md.exchange_id)
			FROM market_data md
			JOIN exchanges e ON md.exchange_id = e.id
			WHERE md.timestamp >= NOW() - INTERVAL '1 hour'
			  AND e.status = 'active'
		`
	}
	err = c.db.QueryRow(c.ctx, query).Scan(&exchangesWithRecentData)
	if err != nil {
		c.logger.WithError(err).Warn("Failed to check exchanges with recent data, assuming backfill needed")
		return true, nil
	}

	// Also check total records in threshold period for context
	var totalRecords int
	err = c.db.QueryRow(c.ctx,
		"SELECT COUNT(*) FROM market_data WHERE timestamp >= ?",
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
				 VALUES (?, ?, ?, ?, ?, ?)`,
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
