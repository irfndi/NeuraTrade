package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/shopspring/decimal"
)

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
			c.mu.RLock()
			paused := worker.Paused
			c.mu.RUnlock()
			if paused {
				c.logger.WithFields(map[string]interface{}{"exchange": worker.Exchange}).Info("Worker paused, skipping collection cycle")
				continue
			}

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
				var partialErr *ccxt.PartialMarketDataError
				if errors.As(fetchErr, &partialErr) && len(partialErr.Data) > 0 {
					c.logger.WithFields(map[string]interface{}{
						"exchange":   worker.Exchange,
						"symbol_cnt": len(validSymbols),
						"partial":    len(partialErr.Data),
						"reason":     partialErr.Error(),
					}).WithError(fetchErr).Warn("Fetched partial market data within fallback limits")
					resp = partialErr.Data
				} else {
					return fetchErr
				}
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

	// Check if we need to signal first data collected
	needSignalFirstData := false
	c.readinessMu.RLock()
	if !c.hasCollectedData {
		needSignalFirstData = true
	}
	c.readinessMu.RUnlock()

	// Use goroutines for concurrent processing of ticker data
	for i, ticker := range marketData {
		go func(idx int, t models.MarketPrice) {
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
				// Signal first data collected (only once, first goroutine wins)
				if needSignalFirstData {
					c.readinessMu.Lock()
					if !c.hasCollectedData {
						c.hasCollectedData = true
						close(c.dataReadyChan)
						c.logger.WithFields(map[string]interface{}{
							"exchange": t.ExchangeName,
							"symbol":   t.Symbol,
							"price":    t.Price,
						}).Info("First market data saved successfully (bulk)")
					}
					c.readinessMu.Unlock()
				}
				successChan <- true
			}
		}(i, ticker)
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
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		 VALUES (?, ?, ?, ?, ?, ?)`,
		exchangeID, tradingPairID, ticker.Price, ticker.Volume, ticker.Timestamp, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save market data: %w", err)
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
