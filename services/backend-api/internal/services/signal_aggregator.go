package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/talib"
	"github.com/shopspring/decimal"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
)

// SignalType represents the type of trading signal
type SignalType string

const (
	SignalTypeArbitrage SignalType = "arbitrage"
	SignalTypeTechnical SignalType = "technical"
)

// SignalStrength represents the strength of a trading signal
type SignalStrength string

const (
	SignalStrengthWeak   SignalStrength = "weak"
	SignalStrengthMedium SignalStrength = "medium"
	SignalStrengthStrong SignalStrength = "strong"
)

// AggregatedSignal represents a consolidated trading signal derived from multiple sources or indicators.
type AggregatedSignal struct {
	ID              string                 `json:"id" gorm:"primaryKey"`
	SignalType      SignalType             `json:"signal_type"`
	Symbol          string                 `json:"symbol"`
	Action          string                 `json:"action"` // "buy", "sell", "hold"
	Strength        SignalStrength         `json:"strength"`
	Confidence      decimal.Decimal        `json:"confidence"`
	ProfitPotential decimal.Decimal        `json:"profit_potential"`
	RiskLevel       decimal.Decimal        `json:"risk_level"`
	Exchanges       []string               `json:"exchanges"`
	Indicators      []string               `json:"indicators"`
	Metadata        map[string]interface{} `json:"metadata"`
	CreatedAt       time.Time              `json:"created_at"`
	ExpiresAt       time.Time              `json:"expires_at"`
}

// SignalFingerprint represents a unique identifier for deduplication of signals.
type SignalFingerprint struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Hash      string    `json:"hash" gorm:"uniqueIndex"`
	SignalID  string    `json:"signal_id"`
	CreatedAt time.Time `json:"created_at"`
}

// SignalComponent represents an individual technical indicator signal that contributes to an aggregated signal.
type SignalComponent struct {
	Indicator   string          `json:"indicator"`
	Description string          `json:"description"`
	Confidence  decimal.Decimal `json:"confidence"`
	Strength    float64         `json:"strength"`
}

// TechnicalSignalInput represents input data required for technical analysis signal generation.
type TechnicalSignalInput struct {
	Symbol     string
	Exchange   string
	Prices     []decimal.Decimal
	Volumes    []decimal.Decimal
	Timestamps []time.Time
}

// ArbitrageSignalInput represents input data required for arbitrage signal generation.
type ArbitrageSignalInput struct {
	Opportunities []models.ArbitrageOpportunity
	MinVolume     decimal.Decimal `json:"min_volume"`
	BaseAmount    decimal.Decimal `json:"base_amount"` // For profit calculation (e.g., $20,000)
}

// SignalQualityScorerInterface defines the contract for assessing the quality of trading signals.
type SignalQualityScorerInterface interface {
	AssessSignalQuality(ctx context.Context, input *SignalQualityInput) (*SignalQualityMetrics, error)
	IsSignalQualityAcceptable(metrics *SignalQualityMetrics, thresholds *QualityThresholds) bool
	GetDefaultQualityThresholds() *QualityThresholds
}

// SignalAggregatorConfig holds configuration parameters for the signal aggregator service.
type SignalAggregatorConfig struct {
	MinConfidence       decimal.Decimal `json:"min_confidence"`
	MinProfitThreshold  decimal.Decimal `json:"min_profit_threshold"`
	MaxRiskLevel        decimal.Decimal `json:"max_risk_level"`
	SignalTTL           time.Duration   `json:"signal_ttl"`
	DeduplicationWindow time.Duration   `json:"deduplication_window"`
	MaxSignalsPerSymbol int             `json:"max_signals_per_symbol"`
}

// SignalAggregator handles the aggregation, processing, and deduplication of trading signals.
type SignalAggregator struct {
	config        *config.Config
	db            DBPool
	logger        *zaplogrus.Logger
	sigConfig     SignalAggregatorConfig
	qualityScorer SignalQualityScorerInterface
	cache         map[string]*AggregatedSignal
}

// NewSignalAggregator creates a new instance of SignalAggregator.
//
// Parameters:
//   - cfg: The application configuration.
//   - db: The database connection pool.
//   - logger: The logger instance.
//
// Returns:
//   - A pointer to the initialized SignalAggregator.
func NewSignalAggregator(cfg *config.Config, db DBPool, logger *zaplogrus.Logger) *SignalAggregator {
	return &SignalAggregator{
		config: cfg,
		db:     db,
		logger: logger,
		sigConfig: SignalAggregatorConfig{
			MinConfidence:       decimal.NewFromFloat(0.6),
			MinProfitThreshold:  decimal.NewFromFloat(0.5), // 0.5%
			MaxRiskLevel:        decimal.NewFromFloat(0.3), // 30%
			SignalTTL:           15 * time.Minute,
			DeduplicationWindow: 5 * time.Minute,
			MaxSignalsPerSymbol: 3,
		},
		qualityScorer: NewSignalQualityScorer(cfg, db, logger),
		cache:         make(map[string]*AggregatedSignal),
	}
}

