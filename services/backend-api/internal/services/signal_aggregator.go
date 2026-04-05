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
	"github.com/irfndi/neuratrade/pkg/indicators"
	"github.com/shopspring/decimal"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
)

// SignalType represents the type of trading signal
type SignalType string

const (
	SignalTypeArbitrage      SignalType = "arbitrage"
	SignalTypeTechnical      SignalType = "technical"
	SignalTypeMicrostructure SignalType = "microstructure"
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
	Opens      []decimal.Decimal
	Highs      []decimal.Decimal
	Lows       []decimal.Decimal
	Volumes    []decimal.Decimal
	Timestamps []time.Time
}

// ArbitrageSignalInput represents input data required for arbitrage signal generation.
type ArbitrageSignalInput struct {
	Opportunities []models.ArbitrageOpportunity
	MinVolume     decimal.Decimal `json:"min_volume"`
	BaseAmount    decimal.Decimal `json:"base_amount"`
}

type MicrostructureSignalInput struct {
	Symbol           string
	Exchange         string
	ImbalanceSignals []*OrderBookImbalanceSignal
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

// Deprecated: SignalAggregator is the legacy signal aggregation service.
// New code should use scalping.ScalpingSignalComposer for microstructure-aware
// signal composition with per-factor attribution. This service remains for
// backward compatibility and will be removed in a future release.
type SignalAggregator struct {
	config            *config.Config
	db                DBPool
	logger            *zaplogrus.Logger
	sigConfig         SignalAggregatorConfig
	qualityScorer     SignalQualityScorerInterface
	cache             map[string]*AggregatedSignal
	indicatorStack    *indicators.MultiIndicatorStack
	indicatorProvider indicators.IndicatorProvider
}

func NewSignalAggregator(cfg *config.Config, db DBPool, logger *zaplogrus.Logger) *SignalAggregator {
	var provider indicators.IndicatorProvider
	if cfg != nil {
		providerType := indicators.ProviderType(cfg.Indicators.Provider)
		if providerType != "" {
			if p, err := indicators.NewProvider(&indicators.ProviderConfig{Type: providerType}); err == nil {
				provider = p
			} else if logger != nil {
				logger.WithError(err).WithField("provider_type", providerType).Warn("Failed to initialize configured indicator provider; falling back to default provider")
			}
		}
	}
	if provider == nil {
		provider = indicators.NewDefaultProvider()
	}

	return &SignalAggregator{
		config: cfg,
		db:     db,
		logger: logger,
		sigConfig: SignalAggregatorConfig{
			MinConfidence:       decimal.NewFromFloat(0.6),
			MinProfitThreshold:  decimal.NewFromFloat(0.5),
			MaxRiskLevel:        decimal.NewFromFloat(0.3),
			SignalTTL:           15 * time.Minute,
			DeduplicationWindow: 5 * time.Minute,
			MaxSignalsPerSymbol: 3,
		},
		qualityScorer:     NewSignalQualityScorer(cfg, db, logger),
		cache:             make(map[string]*AggregatedSignal),
		indicatorStack:    indicators.NewMultiIndicatorStack(provider, indicators.DefaultIndicatorConfig(), nil),
		indicatorProvider: provider,
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

	if len(input.Prices) < 50 {
		sa.logger.WithFields(zaplogrus.Fields{
			"required_points": 50,
			"actual_points":   len(input.Prices),
		}).Error("Insufficient price data for technical analysis")
		err := fmt.Errorf("insufficient price data for technical analysis: need at least 50 points, got %d", len(input.Prices))
		observability.AddBreadcrumb(spanCtx, "signal_aggregator", "Insufficient data for analysis", sentry.LevelWarning)
		return nil, err
	}

	if len(input.Volumes) < 50 {
		sa.logger.WithFields(zaplogrus.Fields{
			"required_volume_points": 50,
			"actual_volume_points":   len(input.Volumes),
		}).Error("Insufficient volume data for technical analysis")
		err := fmt.Errorf("insufficient volume data for technical analysis: need at least 50 points, got %d", len(input.Volumes))
		observability.AddBreadcrumb(spanCtx, "signal_aggregator", "Insufficient volume data for analysis", sentry.LevelWarning)
		return nil, err
	}

	signals := make([]*AggregatedSignal, 0)
	if sa.indicatorStack != nil && len(input.Opens) == len(input.Prices) && len(input.Highs) == len(input.Prices) && len(input.Lows) == len(input.Prices) {
		ohlcv := &indicators.OHLCVData{
			Symbol:   input.Symbol,
			Exchange: input.Exchange,
			Open:     input.Opens,
			High:     input.Highs,
			Low:      input.Lows,
			Close:    input.Prices,
			Volume:   input.Volumes,
		}

		if len(input.Timestamps) == len(input.Prices) {
			ohlcv.Timestamps = input.Timestamps
		}

		stackResult, err := sa.indicatorStack.Analyze(spanCtx, ohlcv)
		if err != nil {
			sa.logger.WithError(err).Warn("Indicator stack analysis failed; falling back to provider-based indicators")
		} else {
			signals = sa.convertStackResultsToSignals(input.Symbol, input.Exchange, stackResult)
		}
	}

	if len(signals) == 0 {
		indicatorValues := sa.calculateTechnicalIndicators(input.Prices)
		signals = sa.generateTechnicalSignals(input.Symbol, input.Exchange, indicatorValues)
	}

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

func (sa *SignalAggregator) AggregateMicrostructureSignals(ctx context.Context, input MicrostructureSignalInput) ([]*AggregatedSignal, error) {
	if len(input.ImbalanceSignals) == 0 {
		return nil, nil
	}

	_, span := observability.StartSpanWithTags(ctx, observability.SpanOpSignalProcessing, "SignalAggregator.AggregateMicrostructureSignals", map[string]string{
		"signal_type": "microstructure",
		"symbol":      input.Symbol,
		"exchange":    input.Exchange,
		"count":       fmt.Sprintf("%d", len(input.ImbalanceSignals)),
	})
	defer observability.FinishSpan(span, nil)

	var signals []*AggregatedSignal
	for _, obi := range input.ImbalanceSignals {
		if !obi.IsValid() {
			continue
		}

		action := "hold"
		switch obi.Direction {
		case "bullish":
			action = "buy"
		case "bearish":
			action = "sell"
		}

		components := []SignalComponent{
			{
				Indicator:   "orderbook_imbalance",
				Description: fmt.Sprintf("OB imbalance %s %.2f%% (score %.0f)", obi.Direction, obi.ImbalancePct.InexactFloat64(), obi.Score.InexactFloat64()),
				Confidence:  obi.Confidence,
				Strength:    obi.Score.InexactFloat64() / 100.0,
			},
		}

		if obi.SpreadPct.GreaterThan(decimal.Zero) {
			spreadPct := obi.SpreadPct.InexactFloat64()
			minThreshold := 0.01
			maxThreshold := 0.50
			normalizedConfidence := clampFloat((spreadPct-minThreshold)/(maxThreshold-minThreshold), 0, 1)
			spreadConfidence := 0.2 + (normalizedConfidence * 0.7)
			components = append(components, SignalComponent{
				Indicator:   "spread",
				Description: fmt.Sprintf("Spread %.4f%%", obi.SpreadPct.InexactFloat64()),
				Confidence:  decimal.NewFromFloat(spreadConfidence),
				Strength:    spreadConfidence,
			})
		}

		signal := sa.createAggregatedTechnicalSignal(input.Symbol, input.Exchange, action, components)
		signal.SignalType = SignalTypeMicrostructure
		signal.Indicators = []string{"orderbook_imbalance"}
		signal.Metadata["imbalance_pct"] = obi.ImbalancePct.String()
		signal.Metadata["obi_score"] = obi.Score.String()
		signal.Metadata["bid_depth_usd"] = obi.BidDepthUSD.String()
		signal.Metadata["ask_depth_usd"] = obi.AskDepthUSD.String()
		signal.Metadata["spread_pct"] = obi.SpreadPct.String()

		signals = append(signals, signal)
	}

	sa.logger.WithFields(zaplogrus.Fields{
		"symbol":            input.Symbol,
		"exchange":          input.Exchange,
		"signals_generated": len(signals),
	}).Info("Microstructure signal aggregation completed")

	return signals, nil
}

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
func (sa *SignalAggregator) calculateTechnicalIndicators(prices []decimal.Decimal) map[string][]float64 {
	indicators := make(map[string][]float64)

	if len(prices) < 50 || sa.indicatorProvider == nil {
		return indicators
	}

	sma20 := sa.indicatorProvider.SMA(prices, 20)
	if len(sma20) > 0 {
		indicators["sma_20"] = decimalSliceToFloat64(sma20)
	}

	sma50 := sa.indicatorProvider.SMA(prices, 50)
	if len(sma50) > 0 {
		indicators["sma_50"] = decimalSliceToFloat64(sma50)
	}

	ema12 := sa.indicatorProvider.EMA(prices, 12)
	if len(ema12) > 0 {
		indicators["ema_12"] = decimalSliceToFloat64(ema12)
	}

	rsi14 := sa.indicatorProvider.RSI(prices, 14)
	if len(rsi14) > 0 {
		indicators["rsi_14"] = decimalSliceToFloat64(rsi14)
	}

	macdLine, signalLine, _ := sa.indicatorProvider.MACD(prices, 12, 26, 9)
	if len(macdLine) > 0 {
		indicators["macd_line"] = decimalSliceToFloat64(macdLine)
	}
	if len(signalLine) > 0 {
		indicators["macd_signal"] = decimalSliceToFloat64(signalLine)
	}

	return indicators
}

func decimalSliceToFloat64(vals []decimal.Decimal) []float64 {
	result := make([]float64, len(vals))
	for i, v := range vals {
		result[i], _ = v.Float64()
	}
	return result
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

	// Moving Average crossover (Golden/Death Cross: SMA20 vs SMA50)
	if sma20, exists := indicators["sma_20"]; exists && len(sma20) > 1 {
		if sma50, sma50Exists := indicators["sma_50"]; sma50Exists && len(sma50) > 1 {
			currentSMA20 := sma20[len(sma20)-1]
			currentSMA50 := sma50[len(sma50)-1]
			prevSMA20 := sma20[len(sma20)-2]
			prevSMA50 := sma50[len(sma50)-2]

			if currentSMA20 > currentSMA50 && prevSMA20 <= prevSMA50 {
				buySignals = append(buySignals, SignalComponent{
					Indicator:   "golden_cross",
					Description: "SMA20 crossed above SMA50 (Golden Cross)",
					Confidence:  decimal.NewFromFloat(0.8),
					Strength:    0.8,
				})
			}
			if currentSMA20 < currentSMA50 && prevSMA20 >= prevSMA50 {
				sellSignals = append(sellSignals, SignalComponent{
					Indicator:   "death_cross",
					Description: "SMA20 crossed below SMA50 (Death Cross)",
					Confidence:  decimal.NewFromFloat(0.8),
					Strength:    0.8,
				})
			}
		}
	}

	// MACD crossover
	if macdLine, exists := indicators["macd_line"]; exists && len(macdLine) > 1 {
		if macdSignal, signalExists := indicators["macd_signal"]; signalExists && len(macdSignal) > 1 {
			currentMACD := macdLine[len(macdLine)-1]
			currentSignal := macdSignal[len(macdSignal)-1]
			prevMACD := macdLine[len(macdLine)-2]
			prevSignal := macdSignal[len(macdSignal)-2]

			if currentMACD > currentSignal && prevMACD <= prevSignal {
				buySignals = append(buySignals, SignalComponent{
					Indicator:   "macd_bullish",
					Description: "MACD bullish crossover",
					Confidence:  decimal.NewFromFloat(0.75),
					Strength:    0.75,
				})
			}
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

	var aggregatedSignals []*AggregatedSignal
	if len(buySignals) > 0 {
		aggregatedSignals = append(aggregatedSignals, sa.createAggregatedTechnicalSignal(symbol, exchange, "buy", buySignals))
	}
	if len(sellSignals) > 0 {
		aggregatedSignals = append(aggregatedSignals, sa.createAggregatedTechnicalSignal(symbol, exchange, "sell", sellSignals))
	}

	return aggregatedSignals
}

func (sa *SignalAggregator) convertStackResultsToSignals(symbol, exchange string, stackResult *indicators.MultiIndicatorResult) []*AggregatedSignal {
	if stackResult == nil || stackResult.OverallSignal == indicators.SignalHold {
		return nil
	}

	var overallAction string
	switch stackResult.OverallSignal {
	case indicators.SignalBuy:
		overallAction = "buy"
	case indicators.SignalSell:
		overallAction = "sell"
	default:
		return nil
	}

	var components []SignalComponent
	for _, ind := range stackResult.Indicators {
		var action string
		switch ind.Signal {
		case indicators.SignalBuy:
			action = "buy"
		case indicators.SignalSell:
			action = "sell"
		default:
			continue
		}

		if action != overallAction {
			continue
		}

		components = append(components, SignalComponent{
			Indicator:   ind.Name,
			Description: fmt.Sprintf("%s %s signal (strength %.2f)", ind.Name, ind.Signal, ind.Strength.InexactFloat64()),
			Confidence:  ind.Strength,
			Strength:    ind.Strength.InexactFloat64(),
		})
	}

	if len(components) == 0 {
		components = []SignalComponent{{
			Indicator:   "stack_overall",
			Description: fmt.Sprintf("Stack overall %s (confidence %.2f)", stackResult.OverallSignal, stackResult.Confidence.InexactFloat64()),
			Confidence:  stackResult.Confidence,
			Strength:    stackResult.Confidence.InexactFloat64(),
		}}
	}

	return []*AggregatedSignal{sa.createAggregatedTechnicalSignal(symbol, exchange, overallAction, components)}
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

// InvalidateSignalsForSymbol invalidates all active signals for a specific symbol.
// This should be called when new technical analysis contradicts previous signals.
//
// Parameters:
//   - ctx: The context for the operation.
//   - symbol: The trading symbol to invalidate signals for.
//   - reason: The reason for invalidation (e.g., "ta_reversal", "market_condition_change").
//
// Returns:
//   - The number of signals invalidated, or an error if the operation fails.
func (sa *SignalAggregator) InvalidateSignalsForSymbol(ctx context.Context, symbol string, reason string) (int, error) {
	if isNilDBPool(sa.db) {
		return 0, nil
	}

	query := `
		UPDATE aggregated_signals
		SET expires_at = NOW(), metadata = jsonb_set(
			COALESCE(metadata, '{}'::jsonb),
			'{invalidated}',
			jsonb_build_object('reason', $2, 'invalidated_at', NOW())
		)
		WHERE symbol = $1
			AND expires_at > NOW()
			AND (metadata->>'invalidated') IS NULL
	`

	result, err := sa.db.Exec(ctx, query, symbol, reason)
	if err != nil {
		return 0, fmt.Errorf("failed to invalidate signals for %s: %w", symbol, err)
	}

	rowsAffected, _ := result.RowsAffected()

	sa.logger.WithFields(zaplogrus.Fields{
		"symbol":           symbol,
		"reason":           reason,
		"count":            rowsAffected,
		"operation":        "signal_invalidation",
		"operation_result": "success",
	}).Info("Invalidated signals for symbol")

	return int(rowsAffected), nil
}

// InvalidateContradictingSignals invalidates signals that contradict new technical analysis.
// For example, if TA shows strong bearish signal, invalidate previous bullish signals.
//
// Parameters:
//   - ctx: The context for the operation.
//   - symbol: The trading symbol.
//   - newAction: The new signal action ("buy", "sell", "hold").
//   - newConfidence: The confidence of the new signal.
//
// Returns:
//   - The number of signals invalidated, or an error if the operation fails.
func (sa *SignalAggregator) InvalidateContradictingSignals(ctx context.Context, symbol string, newAction string, newConfidence float64) (int, error) {
	if isNilDBPool(sa.db) {
		return 0, nil
	}

	// Determine which actions to invalidate based on new signal
	var contradictingAction string
	switch strings.ToLower(newAction) {
	case "buy", "long":
		contradictingAction = "sell"
	case "sell", "short":
		contradictingAction = "buy"
	default:
		return 0, nil // No contradiction for hold or unknown actions
	}

	// Only invalidate if new signal has sufficient confidence
	if newConfidence < 0.7 {
		return 0, nil
	}

	reason := fmt.Sprintf("ta_contradiction: new %s signal (%.2f confidence)", newAction, newConfidence)

	query := `
		UPDATE aggregated_signals
		SET expires_at = NOW(), metadata = jsonb_set(
			COALESCE(metadata, '{}'::jsonb),
			'{invalidated}',
			jsonb_build_object('reason', $4, 'invalidated_at', NOW(), 'contradicted_by', $5)
		)
		WHERE symbol = $1
			AND action = $2
			AND expires_at > NOW()
			AND confidence < $3
			AND (metadata->>'invalidated') IS NULL
	`

	result, err := sa.db.Exec(ctx, query, symbol, contradictingAction, newConfidence, reason, newAction)
	if err != nil {
		return 0, fmt.Errorf("failed to invalidate contradicting signals: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	if rowsAffected > 0 {
		sa.logger.WithFields(zaplogrus.Fields{
			"symbol":             symbol,
			"new_action":         newAction,
			"new_confidence":     newConfidence,
			"invalidated_action": contradictingAction,
			"count":              rowsAffected,
			"operation":          "signal_contradiction_invalidation",
		}).Info("Invalidated contradicting signals")
	}

	return int(rowsAffected), nil
}

// InvalidateStaleSignals invalidates signals older than a specified duration.
// This helps clean up old signals that may no longer be relevant.
//
// Parameters:
//   - ctx: The context for the operation.
//   - olderThan: Duration after which signals are considered stale.
//
// Returns:
//   - The number of signals invalidated, or an error if the operation fails.
func (sa *SignalAggregator) InvalidateStaleSignals(ctx context.Context, olderThan time.Duration) (int, error) {
	if isNilDBPool(sa.db) {
		return 0, nil
	}

	cutoffTime := time.Now().Add(-olderThan)

	query := `
		UPDATE aggregated_signals
		SET expires_at = NOW(), metadata = jsonb_set(
			COALESCE(metadata, '{}'::jsonb),
			'{invalidated}',
			jsonb_build_object('reason', 'stale_signal', 'invalidated_at', NOW())
		)
		WHERE created_at < $1
			AND expires_at > NOW()
			AND (metadata->>'invalidated') IS NULL
	`

	result, err := sa.db.Exec(ctx, query, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to invalidate stale signals: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	sa.logger.WithFields(zaplogrus.Fields{
		"cutoff_time": cutoffTime.Format(time.RFC3339),
		"count":       rowsAffected,
		"operation":   "stale_signal_cleanup",
	}).Info("Invalidated stale signals")

	return int(rowsAffected), nil
}
