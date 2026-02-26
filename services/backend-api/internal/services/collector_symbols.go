package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

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
	if len(exchanges) <= 1 {
		minExchanges = 1
	}

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

	// Check if we have enough successful exchanges for the configured topology.
	requiredSuccessful := 2
	if len(exchanges) <= 1 {
		requiredSuccessful = 1
	}
	if successfulExchanges < requiredSuccessful {
		return nil, fmt.Errorf("insufficient successful exchanges (%d), need at least %d", successfulExchanges, requiredSuccessful)
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