// AggregateArbitrageSignals processes raw arbitrage opportunities into aggregated signals.
// It groups opportunities by symbol, filters by volume and profit threshold, and creates enhanced signals with price ranges.
//
// Parameters:
//   - ctx: The context for the operation.
//   - input: The input data containing arbitrage opportunities.
//
// Returns:
//   - A slice of aggregated signals, or an error if aggregation fails.
func (sa *SignalAggregator) AggregateArbitrageSignals(ctx context.Context, input ArbitrageSignalInput) ([]*AggregatedSignal, error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpSignalProcessing, "SignalAggregator.AggregateArbitrageSignals", map[string]string{
		"signal_type": "arbitrage",
		"input_count": fmt.Sprintf("%d", len(input.Opportunities)),
		"min_volume":  input.MinVolume.String(),
		"base_amount": input.BaseAmount.String(),
	})
	defer observability.FinishSpan(span, nil)

	observability.AddBreadcrumb(spanCtx, "signal_aggregator", "Starting arbitrage signal aggregation", sentry.LevelInfo)

	// Stub telemetry - log arbitrage signal aggregation
	sa.logger.WithFields(zaplogrus.Fields{
		"operation_type": "signal_aggregation",
		"signal_type":    "arbitrage",
		"input_count":    len(input.Opportunities),
		"min_volume":     input.MinVolume.String(),
		"base_amount":    input.BaseAmount.String(),
	}).Info("Starting arbitrage signal aggregation")

	sa.logger.Info("Aggregating arbitrage signals with enhanced price ranges")

	var signals []*AggregatedSignal
	symbolGroups := make(map[string][]models.ArbitrageOpportunity)

	// Set default values if not provided
	minVolume := input.MinVolume
	if minVolume.IsZero() {
		minVolume = decimal.NewFromFloat(10000) // Default $10,000 minimum volume
	}
	baseAmount := input.BaseAmount
	if baseAmount.IsZero() {
		baseAmount = decimal.NewFromFloat(20000) // Default $20,000 base amount
	}

	// Group opportunities by symbol and filter by volume
	for _, opp := range input.Opportunities {
		var symbol string
		if opp.TradingPair != nil {
			symbol = opp.TradingPair.Symbol
		} else {
			// Skip opportunities without trading pair info
			continue
		}

		// Apply volume filtering (simulate volume check - in real implementation, this would come from exchange data)
		estimatedVolume := opp.BuyPrice.Mul(decimal.NewFromFloat(1000)) // Simulate volume calculation
		if estimatedVolume.GreaterThanOrEqual(minVolume) {
			symbolGroups[symbol] = append(symbolGroups[symbol], opp)
		} else {
			sa.logger.WithFields(zaplogrus.Fields{
				"symbol":           symbol,
				"estimated_volume": estimatedVolume,
				"min_volume":       minVolume,
			}).Debug("Filtered out low volume arbitrage opportunity")
		}
	}

	// Process each symbol group
	for symbol, opportunities := range symbolGroups {
		// Filter by profit threshold
		var validOpps []models.ArbitrageOpportunity
		for _, opp := range opportunities {
			if opp.ProfitPercentage.GreaterThanOrEqual(sa.sigConfig.MinProfitThreshold) {
				validOpps = append(validOpps, opp)
			}
		}

		if len(validOpps) == 0 {
			continue
		}

		// Sort by profit percentage (descending)
		sort.Slice(validOpps, func(i, j int) bool {
			return validOpps[i].ProfitPercentage.GreaterThan(validOpps[j].ProfitPercentage)
		})

		// Create aggregated signal with price ranges from multiple opportunities
		if len(validOpps) > 0 {
			signal := sa.createEnhancedArbitrageSignal(validOpps, symbol, minVolume, baseAmount)

			// Assess signal quality
			qualityInput := SignalQualityInput{
				SignalType:       string(signal.SignalType),
				Symbol:           signal.Symbol,
				Exchanges:        signal.Exchanges,
				Volume:           minVolume, // Use minimum volume requirement
				ProfitPotential:  signal.ProfitPotential,
				Confidence:       signal.Confidence,
				Timestamp:        signal.CreatedAt,
				Indicators:       map[string]interface{}{"arbitrage": true, "opportunity_count": len(validOpps)},
				SignalCount:      len(validOpps),
				SignalComponents: []string{"arbitrage"},
			}

			qualityMetrics, err := sa.qualityScorer.AssessSignalQuality(ctx, &qualityInput)
			if err != nil {
				sa.logger.WithError(err).Warn("Failed to assess signal quality")
				signals = append(signals, signal) // Include signal if assessment fails
			} else if sa.qualityScorer.IsSignalQualityAcceptable(qualityMetrics, sa.qualityScorer.GetDefaultQualityThresholds()) {
				signals = append(signals, signal)
			} else {
				sa.logger.WithFields(map[string]interface{}{"signal_id": signal.ID}).Debug("Signal rejected due to low quality")
			}
		}
	}

	// Stub telemetry - log aggregation results
	sa.logger.WithFields(zaplogrus.Fields{
		"signals_generated": len(signals),
		"symbols_processed": len(symbolGroups),
		"operation_result":  "success",
	}).Info("Arbitrage signal aggregation completed")

	return signals, nil
}

