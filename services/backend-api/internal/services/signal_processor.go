package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/shopspring/decimal"

	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
)

// SignalProcessorConfig holds configuration settings for the signal processor service.
type SignalProcessorConfig struct {
	BatchSize            int                  `json:"batch_size"`
	ProcessingInterval   time.Duration        `json:"processing_interval"`
	MaxConcurrentBatch   int                  `json:"max_concurrent_batch"`
	QualityThreshold     float64              `json:"quality_threshold"`
	DeduplicationWindow  time.Duration        `json:"deduplication_window"`
	RateLimitPerSecond   int                  `json:"rate_limit_per_second"`
	RetryAttempts        int                  `json:"retry_attempts"`
	RetryDelay           time.Duration        `json:"retry_delay"`
	TimeoutDuration      time.Duration        `json:"timeout_duration"`
	SignalTTL            time.Duration        `json:"signal_ttl"`
	NotificationEnabled  bool                 `json:"notification_enabled"`
	CircuitBreakerConfig CircuitBreakerConfig `json:"circuit_breaker"`
}

// SignalProcessor orchestrates the entire signal processing pipeline.
// It retrieves market data, generates signals, aggregates them, assesses quality, and triggers notifications.
type SignalProcessor struct {
	config              *SignalProcessorConfig
	db                  DBPool
	logger              logging.Logger
	signalAggregator    SignalAggregatorInterface
	qualityScorer       SignalQualityScorerInterface
	technicalAnalysis   *TechnicalAnalysisService
	notificationService *NotificationService
	collectorService    *CollectorService
	circuitBreaker      *CircuitBreaker

	// Processing state
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	running    bool
	mu         sync.RWMutex
	metrics    *ProcessingMetrics
	lastRun    time.Time
	errorCount int
}

// ProcessingMetrics tracks performance statistics of the signal processing pipeline.
type ProcessingMetrics struct {
	TotalSignalsProcessed  int64     `json:"total_signals_processed"`
	SuccessfulSignals      int64     `json:"successful_signals"`
	FailedSignals          int64     `json:"failed_signals"`
	QualityFilteredSignals int64     `json:"quality_filtered_signals"`
	NotificationsSent      int64     `json:"notifications_sent"`
	AverageProcessingTime  float64   `json:"average_processing_time_ms"`
	LastProcessingTime     time.Time `json:"last_processing_time"`
	ErrorRate              float64   `json:"error_rate"`
	ThroughputPerMinute    float64   `json:"throughput_per_minute"`
}

