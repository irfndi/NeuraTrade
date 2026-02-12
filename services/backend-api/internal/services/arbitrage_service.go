package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
)

// ArbitrageCalculator defines the interface for calculating arbitrage opportunities.
type ArbitrageCalculator interface {
	// CalculateArbitrageOpportunities finds opportunities from market data.
	CalculateArbitrageOpportunities(ctx context.Context, marketData map[string][]models.MarketData) ([]models.ArbitrageOpportunity, error)
}

// SpotArbitrageCalculator implements arbitrage calculations for spot markets.
type SpotArbitrageCalculator struct{}

// NewSpotArbitrageCalculator creates a new spot arbitrage calculator.
//
// Returns:
//
//	*SpotArbitrageCalculator: Initialized calculator.
func NewSpotArbitrageCalculator() *SpotArbitrageCalculator {
	return &SpotArbitrageCalculator{}
}

// CalculateArbitrageOpportunities calculates arbitrage opportunities from market data.
// It compares prices across exchanges to find profitable trades.
//
// Parameters:
//
//	ctx: Context.
//	marketData: Market data map.
//
// Returns:
//
//	[]models.ArbitrageOpportunity: List of opportunities.
//	error: Error if calculation fails.
func (calc *SpotArbitrageCalculator) CalculateArbitrageOpportunities(ctx context.Context, marketData map[string][]models.MarketData) ([]models.ArbitrageOpportunity, error) {
	var opportunities []models.ArbitrageOpportunity

	// For each symbol, find arbitrage opportunities between exchanges
	for _, exchangeData := range marketData {
		if len(exchangeData) < 2 {
			continue
		}

		// Find the lowest and highest prices for this symbol
		var lowestPrice, highestPrice decimal.Decimal
		var lowestExchange, highestExchange *models.Exchange
		var lowestPair *models.TradingPair

		for _, data := range exchangeData {
			// Skip data with nil trading pair
			if data.TradingPair == nil {
				continue
			}

			if lowestExchange == nil || data.LastPrice.LessThan(lowestPrice) {
				lowestPrice = data.LastPrice
				lowestExchange = data.Exchange
				lowestPair = data.TradingPair
			}

			if highestExchange == nil || data.LastPrice.GreaterThan(highestPrice) {
				highestPrice = data.LastPrice
				highestExchange = data.Exchange
			}
		}

		// Calculate profit percentage
		if !lowestPrice.IsZero() {
			profitPercentage := highestPrice.Sub(lowestPrice).Div(lowestPrice).Mul(decimal.NewFromInt(100))

			// Only consider opportunities with meaningful profit
			if profitPercentage.GreaterThan(decimal.NewFromFloat(0.1)) {
				opportunity := models.ArbitrageOpportunity{
					ID:               uuid.New().String(),
					BuyExchangeID:    lowestExchange.ID,
					SellExchangeID:   highestExchange.ID,
					TradingPairID:    lowestPair.ID,
					BuyPrice:         lowestPrice,
					SellPrice:        highestPrice,
					ProfitPercentage: profitPercentage,
					DetectedAt:       time.Now(),
					ExpiresAt:        time.Now().Add(5 * time.Minute),
					BuyExchange:      lowestExchange,
					SellExchange:     highestExchange,
					TradingPair:      lowestPair,
				}
				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// ArbitrageServiceConfig holds configuration for the arbitrage service.
type ArbitrageServiceConfig struct {
	// IntervalSeconds is the interval between checks.
	IntervalSeconds int `mapstructure:"interval_seconds"`
	// MinProfit is the minimum profit threshold.
	MinProfit float64 `mapstructure:"min_profit"`
	// MaxAgeMinutes is the max age of data.
	MaxAgeMinutes int `mapstructure:"max_age_minutes"`
	// BatchSize is the batch processing size.
	BatchSize int `mapstructure:"batch_size"`
	// Enabled indicates if the service is enabled.
	Enabled bool `mapstructure:"enabled"`
}

// ArbitrageService handles periodic calculation and storage of arbitrage opportunities.
type ArbitrageService struct {
	db                 DBPool
	config             *config.Config
	arbitrageConfig    ArbitrageServiceConfig
	calculator         ArbitrageCalculator
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	isRunning          bool
	mu                 sync.RWMutex
	logger             *zaplogrus.Logger
	lastCalculation    time.Time
	opportunitiesFound int
	multiLegCalculator *MultiLegArbitrageCalculator
}

type readOnlyDBPoolAdapter struct {
	pool database.DatabasePool
}

func (r readOnlyDBPoolAdapter) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	return r.pool.Query(ctx, query, args...)
}

func (r readOnlyDBPoolAdapter) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return r.pool.QueryRow(ctx, query, args...)
}

func (r readOnlyDBPoolAdapter) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	return r.pool.Exec(ctx, query, args...)
}

func (r readOnlyDBPoolAdapter) Begin(context.Context) (database.Tx, error) {
	return nil, fmt.Errorf("begin transaction is not supported by this database adapter")
}

// NewArbitrageService creates a new arbitrage service instance.
//
// Parameters:
//
//	db: Database connection.
//	cfg: Configuration.
//	calculator: Arbitrage calculator.
//
// Returns:
//
//	*ArbitrageService: Initialized service.
func NewArbitrageService(db any, cfg *config.Config, calculator ArbitrageCalculator, multiLegCalculator ...*MultiLegArbitrageCalculator) *ArbitrageService {
	ctx, cancel := context.WithCancel(context.Background())

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
	default:
		dbPool = nil
	}

	// Parse configuration with defaults
	arbitrageConfig := ArbitrageServiceConfig{
		IntervalSeconds: 60,  // 1 minute default
		MinProfit:       0.5, // 0.5% minimum profit
		MaxAgeMinutes:   30,  // 30 minutes max age for opportunities
		BatchSize:       100, // Process 100 trading pairs at a time
		Enabled:         true,
	}

	// Override with config values if available
	if cfg.Arbitrage.IntervalSeconds > 0 {
		arbitrageConfig.IntervalSeconds = cfg.Arbitrage.IntervalSeconds
	}
	if cfg.Arbitrage.MinProfitThreshold > 0 {
		arbitrageConfig.MinProfit = cfg.Arbitrage.MinProfitThreshold
	}
	if cfg.Arbitrage.MaxAgeMinutes > 0 {
		arbitrageConfig.MaxAgeMinutes = cfg.Arbitrage.MaxAgeMinutes
	}
	if cfg.Arbitrage.BatchSize > 0 {
		arbitrageConfig.BatchSize = cfg.Arbitrage.BatchSize
	}
	arbitrageConfig.Enabled = cfg.Arbitrage.Enabled

	// Initialize logger
	logger := zaplogrus.New()
	selectedMultiLegCalculator := NewMultiLegArbitrageCalculator(nil, decimal.NewFromFloat(0.001))
	if len(multiLegCalculator) > 0 && multiLegCalculator[0] != nil {
		selectedMultiLegCalculator = multiLegCalculator[0]
	}

	return &ArbitrageService{
		db:                 dbPool,
		config:             cfg,
		arbitrageConfig:    arbitrageConfig,
		calculator:         calculator,
		multiLegCalculator: selectedMultiLegCalculator,
		ctx:                ctx,
		cancel:             cancel,
		logger:             logger,
	}
}