// AggregateTechnicalSignals processes historical price and volume data to generate technical analysis signals.
// It calculates indicators like SMA, EMA, RSI, MACD and generates buy/sell signals based on crossovers and thresholds.
//
// Parameters:
//   - ctx: The context for the operation.
//   - input: The input data containing price and volume history.
//
// Returns:
//   - A slice of aggregated signals based on technical indicators, or an error if processing fails.
func (sa *SignalAggregator) AggregateTechnicalSignals(ctx context.Context, input TechnicalSignalInput) ([]*AggregatedSignal, error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpSignalProcessing, "SignalAggregator.AggregateTechnicalSignals", map[string]string{
		"signal_type":   "technical",
		"symbol":        input.Symbol,
		"exchange":      input.Exchange,
		"prices_count":  fmt.Sprintf("%d", len(input.Prices)),
		"volumes_count": fmt.Sprintf("%d", len(input.Volumes)),
	})
	defer observability.FinishSpan(span, nil)

	observability.AddBreadcrumb(spanCtx, "signal_aggregator", "Starting technical signal aggregation", sentry.LevelInfo)

	// Stub telemetry - log technical signal aggregation
	sa.logger.WithFields(zaplogrus.Fields{
		"operation_type": "signal_aggregation",
		"signal_type":    "technical",
		"symbol":         input.Symbol,
		"exchange":       input.Exchange,
		"prices_count":   len(input.Prices),
		"volumes_count":  len(input.Volumes),
	}).Info("Starting technical signal aggregation")

	sa.logger.WithFields(map[string]interface{}{"symbol": input.Symbol}).Info("Aggregating technical signals")

	if len(input.Prices) < 20 {
		// Stub telemetry - log insufficient data error
		sa.logger.WithFields(zaplogrus.Fields{
			"required_points": 20,
			"actual_points":   len(input.Prices),
		}).Error("Insufficient price data for technical analysis")
		err := fmt.Errorf("insufficient price data for technical analysis: need at least 20 points, got %d", len(input.Prices))
		observability.AddBreadcrumb(spanCtx, "signal_aggregator", "Insufficient data for analysis", sentry.LevelWarning)
		return nil, err
	}

	// Convert decimal prices to float64 for talib (goflux)
	prices := make([]float64, len(input.Prices))
	volumes := make([]float64, len(input.Volumes))
	for i, price := range input.Prices {
		prices[i], _ = price.Float64()
		if i < len(input.Volumes) {
			volumes[i], _ = input.Volumes[i].Float64()
		}
	}

	// Calculate technical indicators
	indicators := sa.calculateTechnicalIndicators(prices)

	// Generate signals based on indicators
	signals := sa.generateTechnicalSignals(input.Symbol, input.Exchange, indicators)

	// Assess quality for each technical signal
	var qualitySignals []*AggregatedSignal
	for _, signal := range signals {
		// Extract signal count and components from metadata
		signalCount := 1
		signalComponents := []string{}
		if metadata, ok := signal.Metadata["signal_count"].(int); ok {
			signalCount = metadata
		}
		if components, ok := signal.Metadata["signal_components"].([]string); ok {
			signalComponents = components
		}

		// Calculate average volume for quality assessment
		avgVolume := decimal.NewFromFloat(0)
		if len(input.Volumes) > 0 {
			totalVolume := decimal.NewFromFloat(0)
			for _, vol := range input.Volumes {
				totalVolume = totalVolume.Add(vol)
			}
			avgVolume = totalVolume.Div(decimal.NewFromInt(int64(len(input.Volumes))))
		}

		qualityInput := SignalQualityInput{
			SignalType:       string(signal.SignalType),
			Symbol:           signal.Symbol,
			Exchanges:        signal.Exchanges,
			Volume:           avgVolume,
			ProfitPotential:  signal.ProfitPotential,
			Confidence:       signal.Confidence,
			Timestamp:        signal.CreatedAt,
			Indicators:       signal.Metadata,
			SignalCount:      signalCount,
			SignalComponents: signalComponents,
		}

		qualityMetrics, err := sa.qualityScorer.AssessSignalQuality(ctx, &qualityInput)
		if err != nil {
			sa.logger.WithError(err).Warn("Failed to assess technical signal quality")
			qualitySignals = append(qualitySignals, signal) // Include signal if assessment fails
		} else if sa.qualityScorer.IsSignalQualityAcceptable(qualityMetrics, sa.qualityScorer.GetDefaultQualityThresholds()) {
			qualitySignals = append(qualitySignals, signal)
		} else {
			sa.logger.WithFields(map[string]interface{}{"signal_id": signal.ID}).Debug("Technical signal rejected due to low quality")
		}
	}

	// Stub telemetry - log technical signal results
	sa.logger.WithFields(zaplogrus.Fields{
		"signals_generated": len(qualitySignals),
		"signals_raw_count": len(signals),
		"operation_result":  "success",
	}).Info("Technical signal aggregation completed")

	return qualitySignals, nil
}