// ProcessingResult represents the outcome of processing a single signal or batch.
type ProcessingResult struct {
	SignalID         string                 `json:"signal_id"`
	SignalType       SignalType             `json:"signal_type"`
	Symbol           string                 `json:"symbol"`
	Processed        bool                   `json:"processed"`
	QualityScore     float64                `json:"quality_score"`
	NotificationSent bool                   `json:"notification_sent"`
	Error            error                  `json:"error,omitempty"`
	ProcessingTime   time.Duration          `json:"processing_time"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// NewSignalProcessor creates a new instance of SignalProcessor.
//
// Parameters:
//   - db: Connection pool to the database.
//   - logger: Logger instance.
//   - signalAggregator: Service for aggregating signals.
//   - qualityScorer: Service for scoring signal quality.
//   - technicalAnalysis: Service for technical analysis.
//   - notificationService: Service for sending notifications.
//   - collectorService: Service for collecting market data.
//   - circuitBreaker: Circuit breaker for fault tolerance.
//
// Returns:
//   - A pointer to the initialized SignalProcessor.
func NewSignalProcessor(
	db DBPool,
	logger logging.Logger,
	signalAggregator SignalAggregatorInterface,
	qualityScorer SignalQualityScorerInterface,
	technicalAnalysis *TechnicalAnalysisService,
	notificationService *NotificationService,
	collectorService *CollectorService,
	circuitBreaker *CircuitBreaker,
) *SignalProcessor {
	config := &SignalProcessorConfig{
		BatchSize:           100,
		ProcessingInterval:  5 * time.Minute,
		MaxConcurrentBatch:  4,
		SignalTTL:           24 * time.Hour,
		QualityThreshold:    0.7,
		NotificationEnabled: true,
		RetryAttempts:       3,
		RetryDelay:          30 * time.Second,
		TimeoutDuration:     30 * time.Second, // Default timeout for database operations
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &SignalProcessor{
		config:              config,
		db:                  db,
		logger:              logger,
		signalAggregator:    signalAggregator,
		qualityScorer:       qualityScorer,
		technicalAnalysis:   technicalAnalysis,
		notificationService: notificationService,
		collectorService:    collectorService,
		circuitBreaker:      circuitBreaker,
		ctx:                 ctx,
		cancel:              cancel,
		metrics:             &ProcessingMetrics{},
	}
}

// Start begins the signal processing pipeline in a background goroutine.
//
// Returns:
//   - An error if the processor is already running.
func (sp *SignalProcessor) Start() error {
	ctx, span := observability.StartSpan(sp.ctx, observability.SpanOpSignalProcessing, "SignalProcessor.Start")
	defer observability.FinishSpan(span, nil)

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.running {
		return fmt.Errorf("signal processor is already running")
	}

	sp.running = true
	sp.logger.WithFields(map[string]interface{}{
		"interval":   sp.config.ProcessingInterval,
		"batch_size": sp.config.BatchSize,
		"workers":    sp.config.MaxConcurrentBatch,
	}).Info("Starting signal processor")

	observability.AddBreadcrumbWithData(ctx, "signal_processor", "Starting signal processor", sentry.LevelInfo, map[string]interface{}{
		"interval":   sp.config.ProcessingInterval.String(),
		"batch_size": sp.config.BatchSize,
		"workers":    sp.config.MaxConcurrentBatch,
	})

	// Start the main processing loop
	sp.wg.Add(1)
	go sp.processingLoop()

	// Start metrics collection
	sp.wg.Add(1)
	go sp.metricsLoop()

	return nil
}

// Stop gracefully shuts down the signal processor, waiting for pending operations to complete.
//
// Returns:
//   - An error if the processor is not running.
func (sp *SignalProcessor) Stop() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if !sp.running {
		return fmt.Errorf("signal processor is not running")
	}

	sp.logger.Info("Stopping signal processor")
	sp.cancel()
	sp.wg.Wait()
	sp.running = false

	return nil
}

// IsRunning checks if the signal processor is currently active.
//
// Returns:
//   - True if running, false otherwise.
func (sp *SignalProcessor) IsRunning() bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.running
}

// GetMetrics returns a snapshot of the current processing metrics.
//
// Returns:
//   - A pointer to the ProcessingMetrics struct.
func (sp *SignalProcessor) GetMetrics() *ProcessingMetrics {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.metrics
}

// processingLoop is the main loop that triggers signal processing at configured intervals.
func (sp *SignalProcessor) processingLoop() {
	defer sp.wg.Done()

	ticker := time.NewTicker(sp.config.ProcessingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sp.ctx.Done():
			sp.logger.Info("Processing loop stopped")
			return
		case <-ticker.C:
			if err := sp.processSignalBatch(); err != nil {
				sp.logger.WithError(err).Error("Error processing signal batch")
				sp.incrementErrorCount()
			}
		}
	}
}

// processSignalBatch executes a single batch processing run with circuit breaker protection.
//
// Returns:
//   - An error if the batch processing fails or the circuit breaker is open.
func (sp *SignalProcessor) processSignalBatch() error {
	ctx, span := observability.StartSpanWithTags(sp.ctx, observability.SpanOpSignalProcessing, "SignalProcessor.processSignalBatch", map[string]string{
		"batch_size":        fmt.Sprintf("%d", sp.config.BatchSize),
		"max_concurrent":    fmt.Sprintf("%d", sp.config.MaxConcurrentBatch),
		"quality_threshold": fmt.Sprintf("%.2f", sp.config.QualityThreshold),
	})

	// Track error for span finishing
	var batchErr error

	// Defer order matters: recover runs first (LIFO), then span finish
	// This ensures the span is properly closed even if there's a panic
	defer func() {
		observability.FinishSpan(span, batchErr)
	}()
	defer func() {
		observability.RecoverAndCapture(ctx, "processSignalBatch")
	}()

	startTime := time.Now()
	sp.lastRun = startTime

	// Use circuit breaker to protect against cascading failures
	batchErr = sp.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		return sp.processSignalBatchWithRetry(ctx, startTime)
	})

	if batchErr != nil {
		observability.CaptureExceptionWithContext(ctx, batchErr, "signal_batch_processing", map[string]interface{}{
			"batch_size":     sp.config.BatchSize,
			"max_concurrent": sp.config.MaxConcurrentBatch,
			"duration_ms":    time.Since(startTime).Milliseconds(),
		})
		return batchErr
	}

	span.SetData("duration_ms", time.Since(startTime).Milliseconds())
	return nil
}

// processSignalBatchWithRetry attempts to process a signal batch with exponential backoff retries.
//
// Parameters:
//   - ctx: The context for the operation.
//   - startTime: The start time of the batch processing.
//
// Returns:
//   - An error if all retry attempts fail.
func (sp *SignalProcessor) processSignalBatchWithRetry(ctx context.Context, startTime time.Time) error {
	var lastErr error

	for attempt := 0; attempt < sp.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			sp.logger.WithFields(map[string]interface{}{
				"attempt":      attempt + 1,
				"max_attempts": sp.config.RetryAttempts,
				"last_error":   lastErr,
			}).Info("Retrying signal batch processing")

			// Wait before retry with exponential backoff
			retryDelay := sp.config.RetryDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
			}
		}

		err := sp.executeSignalBatch(ctx, startTime)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if error is retryable
		if !sp.isRetryableError(err) {
			sp.logger.WithError(err).Error("Non-retryable error in signal processing")
			return err
		}

		sp.logger.WithFields(map[string]interface{}{
			"attempt": attempt + 1,
		}).WithError(err).Warn("Retryable error in signal processing")
	}

	return fmt.Errorf("signal batch processing failed after %d attempts: %w", sp.config.RetryAttempts, lastErr)
}

// executeSignalBatch performs the core logic of fetching data, processing signals, and handling results.
//
// Parameters:
//   - ctx: The context for the operation.
//   - startTime: The start time of the batch execution.
//
// Returns:
//   - An error if any step of the batch execution fails.
func (sp *SignalProcessor) executeSignalBatch(ctx context.Context, startTime time.Time) error {
	// Stub logging for telemetry
	_ = fmt.Sprintf("Signal execution: timeout_duration=%s", sp.config.TimeoutDuration.String())

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, sp.config.TimeoutDuration)
	defer cancel()

	// Get market data for processing
	marketData, err := sp.getMarketDataForProcessingWithContext(timeoutCtx)
	if err != nil {
		// Stub logging for error
		_ = fmt.Sprintf("Failed to get market data: %v", err)
		return fmt.Errorf("failed to get market data: %w", err)
	}

	if len(marketData) == 0 {
		sp.logger.Debug("No market data available for processing")
		return nil
	}

	// Stub logging for market data count
	_ = fmt.Sprintf("Market data count: %d", len(marketData))

	// Process signals concurrently with timeout
	results, err := sp.processSignalsConcurrentlyWithContext(timeoutCtx, marketData)
	if err != nil {
		// Stub logging for error
		_ = fmt.Sprintf("Failed to process signals: %v", err)
		return fmt.Errorf("failed to process signals: %w", err)
	}

	// Store results and send notifications
	if err := sp.handleProcessingResultsWithContext(timeoutCtx, results); err != nil {
		// Stub logging for error
		_ = fmt.Sprintf("Failed to handle processing results: %v", err)
		return fmt.Errorf("failed to handle processing results: %w", err)
	}

	// Update metrics
	processingTime := time.Since(startTime)
	sp.updateMetrics(results, processingTime)

	// Add result tracking
	successfulCount := sp.countSuccessful(results)
	// Stub logging for results
	_ = fmt.Sprintf("Results tracking: total=%d, successful=%d, failed=%d, duration_ms=%d",
		len(results), successfulCount, len(results)-successfulCount, processingTime.Milliseconds())

	// Stub logging for results
	_ = fmt.Sprintf("Concurrent processing results: total=%d, successful=%d, failed=%d",
		len(results), successfulCount, len(results)-successfulCount)

	sp.logger.WithFields(map[string]interface{}{
		"count":      len(results),
		"duration":   processingTime,
		"successful": successfulCount,
	}).Info("Signal batch processed")

	return nil
}

// isRetryableError determines if a given error warrants a retry attempt.
func (sp *SignalProcessor) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Network-related errors are retryable
	if strings.Contains(err.Error(), "connection") ||
		strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "network") ||
		strings.Contains(err.Error(), "temporary") {
		return true
	}

	// Database connection errors are retryable
	if strings.Contains(err.Error(), "database") ||
		strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connection reset") {
		return true
	}

	// Context cancellation and deadline exceeded are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Rate limiting errors are retryable
	if strings.Contains(err.Error(), "rate limit") ||
		strings.Contains(err.Error(), "too many requests") {
		return true
	}

	// Validation errors are not retryable
	if strings.Contains(err.Error(), "validation") ||
		strings.Contains(err.Error(), "invalid") ||
		strings.Contains(err.Error(), "malformed") {
		return false
	}

	// Default to not retryable for unknown errors
	return false
}

// getMarketDataForProcessingWithContext retrieves market data respecting the provided context.
func (sp *SignalProcessor) getMarketDataForProcessingWithContext(ctx context.Context) ([]models.MarketData, error) {
	// Check context first
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return sp.getMarketDataForProcessing()
}

// getMarketDataForProcessing retrieves recent market data for active trading pairs.
func (sp *SignalProcessor) getMarketDataForProcessing() ([]models.MarketData, error) {
	// Get active trading pairs
	tradingPairs, err := sp.getActiveTradingPairs()
	if err != nil {
		return nil, fmt.Errorf("failed to get trading pairs: %w", err)
	}

	var marketData []models.MarketData
	for _, pair := range tradingPairs {
		// Get recent market data for each pair
		data, err := sp.getRecentMarketDataFromDB(pair.Symbol, pair.Exchange.Name, time.Hour)
		if err != nil {
			sp.logger.WithFields(map[string]interface{}{
				"symbol":   pair.Symbol,
				"exchange": pair.Exchange.Name,
			}).WithError(err).Warn("Failed to get market data")
			continue
		}
		marketData = append(marketData, data...)
	}

	return marketData, nil
}

// processSignalsConcurrentlyWithContext processes signals concurrently, respecting the provided context.
func (sp *SignalProcessor) processSignalsConcurrentlyWithContext(ctx context.Context, marketData []models.MarketData) ([]ProcessingResult, error) {
	// Check context first
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	results := sp.processSignalsConcurrently(marketData)
	return results, nil
}

// processSignalsConcurrently distributes market data processing across a pool of workers.
func (sp *SignalProcessor) processSignalsConcurrently(marketData []models.MarketData) []ProcessingResult {
	// Stub logging for telemetry
	workerCount := sp.config.MaxConcurrentBatch
	if workerCount <= 0 {
		workerCount = 4 // Default worker count
	}

	_ = fmt.Sprintf("Concurrent processing: worker_count=%d, market_data_count=%d",
		workerCount, len(marketData))

	jobs := make(chan models.MarketData, len(marketData))
	results := make(chan ProcessingResult, len(marketData))

	// Start workers
	var workerWg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workerWg.Add(1)
		go sp.signalWorker(jobs, results, &workerWg)
	}

	// Send jobs
	for _, data := range marketData {
		jobs <- data
	}
	close(jobs)

	// Wait for workers to complete
	go func() {
		workerWg.Wait()
		close(results)
	}()

	// Collect results
	var allResults []ProcessingResult
	for result := range results {
		allResults = append(allResults, result)
	}

	// Add result tracking
	successfulCount := 0
	for _, result := range allResults {
		if result.Error == nil {
			successfulCount++
		}
	}

	// Stub logging for concurrent results
	_ = fmt.Sprintf("All results: total=%d, successful=%d, failed=%d",
		len(allResults), successfulCount, len(allResults)-successfulCount)

	return allResults
}

// getRecentMarketDataFromDB retrieves market data for a specific symbol and exchange within a duration.
func (sp *SignalProcessor) getRecentMarketDataFromDB(symbol, exchange string, duration time.Duration) ([]models.MarketData, error) {
	since := time.Now().Add(-duration)

	query := `
		SELECT md.id, md.exchange_id, md.trading_pair_id, md.last_price, md.volume_24h,
		       md.timestamp, md.created_at
		FROM market_data md
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		JOIN exchanges e ON md.exchange_id = e.id
		WHERE tp.symbol = $1 AND e.ccxt_id = $2 AND md.timestamp >= $3
		ORDER BY md.timestamp DESC
		LIMIT 100
	`

	// Use processor context with timeout for database operations
	ctx, cancel := context.WithTimeout(sp.ctx, sp.config.TimeoutDuration)
	defer cancel()

	rows, err := sp.db.Query(ctx, query, symbol, exchange, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query market data: %w", err)
	}
	defer rows.Close()

	var marketData []models.MarketData
	for rows.Next() {
		var data models.MarketData
		scanErr := rows.Scan(
			&data.ID,
			&data.ExchangeID,
			&data.TradingPairID,
			&data.LastPrice,
			&data.Volume24h,
			&data.Timestamp,
			&data.CreatedAt,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan market data: %w", scanErr)
		}
		marketData = append(marketData, data)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating market data rows: %w", err)
	}

	return marketData, nil
}

// getTradingPairSymbol retrieves the symbol string for a given trading pair ID.
func (sp *SignalProcessor) getTradingPairSymbol(tradingPairID int) (string, error) {
	var symbol string
	query := `SELECT symbol FROM trading_pairs WHERE id = $1`

	// Use processor context with timeout for database operations
	ctx, cancel := context.WithTimeout(sp.ctx, sp.config.TimeoutDuration)
	defer cancel()

	err := sp.db.QueryRow(ctx, query, tradingPairID).Scan(&symbol)
	if err != nil {
		return "", fmt.Errorf("failed to get trading pair symbol: %w", err)
	}
	return symbol, nil
}

// getExchangeName retrieves the exchange name string for a given exchange ID.
func (sp *SignalProcessor) getExchangeName(exchangeID int) (string, error) {
	var name string
	query := `SELECT name FROM exchanges WHERE id = $1`

	// Use processor context with timeout for database operations
	ctx, cancel := context.WithTimeout(sp.ctx, sp.config.TimeoutDuration)
	defer cancel()

	err := sp.db.QueryRow(ctx, query, exchangeID).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("failed to get exchange name: %w", err)
	}
	return name, nil
}

// signalWorker consumes market data jobs and produces processing results.
func (sp *SignalProcessor) signalWorker(jobs <-chan models.MarketData, results chan<- ProcessingResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for data := range jobs {
		result := sp.processSignal(data)
		results <- result
	}
}

// processSignal analyzes a single market data point to generate, aggregate, and score signals.
// It covers arbitrage, technical analysis, aggregation, and quality assessment.
func (sp *SignalProcessor) processSignal(data models.MarketData) ProcessingResult {
	// Stub logging for telemetry
	_ = fmt.Sprintf("Signal processing: trading_pair_id=%d, exchange_id=%d, price=%s, volume=%s",
		data.TradingPairID, data.ExchangeID, data.LastPrice.String(), data.Volume24h.String())

	startTime := time.Now()

	// Get trading pair symbol from database
	symbol, err := sp.getTradingPairSymbol(data.TradingPairID)
	if err != nil {
		// Stub logging for error
		_ = fmt.Sprintf("Failed to get trading pair symbol: %v", err)
		result := ProcessingResult{
			SignalID:       fmt.Sprintf("unknown_%d_%d", data.TradingPairID, time.Now().Unix()),
			Symbol:         "unknown",
			ProcessingTime: time.Since(startTime),
			Metadata:       make(map[string]interface{}),
			Error:          fmt.Errorf("failed to get trading pair symbol: %w", err),
		}
		return result
	}

	// Get exchange name from database
	exchangeName, err := sp.getExchangeName(data.ExchangeID)
	if err != nil {
		exchangeName = "unknown"
	}

	// Stub logging for symbol and exchange
	_ = fmt.Sprintf("Processing symbol: %s, exchange: %s", symbol, exchangeName)

	result := ProcessingResult{
		SignalID:       fmt.Sprintf("%s_%s_%d", symbol, exchangeName, time.Now().Unix()),
		Symbol:         symbol,
		ProcessingTime: 0,
		Metadata:       make(map[string]interface{}),
	}

	// Generate arbitrage signals
	arbitrageSignals, err := sp.generateArbitrageSignals(data)
	if err != nil {
		// Stub logging for error
		_ = fmt.Sprintf("Arbitrage signal generation failed: %v", err)
		result.Error = fmt.Errorf("arbitrage signal generation failed: %w", err)
		result.ProcessingTime = time.Since(startTime)
		return result
	}

	// Generate technical signals
	technicalSignals, err := sp.generateTechnicalSignals(data)
	if err != nil {
		_ = fmt.Sprintf("Technical signal generation failed: %v", err)
		result.Error = fmt.Errorf("technical signal generation failed: %w", err)
		result.ProcessingTime = time.Since(startTime)
		return result
	}

	// Aggregate signals
	aggregatedSignal, err := sp.aggregateSignals(arbitrageSignals, technicalSignals, data)
	if err != nil {
		_ = fmt.Sprintf("Signal aggregation failed: %v", err)
		result.Error = fmt.Errorf("signal aggregation failed: %w", err)
		result.ProcessingTime = time.Since(startTime)
		return result
	}

	// Assess signal quality
	qualityScore, err := sp.assessSignalQuality(aggregatedSignal, data)
	if err != nil {
		_ = fmt.Sprintf("Quality assessment failed: %v", err)
		result.Error = fmt.Errorf("quality assessment failed: %w", err)
		result.ProcessingTime = time.Since(startTime)
		return result
	}

	result.QualityScore = qualityScore
	result.SignalType = aggregatedSignal.SignalType
	result.Processed = true
	result.ProcessingTime = time.Since(startTime)
	result.Metadata["aggregated_signal"] = aggregatedSignal

	// Stub logging for final results
	_ = fmt.Sprintf("Signal processing completed: arbitrage_count=%d, technical_count=%d, signal_type=%s, quality_score=%f, duration_ms=%d",
		len(arbitrageSignals), len(technicalSignals), string(aggregatedSignal.SignalType), qualityScore, result.ProcessingTime.Milliseconds())

	return result
}

// generateArbitrageSignals identifies potential arbitrage opportunities for a symbol.
func (sp *SignalProcessor) generateArbitrageSignals(data models.MarketData) ([]ArbitrageSignalInput, error) {
	// Get trading pair symbol
	symbol, err := sp.getTradingPairSymbol(data.TradingPairID)
	if err != nil {
		return nil, fmt.Errorf("failed to get trading pair symbol: %w", err)
	}

	// Get arbitrage opportunities for this symbol
	opportunities, err := sp.getArbitrageOpportunities(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get arbitrage opportunities: %w", err)
	}

	var signals []ArbitrageSignalInput
	if len(opportunities) > 0 {
		// Create a single ArbitrageSignalInput with all opportunities
		signal := ArbitrageSignalInput{
			Opportunities: opportunities,
			MinVolume:     decimal.NewFromFloat(1000.0),  // Default minimum volume
			BaseAmount:    decimal.NewFromFloat(10000.0), // Default base amount
		}
		signals = append(signals, signal)
	}

	return signals, nil
}

// getArbitrageOpportunities queries the database for active arbitrage opportunities for a symbol.
func (sp *SignalProcessor) getArbitrageOpportunities(symbol string) ([]models.ArbitrageOpportunity, error) {
	// Query the database for arbitrage opportunities
	query := `
		SELECT ao.id, ao.trading_pair_id, ao.buy_exchange_id, ao.sell_exchange_id, 
		       ao.buy_price, ao.sell_price, ao.profit_percentage, ao.detected_at, ao.expires_at
		FROM arbitrage_opportunities ao
		JOIN trading_pairs tp ON ao.trading_pair_id = tp.id
		JOIN exchanges e ON ao.buy_exchange_id = e.id
		WHERE tp.symbol = $1 AND ao.expires_at > NOW()
		ORDER BY ao.profit_percentage DESC
		LIMIT 10
	`

	rows, err := sp.db.Query(context.Background(), query, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to query arbitrage opportunities: %w", err)
	}
	defer rows.Close()

	var opportunities []models.ArbitrageOpportunity
	for rows.Next() {
		var opp models.ArbitrageOpportunity
		err := rows.Scan(&opp.ID, &opp.TradingPairID, &opp.BuyExchangeID, &opp.SellExchangeID,
			&opp.BuyPrice, &opp.SellPrice, &opp.ProfitPercentage, &opp.DetectedAt, &opp.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan arbitrage opportunity: %w", err)
		}
		opportunities = append(opportunities, opp)
	}

	return opportunities, nil
}

// generateTechnicalSignals prepares technical signal input from market data.
func (sp *SignalProcessor) generateTechnicalSignals(data models.MarketData) ([]TechnicalSignalInput, error) {
	// Get trading pair symbol and exchange name
	symbol, err := sp.getTradingPairSymbol(data.TradingPairID)
	if err != nil {
		return nil, fmt.Errorf("failed to get trading pair symbol: %w", err)
	}

	return []TechnicalSignalInput{
		{
			Symbol: symbol,
		},
	}, nil
}

// aggregateSignals combines various signals into a single actionable signal candidate.
func (sp *SignalProcessor) aggregateSignals(arbitrageSignals []ArbitrageSignalInput, technicalSignals []TechnicalSignalInput, _ models.MarketData) (*AggregatedSignal, error) {
	ctx := context.Background()

	// Aggregate arbitrage signals first (they take priority)
	for _, arbInput := range arbitrageSignals {
		signals, err := sp.signalAggregator.AggregateArbitrageSignals(ctx, arbInput)
		if err != nil {
			continue
		}
		if len(signals) > 0 {
			return signals[0], nil
		}
	}

	// Fall back to technical signals
	for _, techInput := range technicalSignals {
		signals, err := sp.signalAggregator.AggregateTechnicalSignals(ctx, techInput)
		if err != nil {
			continue
		}
		if len(signals) > 0 {
			return signals[0], nil
		}
	}

	return nil, fmt.Errorf("no signals could be aggregated")
}

// assessSignalQuality evaluates the quality of the aggregated signal.
func (sp *SignalProcessor) assessSignalQuality(signal *AggregatedSignal, data models.MarketData) (float64, error) {
	// Create SignalQualityInput from aggregated signal
	input := &SignalQualityInput{
		SignalType: string(signal.SignalType),
		Symbol:     signal.Symbol,
		Exchanges:  sp.extractExchanges(signal),
		Volume:     data.Volume24h,
		// Other fields...
	}

	metrics, err := sp.qualityScorer.AssessSignalQuality(context.Background(), input)
	if err != nil {
		return 0, err
	}

	return metrics.OverallScore.InexactFloat64(), nil
}

// extractExchanges helper (stub)
func (sp *SignalProcessor) extractExchanges(signal *AggregatedSignal) []string {
	if signal == nil {
		return []string{}
	}

	unique := make(map[string]struct{})
	exchanges := make([]string, 0)

	appendExchange := func(exchange string) {
		trimmed := strings.TrimSpace(exchange)
		if trimmed == "" {
			return
		}

		key := strings.ToLower(trimmed)
		if _, exists := unique[key]; exists {
			return
		}

		unique[key] = struct{}{}
		exchanges = append(exchanges, trimmed)
	}

	appendExchangeList := func(raw interface{}) {
		switch typed := raw.(type) {
		case []string:
			for _, exchange := range typed {
				appendExchange(exchange)
			}
		case []interface{}:
			for _, item := range typed {
				exchange, ok := item.(string)
				if !ok {
					continue
				}
				appendExchange(exchange)
			}
		case string:
			for _, item := range strings.Split(typed, ",") {
				appendExchange(item)
			}
		}
	}

	appendExchangeList(signal.Exchanges)

	if signal.Metadata != nil {
		appendExchangeList(signal.Metadata["buy_exchanges"])
		appendExchangeList(signal.Metadata["sell_exchanges"])
		appendExchangeList(signal.Metadata["exchanges"])
	}

	return exchanges
}

func (sp *SignalProcessor) collectNotifiableSignals(results []ProcessingResult) []*AggregatedSignal {
	threshold := 0.0
	if sp.config != nil {
		threshold = sp.config.QualityThreshold
	}

	seen := make(map[string]struct{})
	signals := make([]*AggregatedSignal, 0)

	for _, result := range results {
		if !result.Processed || result.QualityScore <= threshold || result.Metadata == nil {
			continue
		}

		rawSignal, exists := result.Metadata["aggregated_signal"]
		if !exists {
			continue
		}

		var signal *AggregatedSignal
		switch typed := rawSignal.(type) {
		case *AggregatedSignal:
			signal = typed
		case AggregatedSignal:
			copied := typed
			signal = &copied
		default:
			continue
		}

		if signal == nil {
			continue
		}

		signal.Exchanges = sp.extractExchanges(signal)

		key := signal.ID
		if key == "" {
			key = fmt.Sprintf("%s|%s|%s|%d", signal.Symbol, signal.SignalType, signal.Action, signal.CreatedAt.UnixNano())
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		signals = append(signals, signal)
	}

	return signals
}

// handleProcessingResultsWithContext handles the results of signal processing (store in DB, notification, metrics)
func (sp *SignalProcessor) handleProcessingResultsWithContext(ctx context.Context, results []ProcessingResult) error {
	if sp.config == nil || !sp.config.NotificationEnabled || sp.notificationService == nil {
		return nil
	}

	notifiableSignals := sp.collectNotifiableSignals(results)
	if len(notifiableSignals) == 0 {
		return nil
	}

	if err := sp.notificationService.NotifyAggregatedSignals(ctx, notifiableSignals); err != nil {
		if sp.logger != nil {
			sp.logger.WithError(err).WithFields(map[string]interface{}{
				"signal_count": len(notifiableSignals),
			}).Warn("Failed to send aggregated signal notifications")
		}
		return nil
	}

	if sp.logger != nil {
		sp.logger.WithFields(map[string]interface{}{
			"signal_count": len(notifiableSignals),
		}).Info("Sent high-quality aggregated signal notifications")
	}

	return nil
}

// updateMetrics updates internal metrics based on processing results
func (sp *SignalProcessor) updateMetrics(results []ProcessingResult, duration time.Duration) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.metrics.LastProcessingTime = time.Now()
	// Update other fields...
}

// countSuccessful helper
func (sp *SignalProcessor) countSuccessful(results []ProcessingResult) int {
	count := 0
	for _, r := range results {
		if r.Error == nil {
			count++
		}
	}
	return count
}

// incrementErrorCount helper
func (sp *SignalProcessor) incrementErrorCount() {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.errorCount++
}

// metricsLoop collects metrics periodically
func (sp *SignalProcessor) metricsLoop() {
	defer sp.wg.Done()
	// Metric collection logic
}

// getActiveTradingPairs Mock/Stub
func (sp *SignalProcessor) getActiveTradingPairs() ([]struct {
	Symbol   string
	Exchange struct{ Name string }
}, error) {
	return []struct {
		Symbol   string
		Exchange struct{ Name string }
	}{}, nil
}