// Start begins the periodic arbitrage calculation.
//
// Returns:
//
//	error: Error if service fails to start.
func (s *ArbitrageService) Start() error {
	if !s.arbitrageConfig.Enabled {
		s.logger.Info("Arbitrage service is disabled in configuration")
		return nil
	}

	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return fmt.Errorf("arbitrage service is already running")
	}
	s.isRunning = true
	s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"interval_seconds": s.arbitrageConfig.IntervalSeconds,
		"min_profit":       s.arbitrageConfig.MinProfit,
		"max_age_minutes":  s.arbitrageConfig.MaxAgeMinutes,
		"batch_size":       s.arbitrageConfig.BatchSize,
	}).Info("Starting arbitrage service")

	// Start the main calculation loop
	s.wg.Add(1)
	go s.calculationLoop()

	return nil
}

// Stop gracefully shuts down the arbitrage service.
func (s *ArbitrageService) Stop() {
	s.mu.Lock()
	if !s.isRunning {
		s.mu.Unlock()
		return
	}
	s.isRunning = false
	s.mu.Unlock()

	s.logger.Info("Stopping arbitrage service")
	s.cancel()
	s.wg.Wait()
	s.logger.Info("Arbitrage service stopped")
}

// IsRunning returns true if the service is currently running.
//
// Returns:
//
//	bool: True if running.
func (s *ArbitrageService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

// GetStatus returns the current status of the arbitrage service.
//
// Returns:
//
//	bool: Running status.
//	time.Time: Last calculation time.
//	int: Number of opportunities found.
func (s *ArbitrageService) GetStatus() (bool, time.Time, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning, s.lastCalculation, s.opportunitiesFound
}

// calculationLoop runs the periodic arbitrage calculation
func (s *ArbitrageService) calculationLoop() {
	// Only call Done() if Add() was called (prevents negative WaitGroup counter)
	if s.isRunning {
		defer s.wg.Done()
	}

	// Perform initial calculation immediately
	if err := s.calculateAndStoreOpportunities(); err != nil {
		s.logger.WithError(err).Error("Initial arbitrage calculation failed")
	}

	ticker := time.NewTicker(time.Duration(s.arbitrageConfig.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.calculateAndStoreOpportunities(); err != nil {
				s.logger.WithError(err).Error("Arbitrage calculation failed")
			}
		}
	}
}

// calculateAndStoreOpportunities fetches market data, calculates arbitrage opportunities, and stores them
func (s *ArbitrageService) calculateAndStoreOpportunities() error {
	startTime := time.Now()
	s.logger.Info("Starting arbitrage opportunity calculation")

	// Start Sentry span for the entire calculation cycle
	ctx, span := observability.StartSpan(s.ctx, observability.SpanOpArbitrage, "arbitrage_calculation_cycle")
	defer observability.FinishSpan(span, nil)

	span.SetTag("service", "arbitrage")
	span.SetData("min_profit_threshold", s.arbitrageConfig.MinProfit)

	// Add breadcrumb for tracking
	observability.AddBreadcrumb(ctx, "arbitrage", "Starting arbitrage calculation cycle", sentry.LevelInfo)

	// Step 1: Clean up old opportunities
	_, cleanupSpan := observability.StartSpan(ctx, "arbitrage.cleanup", "cleanup_old_opportunities")
	if err := s.cleanupOldOpportunities(); err != nil {
		s.logger.WithError(err).Warn("Failed to cleanup old opportunities")
		observability.AddBreadcrumb(ctx, "arbitrage", "Cleanup warning: "+err.Error(), sentry.LevelWarning)
	}
	observability.FinishSpan(cleanupSpan, nil)

	// Step 2: Get latest market data for all exchanges
	_, dataSpan := observability.StartSpan(ctx, observability.SpanOpMarketData, "get_market_data")
	marketData, err := s.getLatestMarketData()
	if err != nil {
		observability.FinishSpan(dataSpan, err)
		observability.CaptureExceptionWithContext(ctx, err, "get_market_data", map[string]interface{}{
			"service": "arbitrage",
		})
		return fmt.Errorf("failed to get market data: %w", err)
	}
	dataSpan.SetData("exchanges_count", len(marketData))
	dataSpan.SetData("total_pairs", s.countTotalTradingPairs(marketData))
	observability.FinishSpan(dataSpan, nil)

	if len(marketData) == 0 {
		s.logger.Warn("No market data available for arbitrage calculation")
		span.SetTag("result", "no_data")
		observability.AddBreadcrumb(ctx, "arbitrage", "No market data available", sentry.LevelWarning)
		return nil
	}

	s.logger.WithFields(map[string]interface{}{
		"exchanges":   len(marketData),
		"total_pairs": s.countTotalTradingPairs(marketData),
	}).Info("Retrieved market data")

	// Step 3: Calculate arbitrage opportunities
	_, calcSpan := observability.StartSpan(ctx, observability.SpanOpArbitrage, "calculate_opportunities")
	opportunities, err := s.calculator.CalculateArbitrageOpportunities(s.ctx, marketData)
	if err != nil {
		observability.FinishSpan(calcSpan, err)
		observability.CaptureExceptionWithContext(ctx, err, "calculate_opportunities", map[string]interface{}{
			"exchanges_count": len(marketData),
			"service":         "arbitrage",
		})
		return fmt.Errorf("failed to calculate arbitrage opportunities: %w", err)
	}
	calcSpan.SetData("opportunities_calculated", len(opportunities))
	observability.FinishSpan(calcSpan, nil)

	// Step 3b: Calculate multi-leg arbitrage opportunities (Triangular)
	_, mlCalcSpan := observability.StartSpan(ctx, observability.SpanOpArbitrage, "calculate_multi_leg_opportunities")
	var allMultiLegOps []models.MultiLegOpportunity
	var multiLegErrors int
	for exchangeName, tickers := range marketData {
		tickerData := make([]TickerData, len(tickers))
		for i, t := range tickers {
			tickerData[i] = TickerData{
				Symbol: t.TradingPair.Symbol,
				Bid:    t.LastPrice, // Using LastPrice as proxy if Bid/Ask are missing
				Ask:    t.LastPrice,
			}
		}
		mlOps, err := s.multiLegCalculator.FindTriangularOpportunities(ctx, exchangeName, tickerData)
		if err != nil {
			multiLegErrors++
			s.logger.WithFields(map[string]interface{}{"exchange": exchangeName}).WithError(err).Warn("Failed to calculate multi-leg opportunities")
		} else {
			allMultiLegOps = append(allMultiLegOps, mlOps...)
		}
	}
	mlCalcSpan.SetData("multi_leg_calculated", len(allMultiLegOps))
	mlCalcSpan.SetData("multi_leg_errors", multiLegErrors)
	observability.FinishSpan(mlCalcSpan, nil)

	// Step 4: Filter opportunities by minimum profit threshold
	_, filterSpan := observability.StartSpan(ctx, "arbitrage.filter", "filter_opportunities")
	validOpportunities := s.filterOpportunities(opportunities)
	filterSpan.SetData("opportunities_before", len(opportunities))
	filterSpan.SetData("opportunities_after", len(validOpportunities))
	filterSpan.SetData("min_profit", s.arbitrageConfig.MinProfit)
	observability.FinishSpan(filterSpan, nil)

	// Step 5: Store valid opportunities in database
	_, storeSpan := observability.StartSpan(ctx, "db.sql.batch", "store_opportunities")
	if err := s.storeOpportunities(validOpportunities); err != nil {
		observability.FinishSpan(storeSpan, err)
		observability.CaptureExceptionWithContext(ctx, err, "store_opportunities", map[string]interface{}{
			"opportunities_count": len(validOpportunities),
			"service":             "arbitrage",
		})
		return fmt.Errorf("failed to store opportunities: %w", err)
	}
	storeSpan.SetData("opportunities_stored", len(validOpportunities))

	// Step 5b: Store multi-leg opportunities
	if len(allMultiLegOps) > 0 {
		if err := s.storeMultiLegOpportunities(allMultiLegOps); err != nil {
			s.logger.WithError(err).Warn("Failed to store multi-leg opportunities")
		}
	}
	observability.FinishSpan(storeSpan, nil)

	// Update status
	s.mu.Lock()
	s.lastCalculation = time.Now()
	s.opportunitiesFound = len(validOpportunities)
	s.mu.Unlock()

	duration := time.Since(startTime)

	// Update main span with final metrics
	span.SetData("duration_ms", duration.Milliseconds())
	span.SetData("opportunities_found", len(validOpportunities))
	span.SetData("total_calculated", len(opportunities))
	span.SetTag("result", "success")

	// Add completion breadcrumb
	observability.AddBreadcrumbWithData(ctx, "arbitrage", "Calculation completed", sentry.LevelInfo, map[string]interface{}{
		"duration_ms":         duration.Milliseconds(),
		"opportunities_found": len(validOpportunities),
	})

	s.logger.WithFields(map[string]interface{}{
		"duration_ms":          duration.Milliseconds(),
		"opportunities_found":  len(validOpportunities),
		"total_calculated":     len(opportunities),
		"min_profit_threshold": s.arbitrageConfig.MinProfit,
	}).Info("Arbitrage calculation completed")

	return nil
}

// getLatestMarketData retrieves the latest market data for all exchanges
func (s *ArbitrageService) getLatestMarketData() (map[string][]models.MarketData, error) {
	// Check if database pool is available
	if isNilDBPool(s.db) {
		return nil, fmt.Errorf("database pool is not available")
	}

	// Query to get the latest market data for each trading pair on each exchange
	query := `
		SELECT DISTINCT ON (md.exchange_id, md.trading_pair_id)
			md.id, md.exchange_id, md.trading_pair_id, md.last_price, md.volume_24h, 
			md.timestamp, md.created_at, e.name as exchange_name, tp.symbol
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE md.timestamp >= NOW() - INTERVAL '10 minutes'
			AND e.status = 'active'
			AND tp.is_active = true
		ORDER BY md.exchange_id, md.trading_pair_id, md.timestamp DESC
	`

	rows, err := s.db.Query(s.ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query market data: %w", err)
	}
	defer rows.Close()

	marketData := make(map[string][]models.MarketData)

	for rows.Next() {
		var data models.MarketData
		var exchangeName string
		var symbol string

		err := rows.Scan(
			&data.ID, &data.ExchangeID, &data.TradingPairID, &data.LastPrice,
			&data.Volume24h, &data.Timestamp, &data.CreatedAt, &exchangeName, &symbol,
		)
		if err != nil {
			s.logger.WithError(err).Error("Failed to scan market data row")
			continue
		}

		// Set the exchange and trading pair info for the calculator
		data.Exchange = &models.Exchange{ID: data.ExchangeID, Name: exchangeName}
		data.TradingPair = &models.TradingPair{ID: data.TradingPairID, Symbol: symbol}

		marketData[exchangeName] = append(marketData[exchangeName], data)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating market data rows: %w", err)
	}

	s.logger.WithFields(map[string]interface{}{"exchanges": len(marketData)}).Info("Market data retrieved")

	// If no market data found, run diagnostics to understand why
	if len(marketData) == 0 {
		s.diagnoseNoMarketData()
	}

	for exchange, data := range marketData {
		s.logger.WithFields(map[string]interface{}{
			"exchange": exchange,
			"pairs":    len(data),
		}).Debug("Exchange data")
	}

	return marketData, nil
}

// diagnoseNoMarketData logs diagnostic information when no market data is available.
// This helps identify the root cause: missing data, stale data, or database seeding issues.
func (s *ArbitrageService) diagnoseNoMarketData() {
	var totalRows, freshRows, activeExchanges, activePairs int

	// Check total market_data rows
	err := s.db.QueryRow(s.ctx, "SELECT COUNT(*) FROM market_data").Scan(&totalRows)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to count market_data rows")
		totalRows = -1
	}

	// Check fresh market_data rows (within 10 minutes)
	err = s.db.QueryRow(s.ctx, "SELECT COUNT(*) FROM market_data WHERE timestamp >= NOW() - INTERVAL '10 minutes'").Scan(&freshRows)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to count fresh market_data rows")
		freshRows = -1
	}

	// Check active exchanges
	err = s.db.QueryRow(s.ctx, "SELECT COUNT(*) FROM exchanges WHERE status = 'active'").Scan(&activeExchanges)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to count active exchanges")
		activeExchanges = -1
	}

	// Check active trading pairs
	err = s.db.QueryRow(s.ctx, "SELECT COUNT(*) FROM trading_pairs WHERE is_active = true").Scan(&activePairs)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to count active trading pairs")
		activePairs = -1
	}

	// Log comprehensive diagnostic
	s.logger.WithFields(map[string]interface{}{
		"total_market_data_rows":    totalRows,
		"fresh_rows_10min":          freshRows,
		"active_exchanges":          activeExchanges,
		"active_trading_pairs":      activePairs,
		"diagnostic_recommendation": s.getDiagnosticRecommendation(totalRows, freshRows, activeExchanges, activePairs),
	}).Warn("Market data diagnostic - no data available for arbitrage")
}