// DeduplicateSignals filters out signals that are considered duplicates of recently processed signals.
// It uses a fingerprinting mechanism based on signal characteristics to identify duplicates within a configured time window.
//
// Parameters:
//   - ctx: The context for the operation.
//   - signals: The list of signals to deduplicate.
//
// Returns:
//   - A slice of unique signals, or an error if deduplication fails.
func (sa *SignalAggregator) DeduplicateSignals(ctx context.Context, signals []*AggregatedSignal) ([]*AggregatedSignal, error) {
	// Stub telemetry - log deduplication start
	sa.logger.WithFields(zaplogrus.Fields{
		"operation_type":      "signal_deduplication",
		"signals_input_count": len(signals),
	}).Info("Starting signal deduplication")

	sa.logger.Info("Deduplicating signals")

	var uniqueSignals []*AggregatedSignal
	seenHashes := make(map[string]bool)

	for _, signal := range signals {
		hash := sa.generateSignalHash(signal)

		// Check if we've seen this hash recently
		if !sa.isHashRecent(ctx, hash) {
			uniqueSignals = append(uniqueSignals, signal)
			seenHashes[hash] = true

			// Store fingerprint
			fingerprint := &SignalFingerprint{
				ID:        uuid.New().String(),
				Hash:      hash,
				SignalID:  signal.ID,
				CreatedAt: time.Now(),
			}
			sa.storeFingerprint(ctx, fingerprint)
		}
	}

	// Stub telemetry - log deduplication results
	sa.logger.WithFields(zaplogrus.Fields{
		"signals_unique_count":       len(uniqueSignals),
		"signals_duplicates_removed": len(signals) - len(uniqueSignals),
		"operation_result":           "success",
	}).Info("Signal deduplication completed")

	sa.logger.WithFields(zaplogrus.Fields{
		"original_count": len(signals),
		"unique_count":   len(uniqueSignals),
	}).Info("Signal deduplication completed")

	return uniqueSignals, nil
}

// createEnhancedArbitrageSignal creates an aggregated signal with price ranges from multiple opportunities.
func (sa *SignalAggregator) createEnhancedArbitrageSignal(opportunities []models.ArbitrageOpportunity, symbol string, minVolume, baseAmount decimal.Decimal) *AggregatedSignal {
	if len(opportunities) == 0 {
		return nil
	}

	// Calculate price ranges
	buyPrices := make([]decimal.Decimal, 0)
	sellPrices := make([]decimal.Decimal, 0)
	buyExchanges := make([]string, 0)
	sellExchanges := make([]string, 0)
	allExchanges := make(map[string]bool)

	for _, opp := range opportunities {
		buyPrices = append(buyPrices, opp.BuyPrice)
		sellPrices = append(sellPrices, opp.SellPrice)

		buyExchangeName := fmt.Sprintf("%d", opp.BuyExchangeID)
		sellExchangeName := fmt.Sprintf("%d", opp.SellExchangeID)

		if opp.BuyExchange != nil {
			buyExchangeName = opp.BuyExchange.Name
		}
		if opp.SellExchange != nil {
			sellExchangeName = opp.SellExchange.Name
		}

		buyExchanges = append(buyExchanges, buyExchangeName)
		sellExchanges = append(sellExchanges, sellExchangeName)
		allExchanges[buyExchangeName] = true
		allExchanges[sellExchangeName] = true
	}

	// Calculate buy price range
	buyMin := buyPrices[0]
	buyMax := buyPrices[0]
	buySum := decimal.NewFromFloat(0)
	for _, price := range buyPrices {
		if price.LessThan(buyMin) {
			buyMin = price
		}
		if price.GreaterThan(buyMax) {
			buyMax = price
		}
		buySum = buySum.Add(price)
	}
	buyAvg := buySum.Div(decimal.NewFromInt(int64(len(buyPrices))))

	// Calculate sell price range
	sellMin := sellPrices[0]
	sellMax := sellPrices[0]
	sellSum := decimal.NewFromFloat(0)
	for _, price := range sellPrices {
		if price.LessThan(sellMin) {
			sellMin = price
		}
		if price.GreaterThan(sellMax) {
			sellMax = price
		}
		sellSum = sellSum.Add(price)
	}
	sellAvg := sellSum.Div(decimal.NewFromInt(int64(len(sellPrices))))

	// Calculate profit ranges
	minProfitPercent := sellMin.Sub(buyMax).Div(buyMax).Mul(decimal.NewFromFloat(100))
	maxProfitPercent := sellMax.Sub(buyMin).Div(buyMin).Mul(decimal.NewFromFloat(100))
	avgProfitPercent := sellAvg.Sub(buyAvg).Div(buyAvg).Mul(decimal.NewFromFloat(100))

	// Calculate dollar amounts based on base amount
	minProfitDollar := baseAmount.Mul(minProfitPercent).Div(decimal.NewFromFloat(100))
	maxProfitDollar := baseAmount.Mul(maxProfitPercent).Div(decimal.NewFromFloat(100))
	avgProfitDollar := baseAmount.Mul(avgProfitPercent).Div(decimal.NewFromFloat(100))

	// Calculate overall confidence (higher for multiple opportunities)
	baseConfidence := sa.calculateArbitrageConfidence(opportunities[0])
	multiOpportunityBonus := decimal.NewFromFloat(float64(len(opportunities)-1) * 0.05) // 5% bonus per additional opportunity
	if multiOpportunityBonus.GreaterThan(decimal.NewFromFloat(0.2)) {
		multiOpportunityBonus = decimal.NewFromFloat(0.2) // Cap at 20% bonus
	}
	finalConfidence := baseConfidence.Add(multiOpportunityBonus)
	if finalConfidence.GreaterThan(decimal.NewFromFloat(1.0)) {
		finalConfidence = decimal.NewFromFloat(1.0)
	}

	strength := sa.determineSignalStrengthWithProfit(finalConfidence, avgProfitPercent)

	// Convert exchange map to slice
	exchangeList := make([]string, 0, len(allExchanges))
	for exchange := range allExchanges {
		exchangeList = append(exchangeList, exchange)
	}

	// Remove duplicates from buy and sell exchanges
	uniqueBuyExchanges := sa.removeDuplicateStrings(buyExchanges)
	uniqueSellExchanges := sa.removeDuplicateStrings(sellExchanges)

	return &AggregatedSignal{
		ID:              uuid.New().String(),
		SignalType:      SignalTypeArbitrage,
		Symbol:          symbol,
		Action:          "buy",
		Strength:        strength,
		Confidence:      finalConfidence,
		ProfitPotential: avgProfitPercent,
		RiskLevel:       decimal.NewFromFloat(0.08), // Lower risk for multiple opportunities
		Exchanges:       exchangeList,
		Indicators:      []string{"arbitrage"},
		Metadata: map[string]interface{}{
			"buy_price_range": map[string]interface{}{
				"min": buyMin,
				"max": buyMax,
				"avg": buyAvg,
			},
			"sell_price_range": map[string]interface{}{
				"min": sellMin,
				"max": sellMax,
				"avg": sellAvg,
			},
			"profit_range": map[string]interface{}{
				"min_percent": minProfitPercent,
				"max_percent": maxProfitPercent,
				"avg_percent": avgProfitPercent,
				"min_dollar":  minProfitDollar,
				"max_dollar":  maxProfitDollar,
				"avg_dollar":  avgProfitDollar,
				"base_amount": baseAmount,
			},
			"buy_exchanges":     uniqueBuyExchanges,
			"sell_exchanges":    uniqueSellExchanges,
			"opportunity_count": len(opportunities),
			"min_volume":        minVolume,
			"validity_minutes":  int(sa.sigConfig.SignalTTL.Minutes()),
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sa.sigConfig.SignalTTL),
	}
}

// removeDuplicateStrings removes duplicate strings from a slice
func (sa *SignalAggregator) removeDuplicateStrings(slice []string) []string {
	keys := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}
	return result
}

// calculateTechnicalIndicators computes various technical indicators from price history.
func (sa *SignalAggregator) calculateTechnicalIndicators(prices []float64) map[string][]float64 {
	indicators := make(map[string][]float64)

	if len(prices) < 50 {
		return indicators
	}

	// Calculate SMA 20 and 50 using goflux
	sma20 := talib.Sma(prices, 20)
	sma50 := talib.Sma(prices, 50)

	// Calculate EMA 12 using goflux
	ema12 := talib.Ema(prices, 12)

	// Calculate RSI 14 using goflux
	rsi := talib.Rsi(prices, 14)

	// Calculate MACD using goflux
	macdLine, macdSignal, _ := talib.Macd(prices, 12, 26, 9)

	indicators["sma_20"] = sma20
	indicators["sma_50"] = sma50
	indicators["ema_12"] = ema12
	indicators["rsi_14"] = rsi
	indicators["macd_line"] = macdLine
	indicators["macd_signal"] = macdSignal

	return indicators
}

// generateTechnicalSignals interprets technical indicators to generate buy/sell signals.
func (sa *SignalAggregator) generateTechnicalSignals(symbol, exchange string, indicators map[string][]float64) []*AggregatedSignal {
	// Collect individual signal components
	buySignals := make([]SignalComponent, 0)
	sellSignals := make([]SignalComponent, 0)

	// RSI-based signals
	if rsi, exists := indicators["rsi_14"]; exists && len(rsi) > 0 {
		currentRSI := rsi[len(rsi)-1]
		if currentRSI < 30 {
			// Oversold - Buy signal
			buySignals = append(buySignals, SignalComponent{
				Indicator:   "rsi_oversold",
				Description: "RSI oversold recovery",
				Confidence:  decimal.NewFromFloat(0.7),
				Strength:    0.7,
			})
		} else if currentRSI > 70 {
			// Overbought - Sell signal
			sellSignals = append(sellSignals, SignalComponent{
				Indicator:   "rsi_overbought",
				Description: "RSI overbought condition",
				Confidence:  decimal.NewFromFloat(0.7),
				Strength:    0.7,
			})
		}
	}

	// Moving Average Crossover
	if sma, exists := indicators["sma_20"]; exists && len(sma) > 1 {
		if ema, emaExists := indicators["ema_12"]; emaExists && len(ema) > 1 {
			currentSMA := sma[len(sma)-1]
			currentEMA := ema[len(ema)-1]
			prevSMA := sma[len(sma)-2]
			prevEMA := ema[len(ema)-2]

			// Golden Cross (EMA crosses above SMA)
			if currentEMA > currentSMA && prevEMA <= prevSMA {
				buySignals = append(buySignals, SignalComponent{
					Indicator:   "golden_cross",
					Description: "MA20 crossed above MA50 (Golden Cross)",
					Confidence:  decimal.NewFromFloat(0.8),
					Strength:    0.8,
				})
			}
			// Death Cross (EMA crosses below SMA)
			if currentEMA < currentSMA && prevEMA >= prevSMA {
				sellSignals = append(sellSignals, SignalComponent{
					Indicator:   "death_cross",
					Description: "MA20 crossed below MA50 (Death Cross)",
					Confidence:  decimal.NewFromFloat(0.8),
					Strength:    0.8,
				})
			}
		}
	}

	// MACD signals
	if macdLine, exists := indicators["macd_line"]; exists && len(macdLine) > 1 {
		if macdSignal, signalExists := indicators["macd_signal"]; signalExists && len(macdSignal) > 1 {
			currentMACD := macdLine[len(macdLine)-1]
			currentSignal := macdSignal[len(macdSignal)-1]
			prevMACD := macdLine[len(macdLine)-2]
			prevSignal := macdSignal[len(macdSignal)-2]

			// MACD bullish crossover (MACD line crosses above signal line)
			if currentMACD > currentSignal && prevMACD <= prevSignal {
				buySignals = append(buySignals, SignalComponent{
					Indicator:   "macd_bullish",
					Description: "MACD bullish crossover",
					Confidence:  decimal.NewFromFloat(0.75),
					Strength:    0.75,
				})
			}
			// MACD bearish crossover
			if currentMACD < currentSignal && prevMACD >= prevSignal {
				sellSignals = append(sellSignals, SignalComponent{
					Indicator:   "macd_bearish",
					Description: "MACD bearish crossover",
					Confidence:  decimal.NewFromFloat(0.75),
					Strength:    0.75,
				})
			}
		}
	}

	// Aggregate signals
	var aggregatedSignals []*AggregatedSignal

	// Create aggregated buy signal if we have buy components
	if len(buySignals) > 0 {
		aggregatedSignal := sa.createAggregatedTechnicalSignal(symbol, exchange, "buy", buySignals)
		aggregatedSignals = append(aggregatedSignals, aggregatedSignal)
	}

	// Create aggregated sell signal if we have sell components
	if len(sellSignals) > 0 {
		aggregatedSignal := sa.createAggregatedTechnicalSignal(symbol, exchange, "sell", sellSignals)
		aggregatedSignals = append(aggregatedSignals, aggregatedSignal)
	}

	return aggregatedSignals
}