// getDiagnosticRecommendation returns a human-readable recommendation based on the counts.
func (s *ArbitrageService) getDiagnosticRecommendation(totalRows, freshRows, activeExchanges, activePairs int) string {
	if activeExchanges == 0 {
		return "No active exchanges in database - check migrations and seeding"
	}
	if activePairs == 0 {
		return "No active trading pairs - collector may not have created pairs yet"
	}
	if totalRows == 0 {
		return "No market data at all - collector has not saved any data yet"
	}
	if freshRows == 0 {
		return "Market data exists but is stale (>10min old) - collector may be failing"
	}
	return "Unknown issue - data exists but query returned nothing"
}

// filterOpportunities filters arbitrage opportunities based on configuration thresholds
func (s *ArbitrageService) filterOpportunities(opportunities []models.ArbitrageOpportunity) []models.ArbitrageOpportunity {
	var filtered []models.ArbitrageOpportunity

	for _, opp := range opportunities {
		// Check minimum profit threshold
		if opp.ProfitPercentage.LessThan(decimal.NewFromFloat(s.arbitrageConfig.MinProfit)) {
			continue
		}

		// Check if opportunity is not too old
		maxAge := time.Duration(s.arbitrageConfig.MaxAgeMinutes) * time.Minute
		if time.Since(opp.DetectedAt) > maxAge {
			continue
		}

		// Additional filtering can be added here (volume thresholds, etc.)
		filtered = append(filtered, opp)
	}

	s.logger.WithFields(map[string]interface{}{
		"original": len(opportunities),
		"filtered": len(filtered),
	}).Info("Filtered opportunities")
	return filtered
}