// createAggregatedTechnicalSignal combines multiple signal components into a single aggregated signal.
func (sa *SignalAggregator) createAggregatedTechnicalSignal(symbol, exchange, action string, components []SignalComponent) *AggregatedSignal {
	// Combine descriptions
	descriptions := make([]string, len(components))
	indicators := make([]string, len(components))
	totalStrength := 0.0
	totalConfidence := decimal.NewFromFloat(0.0)

	for i, component := range components {
		descriptions[i] = component.Description
		indicators[i] = component.Indicator
		totalStrength += component.Strength
		totalConfidence = totalConfidence.Add(component.Confidence)
	}

	// Calculate aggregated confidence (average with bonus for multiple signals)
	avgConfidence := totalConfidence.Div(decimal.NewFromInt(int64(len(components))))
	// Bonus for multiple confirming signals (up to 20% boost)
	multiSignalBonus := decimal.NewFromFloat(float64(len(components)-1) * 0.1)
	if multiSignalBonus.GreaterThan(decimal.NewFromFloat(0.2)) {
		multiSignalBonus = decimal.NewFromFloat(0.2)
	}
	finalConfidence := avgConfidence.Add(multiSignalBonus)
	if finalConfidence.GreaterThan(decimal.NewFromFloat(1.0)) {
		finalConfidence = decimal.NewFromFloat(1.0)
	}

	// Calculate enhanced profit potential based on signal strength
	baseProfitPotential := decimal.NewFromFloat(2.0)
	if len(components) > 1 {
		// Increase profit potential for multiple confirming signals
		enhancementFactor := decimal.NewFromFloat(1.0 + float64(len(components)-1)*0.3)
		baseProfitPotential = baseProfitPotential.Mul(enhancementFactor)
	}

	// Calculate risk level (lower risk for multiple confirming signals)
	baseRiskLevel := decimal.NewFromFloat(0.2)
	if len(components) > 1 {
		// Reduce risk for multiple confirming signals
		riskReduction := decimal.NewFromFloat(float64(len(components)-1) * 0.03)
		baseRiskLevel = baseRiskLevel.Sub(riskReduction)
		if baseRiskLevel.LessThan(decimal.NewFromFloat(0.1)) {
			baseRiskLevel = decimal.NewFromFloat(0.1)
		}
	}

	return &AggregatedSignal{
		ID:              uuid.New().String(),
		SignalType:      SignalTypeTechnical,
		Symbol:          symbol,
		Action:          action,
		Strength:        sa.determineSignalStrength(finalConfidence),
		Confidence:      finalConfidence,
		ProfitPotential: baseProfitPotential,
		RiskLevel:       baseRiskLevel,
		Exchanges:       []string{exchange},
		Indicators:      indicators,
		Metadata: map[string]interface{}{
			"description":       strings.Join(descriptions, ", "),
			"signal_count":      len(components),
			"signal_components": indicators,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sa.sigConfig.SignalTTL),
	}
}

func (sa *SignalAggregator) calculateArbitrageConfidence(opp models.ArbitrageOpportunity) decimal.Decimal {
	// Basic confidence based on profit and spread
	// Higher profit = higher confidence (up to a point, then it looks suspicious)

	confidence := decimal.NewFromFloat(0.7) // Base confidence

	if opp.ProfitPercentage.GreaterThan(decimal.NewFromFloat(0.5)) {
		confidence = confidence.Add(decimal.NewFromFloat(0.1))
	}

	// Reduce if suspicious
	if opp.ProfitPercentage.GreaterThan(decimal.NewFromFloat(5.0)) {
		confidence = decimal.NewFromFloat(0.5) // Too good to be true?
	}

	return confidence
}

// determineSignalStrength maps confidence to strength (Weak, Medium, Strong)
func (sa *SignalAggregator) determineSignalStrength(confidence decimal.Decimal) SignalStrength {
	if confidence.GreaterThan(decimal.NewFromFloat(0.8)) {
		return SignalStrengthStrong
	}
	if confidence.GreaterThan(decimal.NewFromFloat(0.5)) {
		return SignalStrengthMedium
	}
	return SignalStrengthWeak
}

// determineSignalStrengthWithProfit considers profit potential
func (sa *SignalAggregator) determineSignalStrengthWithProfit(confidence, profit decimal.Decimal) SignalStrength {
	score := confidence.Mul(profit)

	if score.GreaterThan(decimal.NewFromFloat(1.0)) { // e.g. 0.8 conf * 1.5 profit
		return SignalStrengthStrong
	}
	if score.GreaterThan(decimal.NewFromFloat(0.4)) {
		return SignalStrengthMedium
	}
	return SignalStrengthWeak
}

func (sa *SignalAggregator) generateSignalHash(signal *AggregatedSignal) string {
	// Create a hash based on signal characteristics
	// Sort exchanges to ensure consistent hashing regardless of order
	sortedExchanges := make([]string, len(signal.Exchanges))
	copy(sortedExchanges, signal.Exchanges)
	sort.Strings(sortedExchanges)

	// Sort indicators as well for consistency
	sortedIndicators := make([]string, len(signal.Indicators))
	copy(sortedIndicators, signal.Indicators)
	sort.Strings(sortedIndicators)

	hashInput := fmt.Sprintf("%s_%s_%s_%s_%s",
		signal.SignalType,
		signal.Symbol,
		signal.Action,
		strings.Join(sortedIndicators, ","),
		strings.Join(sortedExchanges, ","),
	)
	return fmt.Sprintf("%x", []byte(hashInput))
}

func (sa *SignalAggregator) isHashRecent(ctx context.Context, hash string) bool {
	// Check if hash exists in recent fingerprints
	cutoff := time.Now().Add(-sa.sigConfig.DeduplicationWindow)
	var count int64
	query := `SELECT COUNT(*) FROM signal_fingerprints WHERE hash = $1 AND created_at > $2`
	err := sa.db.QueryRow(ctx, query, hash, cutoff).Scan(&count)

	if err != nil {
		sa.logger.WithError(err).Error("Failed to check hash recency")
		return false
	}

	return count > 0
}

func (sa *SignalAggregator) storeFingerprint(ctx context.Context, fingerprint *SignalFingerprint) {
	query := `INSERT INTO signal_fingerprints (hash, signal_id, created_at) VALUES ($1, $2, $3)`
	_, err := sa.db.Exec(ctx, query, fingerprint.Hash, fingerprint.SignalID, fingerprint.CreatedAt)
	if err != nil {
		sa.logger.WithError(err).Error("Failed to store signal fingerprint")
	}
}