// storeOpportunities stores arbitrage opportunities in the database
func (s *ArbitrageService) storeOpportunities(opportunities []models.ArbitrageOpportunity) error {
	if len(opportunities) == 0 {
		s.logger.Info("No opportunities to store")
		return nil
	}

	// Use batch processing for better performance
	batchSize := s.arbitrageConfig.BatchSize
	for i := 0; i < len(opportunities); i += batchSize {
		end := i + batchSize
		if end > len(opportunities) {
			end = len(opportunities)
		}

		batch := opportunities[i:end]
		if err := s.storeOpportunityBatch(batch); err != nil {
			return fmt.Errorf("failed to store opportunity batch %d-%d: %w", i, end-1, err)
		}
	}

	s.logger.WithFields(map[string]interface{}{"count": len(opportunities)}).Info("Stored opportunities")
	return nil
}

// storeOpportunityBatch stores a batch of arbitrage opportunities
func (s *ArbitrageService) storeOpportunityBatch(opportunities []models.ArbitrageOpportunity) error {
	if isNilDBPool(s.db) {
		return fmt.Errorf("database pool is not available")
	}

	tx, err := s.db.Begin(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(s.ctx); err != nil && err != pgx.ErrTxClosed {
			s.logger.WithError(err).Error("Failed to rollback transaction")
		}
	}()

	for _, opp := range opportunities {
		// Generate UUID for the opportunity if not already set
		if opp.ID == "" {
			opp.ID = uuid.New().String()
		}

		// Insert the arbitrage opportunity
		query := `
			INSERT INTO arbitrage_opportunities (
				id, buy_exchange_id, sell_exchange_id, trading_pair_id, 
				buy_price, sell_price, profit_percentage, detected_at, expires_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id) DO UPDATE SET
				buy_price = EXCLUDED.buy_price,
				sell_price = EXCLUDED.sell_price,
				profit_percentage = EXCLUDED.profit_percentage,
				detected_at = EXCLUDED.detected_at,
				expires_at = EXCLUDED.expires_at
		`

		// Convert decimal values to database-compatible types
		buyPrice, _ := opp.BuyPrice.Float64()
		sellPrice, _ := opp.SellPrice.Float64()
		profitPercentage, _ := opp.ProfitPercentage.Float64()

		_, err := tx.Exec(s.ctx, query,
			opp.ID, opp.BuyExchangeID, opp.SellExchangeID, opp.TradingPairID,
			buyPrice, sellPrice, profitPercentage, opp.DetectedAt, opp.ExpiresAt,
		)

		if err != nil {
			s.logger.WithFields(map[string]interface{}{
				"buy_exchange":  opp.BuyExchangeID,
				"sell_exchange": opp.SellExchangeID,
				"symbol":        opp.TradingPair != nil,
			}).WithError(err).Error("Failed to insert opportunity")
			return fmt.Errorf("failed to insert opportunity: %w", err)
		}
	}

	if err := tx.Commit(s.ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// cleanupOldOpportunities removes expired arbitrage opportunities
func (s *ArbitrageService) cleanupOldOpportunities() error {
	// Check if database pool is available
	if isNilDBPool(s.db) {
		return fmt.Errorf("database pool is not available")
	}

	// Since the table uses expires_at instead of is_active, we can just delete expired opportunities
	// or we can leave them for historical analysis. For now, let's just log expired ones.

	query := `
		SELECT COUNT(*) 
		FROM arbitrage_opportunities 
		WHERE expires_at <= NOW()
	`

	var expiredCount int
	err := s.db.QueryRow(s.ctx, query).Scan(&expiredCount)
	if err != nil {
		return fmt.Errorf("failed to count expired opportunities: %w", err)
	}

	if expiredCount > 0 {
		s.logger.WithFields(map[string]interface{}{"count": expiredCount}).Info("Found expired opportunities")
	}

	return nil
}

// countTotalTradingPairs counts the total number of trading pairs across all exchanges
func (s *ArbitrageService) countTotalTradingPairs(marketData map[string][]models.MarketData) int {
	total := 0
	for _, data := range marketData {
		total += len(data)
	}
	return total
}

// GetActiveOpportunities retrieves currently active arbitrage opportunities
func (s *ArbitrageService) GetActiveOpportunities(ctx context.Context, limit int) ([]models.ArbitrageOpportunity, error) {
	// Check if database pool is available
	if isNilDBPool(s.db) {
		return nil, fmt.Errorf("database pool is not available")
	}

	query := `
		SELECT 
			ao.id, ao.buy_exchange_id, ao.sell_exchange_id, ao.trading_pair_id,
			ao.buy_price, ao.sell_price, ao.profit_percentage, ao.detected_at, ao.expires_at,
			be.name as buy_exchange_name, se.name as sell_exchange_name,
			tp.symbol, tp.base_currency, tp.quote_currency
		FROM arbitrage_opportunities ao
		JOIN exchanges be ON ao.buy_exchange_id = be.id
		JOIN exchanges se ON ao.sell_exchange_id = se.id
		JOIN trading_pairs tp ON ao.trading_pair_id = tp.id
		WHERE ao.expires_at > NOW()
		ORDER BY ao.profit_percentage DESC, ao.detected_at DESC
		LIMIT $1
	`

	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query active opportunities: %w", err)
	}
	defer rows.Close()

	var opportunities []models.ArbitrageOpportunity

	for rows.Next() {
		var opp models.ArbitrageOpportunity
		var buyExchangeName, sellExchangeName, symbol, baseCurrency, quoteCurrency string

		err := rows.Scan(
			&opp.ID, &opp.BuyExchangeID, &opp.SellExchangeID, &opp.TradingPairID,
			&opp.BuyPrice, &opp.SellPrice, &opp.ProfitPercentage, &opp.DetectedAt, &opp.ExpiresAt,
			&buyExchangeName, &sellExchangeName,
			&symbol, &baseCurrency, &quoteCurrency,
		)
		if err != nil {
			s.logger.WithError(err).Error("Failed to scan opportunity row")
			continue
		}

		// Set the exchange and trading pair info
		opp.BuyExchange = &models.Exchange{ID: opp.BuyExchangeID, Name: buyExchangeName}
		opp.SellExchange = &models.Exchange{ID: opp.SellExchangeID, Name: sellExchangeName}
		opp.TradingPair = &models.TradingPair{
			ID:            opp.TradingPairID,
			Symbol:        symbol,
			BaseCurrency:  baseCurrency,
			QuoteCurrency: quoteCurrency,
		}

		opportunities = append(opportunities, opp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating opportunity rows: %w", err)
	}

	return opportunities, nil
}

// storeMultiLegOpportunities stores multi-leg opportunities in the database
func (s *ArbitrageService) storeMultiLegOpportunities(opportunities []models.MultiLegOpportunity) error {
	if len(opportunities) == 0 {
		return nil
	}

	tx, err := s.db.Begin(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(s.ctx)
	}()

	for _, opp := range opportunities {
		if opp.ID == "" {
			opp.ID = uuid.New().String()
		}

		// Insert main opportunity
		oppQuery := `
			INSERT INTO multi_leg_opportunities (id, exchange_id, profit_percentage, detected_at, expires_at)
			SELECT $1, e.id, $3, $4, $5
			FROM exchanges e WHERE e.name = $2
			ON CONFLICT (id) DO UPDATE SET
				profit_percentage = EXCLUDED.profit_percentage,
				expires_at = EXCLUDED.expires_at
		`
		_, err = tx.Exec(s.ctx, oppQuery, opp.ID, opp.ExchangeName, opp.ProfitPercentage, opp.DetectedAt, opp.ExpiresAt)
		if err != nil {
			return fmt.Errorf("failed to insert multi-leg opportunity: %w", err)
		}

		// Insert legs
		for i, leg := range opp.Legs {
			legQuery := `
				INSERT INTO multi_leg_legs (opportunity_id, leg_index, symbol, side, price)
				VALUES ($1, $2, $3, $4, $5)
			`
			_, err = tx.Exec(s.ctx, legQuery, opp.ID, i, leg.Symbol, leg.Side, leg.Price)
			if err != nil {
				return fmt.Errorf("failed to insert multi-leg leg: %w", err)
			}
		}
	}

	return tx.Commit(s.ctx)
}

// GetActiveMultiLegOpportunities retrieves currently active multi-leg arbitrage opportunities
func (s *ArbitrageService) GetActiveMultiLegOpportunities(ctx context.Context, limit int) ([]models.MultiLegOpportunity, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database pool is not available")
	}

	query := `
		SELECT 
			mlo.id, e.name as exchange_name, mlo.profit_percentage, mlo.detected_at, mlo.expires_at
		FROM multi_leg_opportunities mlo
		JOIN exchanges e ON mlo.exchange_id = e.id
		WHERE mlo.expires_at > NOW()
		ORDER BY mlo.profit_percentage DESC
		LIMIT $1
	`

	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var opportunities []models.MultiLegOpportunity
	for rows.Next() {
		var opp models.MultiLegOpportunity
		if err := rows.Scan(&opp.ID, &opp.ExchangeName, &opp.ProfitPercentage, &opp.DetectedAt, &opp.ExpiresAt); err != nil {
			continue
		}

		// Fetch legs for this opportunity
		legsQuery := `SELECT symbol, side, price FROM multi_leg_legs WHERE opportunity_id = $1 ORDER BY leg_index`
		legRows, err := s.db.Query(ctx, legsQuery, opp.ID)
		if err == nil {
			for legRows.Next() {
				var leg models.ArbitrageLeg
				if err := legRows.Scan(&leg.Symbol, &leg.Side, &leg.Price); err == nil {
					opp.Legs = append(opp.Legs, leg)
				}
			}
			legRows.Close()
		}

		opportunities = append(opportunities, opp)
	}

	return opportunities, nil
}