// GetActiveAggregatedSignals retrieves active aggregated signals from the database, filtered by confidence.
//
// Parameters:
//   - ctx: The context for the operation.
//   - limit: The maximum number of signals to retrieve.
//
// Returns:
//   - A slice of active aggregated signals, or an error if retrieval fails.
func (sa *SignalAggregator) GetActiveAggregatedSignals(ctx context.Context, limit int) ([]*AggregatedSignal, error) {
	if isNilDBPool(sa.db) {
		return []*AggregatedSignal{}, nil
	}

	query := `
		SELECT 
			id, signal_type, symbol, action, strength, confidence,
			profit_potential, risk_level, exchanges, indicators,
			metadata, created_at, expires_at
		FROM aggregated_signals 
		WHERE expires_at > NOW() 
			AND confidence >= $1
		ORDER BY confidence DESC, profit_potential DESC, created_at DESC
		LIMIT $2
	`

	rows, err := sa.db.Query(ctx, query, sa.sigConfig.MinConfidence, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregated signals: %w", err)
	}
	defer rows.Close()

	var signals []*AggregatedSignal
	for rows.Next() {
		signal := &AggregatedSignal{}
		var exchangesJSON, indicatorsJSON, metadataJSON []byte
		var strengthStr string

		err := rows.Scan(
			&signal.ID, &signal.SignalType, &signal.Symbol, &signal.Action,
			&strengthStr, &signal.Confidence, &signal.ProfitPotential,
			&signal.RiskLevel, &exchangesJSON, &indicatorsJSON,
			&metadataJSON, &signal.CreatedAt, &signal.ExpiresAt,
		)
		if err != nil {
			sa.logger.WithError(err).Error("Failed to scan aggregated signal")
			continue
		}

		// Parse strength
		signal.Strength = SignalStrength(strengthStr)

		// Parse JSON fields
		if len(exchangesJSON) > 0 {
			if err := json.Unmarshal(exchangesJSON, &signal.Exchanges); err != nil {
				sa.logger.WithError(err).Error("Failed to unmarshal exchanges")
			}
		}

		if len(indicatorsJSON) > 0 {
			if err := json.Unmarshal(indicatorsJSON, &signal.Indicators); err != nil {
				sa.logger.WithError(err).Error("Failed to unmarshal indicators")
			}
		}

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &signal.Metadata); err != nil {
				sa.logger.WithError(err).Error("Failed to unmarshal metadata")
			}
		}

		signals = append(signals, signal)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over aggregated signals: %w", err)
	}

	sa.logger.WithFields(zaplogrus.Fields{
		"count": len(signals),
		"limit": limit,
	}).Info("Retrieved active aggregated signals")

	return signals, nil
}

// GetAggregatedSignalsBySymbol retrieves active aggregated signals for a specific symbol.
//
// Parameters:
//   - ctx: The context for the operation.
//   - symbol: The trading symbol to filter by.
//   - limit: The maximum number of signals to retrieve.
//
// Returns:
//   - A slice of aggregated signals for the specified symbol, or an error if retrieval fails.
func (sa *SignalAggregator) GetAggregatedSignalsBySymbol(ctx context.Context, symbol string, limit int) ([]*AggregatedSignal, error) {
	if isNilDBPool(sa.db) {
		return []*AggregatedSignal{}, nil
	}

	query := `
		SELECT 
			id, signal_type, symbol, action, strength, confidence,
			profit_potential, risk_level, exchanges, indicators,
			metadata, created_at, expires_at
		FROM aggregated_signals 
		WHERE symbol = $1 
			AND expires_at > NOW() 
			AND confidence >= $2
		ORDER BY confidence DESC, profit_potential DESC, created_at DESC
		LIMIT $3
	`

	rows, err := sa.db.Query(ctx, query, symbol, sa.sigConfig.MinConfidence, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregated signals for symbol %s: %w", symbol, err)
	}
	defer rows.Close()

	var signals []*AggregatedSignal
	for rows.Next() {
		signal := &AggregatedSignal{}
		var exchangesJSON, indicatorsJSON, metadataJSON []byte
		var strengthStr string

		err := rows.Scan(
			&signal.ID, &signal.SignalType, &signal.Symbol, &signal.Action,
			&strengthStr, &signal.Confidence, &signal.ProfitPotential,
			&signal.RiskLevel, &exchangesJSON, &indicatorsJSON,
			&metadataJSON, &signal.CreatedAt, &signal.ExpiresAt,
		)
		if err != nil {
			sa.logger.WithError(err).Error("Failed to scan aggregated signal")
			continue
		}

		// Parse strength
		signal.Strength = SignalStrength(strengthStr)

		// Parse JSON fields
		if len(exchangesJSON) > 0 {
			if err := json.Unmarshal(exchangesJSON, &signal.Exchanges); err != nil {
				sa.logger.WithError(err).Error("Failed to unmarshal exchanges")
			}
		}

		if len(indicatorsJSON) > 0 {
			if err := json.Unmarshal(indicatorsJSON, &signal.Indicators); err != nil {
				sa.logger.WithError(err).Error("Failed to unmarshal indicators")
			}
		}

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &signal.Metadata); err != nil {
				sa.logger.WithError(err).Error("Failed to unmarshal metadata")
			}
		}

		signals = append(signals, signal)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over aggregated signals for symbol %s: %w", symbol, err)
	}

	sa.logger.WithFields(zaplogrus.Fields{
		"symbol": symbol,
		"count":  len(signals),
		"limit":  limit,
	}).Info("Retrieved aggregated signals for symbol")

	return signals, nil
}
