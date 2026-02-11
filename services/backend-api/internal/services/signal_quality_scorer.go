package services

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"

	"github.com/irfandi/celebrum-ai-go/internal/config"
)

// SignalQualityScorer provides signal quality assessment capabilities, evaluating signals based on
// exchange reliability, market conditions, and technical indicators.
type SignalQualityScorer struct {
	config *config.Config
	db     DBPool
	logger *zaplogrus.Logger

	// Cached exchange reliability scores
	exchangeReliabilityCache map[string]*ExchangeReliability
	cacheExpiry              time.Time
}

// ExchangeReliability represents reliability metrics for an exchange, used in scoring signals.
type ExchangeReliability struct {
	ExchangeName     string           `json:"exchange_name"`
	ReliabilityScore decimal.Decimal  `json:"reliability_score"`  // 0.0 to 1.0
	UptimeScore      decimal.Decimal  `json:"uptime_score"`       // 0.0 to 1.0
	VolumeScore      decimal.Decimal  `json:"volume_score"`       // 0.0 to 1.0
	LatencyScore     decimal.Decimal  `json:"latency_score"`      // 0.0 to 1.0
	SpreadScore      decimal.Decimal  `json:"spread_score"`       // 0.0 to 1.0
	DataQualityScore decimal.Decimal  `json:"data_quality_score"` // 0.0 to 1.0
	LastUpdated      time.Time        `json:"last_updated"`
	Metrics          *ExchangeMetrics `json:"metrics"`
}

// ExchangeMetrics contains detailed raw metrics collected for exchange assessment.
type ExchangeMetrics struct {
	TotalTrades      int64           `json:"total_trades"`
	AvgDailyVolume   decimal.Decimal `json:"avg_daily_volume"`
	AvgSpread        decimal.Decimal `json:"avg_spread"`
	AvgLatency       time.Duration   `json:"avg_latency"`
	UptimePercentage decimal.Decimal `json:"uptime_percentage"`
	DataGaps         int             `json:"data_gaps"`
	LastDataUpdate   time.Time       `json:"last_data_update"`
	SupportedPairs   int             `json:"supported_pairs"`
	APIResponseTime  time.Duration   `json:"api_response_time"`
	ErrorRate        decimal.Decimal `json:"error_rate"`
}

// SignalQualityMetrics represents the comprehensive quality assessment scores for a signal.
type SignalQualityMetrics struct {
	OverallScore         decimal.Decimal `json:"overall_score"`          // 0.0 to 1.0
	ExchangeScore        decimal.Decimal `json:"exchange_score"`         // 0.0 to 1.0
	VolumeScore          decimal.Decimal `json:"volume_score"`           // 0.0 to 1.0
	LiquidityScore       decimal.Decimal `json:"liquidity_score"`        // 0.0 to 1.0
	VolatilityScore      decimal.Decimal `json:"volatility_score"`       // 0.0 to 1.0
	TimingScore          decimal.Decimal `json:"timing_score"`           // 0.0 to 1.0
	ConfidenceScore      decimal.Decimal `json:"confidence_score"`       // 0.0 to 1.0
	RiskScore            decimal.Decimal `json:"risk_score"`             // 0.0 to 1.0 (lower is better)
	DataFreshnessScore   decimal.Decimal `json:"data_freshness_score"`   // 0.0 to 1.0
	MarketConditionScore decimal.Decimal `json:"market_condition_score"` // 0.0 to 1.0
}

// SignalQualityInput contains all necessary input data for performing a quality assessment on a signal.
type SignalQualityInput struct {
	SignalType       string                 `json:"signal_type"` // "arbitrage" or "technical"
	Symbol           string                 `json:"symbol"`
	Exchanges        []string               `json:"exchanges"`
	Volume           decimal.Decimal        `json:"volume"`
	ProfitPotential  decimal.Decimal        `json:"profit_potential"`
	Confidence       decimal.Decimal        `json:"confidence"`
	Timestamp        time.Time              `json:"timestamp"`
	Indicators       map[string]interface{} `json:"indicators,omitempty"`
	MarketData       *MarketDataSnapshot    `json:"market_data,omitempty"`
	SignalComponents []string               `json:"signal_components,omitempty"` // List of individual signal indicators for aggregated signals
	SignalCount      int                    `json:"signal_count,omitempty"`      // Number of confirming signals
}

// MarketDataSnapshot represents a snapshot of market conditions at the time of signal generation.
type MarketDataSnapshot struct {
	Price          decimal.Decimal `json:"price"`
	Volume24h      decimal.Decimal `json:"volume_24h"`
	PriceChange24h decimal.Decimal `json:"price_change_24h"`
	Volatility     decimal.Decimal `json:"volatility"`
	Spread         decimal.Decimal `json:"spread"`
	OrderBookDepth decimal.Decimal `json:"order_book_depth"`
	LastTradeTime  time.Time       `json:"last_trade_time"`
}

// QualityThresholds defines minimum acceptable scores for various quality metrics.
type QualityThresholds struct {
	MinOverallScore   decimal.Decimal `json:"min_overall_score"`
	MinExchangeScore  decimal.Decimal `json:"min_exchange_score"`
	MinVolumeScore    decimal.Decimal `json:"min_volume_score"`
	MinLiquidityScore decimal.Decimal `json:"min_liquidity_score"`
	MaxRiskScore      decimal.Decimal `json:"max_risk_score"`
	MinDataFreshness  time.Duration   `json:"min_data_freshness"`
}

// NewSignalQualityScorer creates a new instance of SignalQualityScorer.
//
// Parameters:
//   - cfg: Application configuration.
//   - db: Database connection.
//   - logger: Logger instance.
//
// Returns:
//   - A pointer to the initialized SignalQualityScorer.
func NewSignalQualityScorer(cfg *config.Config, db DBPool, logger *zaplogrus.Logger) *SignalQualityScorer {
	return &SignalQualityScorer{
		config:                   cfg,
		db:                       db,
		logger:                   logger,
		exchangeReliabilityCache: make(map[string]*ExchangeReliability),
		cacheExpiry:              time.Now(),
	}
}

// GetDefaultQualityThresholds returns a default set of quality thresholds.
func (sqs *SignalQualityScorer) GetDefaultQualityThresholds() *QualityThresholds {
	return &QualityThresholds{
		MinOverallScore:   decimal.NewFromFloat(0.6),
		MinExchangeScore:  decimal.NewFromFloat(0.7),
		MinVolumeScore:    decimal.NewFromFloat(0.5),
		MinLiquidityScore: decimal.NewFromFloat(0.6),
		MaxRiskScore:      decimal.NewFromFloat(0.4),
		MinDataFreshness:  5 * time.Minute,
	}
}

// AssessSignalQuality calculates comprehensive quality metrics for a given signal input.
//
// Parameters:
//   - ctx: Context for the operation.
//   - input: Signal data to assess.
//
// Returns:
//   - Calculated quality metrics, or an error if assessment fails.
func (sqs *SignalQualityScorer) AssessSignalQuality(ctx context.Context, input *SignalQualityInput) (*SignalQualityMetrics, error) {
	// Stub logging for telemetry
	_ = fmt.Sprintf("Signal quality assessment: type=%s, symbol=%s, exchanges=%v, volume=%f, profit=%f, confidence=%f",
		input.SignalType, input.Symbol, input.Exchanges, input.Volume.InexactFloat64(),
		input.ProfitPotential.InexactFloat64(), input.Confidence.InexactFloat64())

	sqs.logger.WithFields(zaplogrus.Fields{
		"signal_type": input.SignalType,
		"symbol":      input.Symbol,
		"exchanges":   input.Exchanges,
	}).Info("Assessing signal quality")

	// Ensure exchange reliability cache is fresh
	if err := sqs.refreshExchangeReliabilityCache(ctx); err != nil {
		sqs.logger.WithError(err).Warn("Failed to refresh exchange reliability cache")
	}

	// Calculate individual quality scores
	exchangeScore := sqs.calculateExchangeScore(input.Exchanges)
	volumeScore := sqs.calculateVolumeScore(input)
	liquidityScore := sqs.calculateLiquidityScore(input)
	volatilityScore := sqs.calculateVolatilityScore(input)
	timingScore := sqs.calculateTimingScore(input)
	confidenceScore := input.Confidence
	riskScore := sqs.calculateRiskScore(input)
	dataFreshnessScore := sqs.calculateDataFreshnessScore(input)
	marketConditionScore := sqs.calculateMarketConditionScore(input)

	// Calculate multi-signal bonus for aggregated signals
	multiSignalScore := sqs.calculateMultiSignalScore(input)

	// Calculate overall score using weighted average
	overallScore := sqs.calculateOverallScore(map[string]decimal.Decimal{
		"exchange":         exchangeScore,
		"volume":           volumeScore,
		"liquidity":        liquidityScore,
		"volatility":       volatilityScore,
		"timing":           timingScore,
		"confidence":       confidenceScore,
		"risk":             decimal.NewFromFloat(1.0).Sub(riskScore), // Invert risk score
		"data_freshness":   dataFreshnessScore,
		"market_condition": marketConditionScore,
		"multi_signal":     multiSignalScore,
	})

	// Stub logging for result tracking
	_ = fmt.Sprintf("Quality assessment results: overall=%f, exchange=%f, volume=%f, liquidity=%f, volatility=%f, timing=%f, confidence=%f, risk=%f, freshness=%f, market=%f",
		overallScore.InexactFloat64(), exchangeScore.InexactFloat64(), volumeScore.InexactFloat64(),
		liquidityScore.InexactFloat64(), volatilityScore.InexactFloat64(), timingScore.InexactFloat64(),
		confidenceScore.InexactFloat64(), riskScore.InexactFloat64(), dataFreshnessScore.InexactFloat64(),
		marketConditionScore.InexactFloat64())

	return &SignalQualityMetrics{
		OverallScore:         overallScore,
		ExchangeScore:        exchangeScore,
		VolumeScore:          volumeScore,
		LiquidityScore:       liquidityScore,
		VolatilityScore:      volatilityScore,
		TimingScore:          timingScore,
		ConfidenceScore:      confidenceScore,
		RiskScore:            riskScore,
		DataFreshnessScore:   dataFreshnessScore,
		MarketConditionScore: marketConditionScore,
	}, nil
}

// IsSignalQualityAcceptable determines if signal quality metrics meet the provided thresholds.
//
// Parameters:
//   - metrics: The calculated quality metrics.
//   - thresholds: The criteria for acceptance.
//
// Returns:
//   - True if acceptable, false otherwise.
func (sqs *SignalQualityScorer) IsSignalQualityAcceptable(metrics *SignalQualityMetrics, thresholds *QualityThresholds) bool {
	return metrics.OverallScore.GreaterThanOrEqual(thresholds.MinOverallScore) &&
		metrics.ExchangeScore.GreaterThanOrEqual(thresholds.MinExchangeScore) &&
		metrics.VolumeScore.GreaterThanOrEqual(thresholds.MinVolumeScore) &&
		metrics.LiquidityScore.GreaterThanOrEqual(thresholds.MinLiquidityScore) &&
		metrics.RiskScore.LessThanOrEqual(thresholds.MaxRiskScore)
}

// calculateExchangeScore computes a score based on the reliability of the exchanges involved.
func (sqs *SignalQualityScorer) calculateExchangeScore(exchanges []string) decimal.Decimal {
	if len(exchanges) == 0 {
		return decimal.Zero
	}

	totalScore := decimal.Zero
	validExchanges := 0

	for _, exchange := range exchanges {
		if reliability, exists := sqs.exchangeReliabilityCache[exchange]; exists {
			totalScore = totalScore.Add(reliability.ReliabilityScore)
			validExchanges++
		} else {
			// Default score for unknown exchanges
			totalScore = totalScore.Add(decimal.NewFromFloat(0.5))
			validExchanges++
		}
	}

	if validExchanges == 0 {
		return decimal.Zero
	}

	return totalScore.Div(decimal.NewFromInt(int64(validExchanges)))
}

// calculateVolumeScore evaluates if the trading volume is sufficient for the signal.
func (sqs *SignalQualityScorer) calculateVolumeScore(input *SignalQualityInput) decimal.Decimal {
	if input.Volume.IsZero() {
		return decimal.Zero
	}

	// Define volume thresholds (these could be configurable)
	minVolume := decimal.NewFromFloat(1000)         // $1,000
	goodVolume := decimal.NewFromFloat(10000)       // $10,000
	excellentVolume := decimal.NewFromFloat(100000) // $100,000

	if input.Volume.LessThan(minVolume) {
		return decimal.NewFromFloat(0.2)
	} else if input.Volume.LessThan(goodVolume) {
		// Linear interpolation between 0.2 and 0.7
		ratio := input.Volume.Sub(minVolume).Div(goodVolume.Sub(minVolume))
		return decimal.NewFromFloat(0.2).Add(ratio.Mul(decimal.NewFromFloat(0.5)))
	} else if input.Volume.LessThan(excellentVolume) {
		// Linear interpolation between 0.7 and 1.0
		ratio := input.Volume.Sub(goodVolume).Div(excellentVolume.Sub(goodVolume))
		return decimal.NewFromFloat(0.7).Add(ratio.Mul(decimal.NewFromFloat(0.3)))
	} else {
		return decimal.NewFromFloat(1.0)
	}
}

// calculateLiquidityScore assesses market liquidity based on order book depth and spread.
func (sqs *SignalQualityScorer) calculateLiquidityScore(input *SignalQualityInput) decimal.Decimal {
	if input.MarketData == nil {
		return decimal.NewFromFloat(0.5) // Default score when no market data
	}

	// Assess liquidity based on order book depth and spread
	depthScore := sqs.assessOrderBookDepth(input.MarketData.OrderBookDepth)
	spreadScore := sqs.assessSpread(input.MarketData.Spread, input.MarketData.Price)

	// Weighted average: 60% depth, 40% spread
	return depthScore.Mul(decimal.NewFromFloat(0.6)).Add(spreadScore.Mul(decimal.NewFromFloat(0.4)))
}

// calculateVolatilityScore evaluates if the volatility is within an optimal range for trading.
func (sqs *SignalQualityScorer) calculateVolatilityScore(input *SignalQualityInput) decimal.Decimal {
	if input.MarketData == nil {
		return decimal.NewFromFloat(0.5)
	}

	volatility := input.MarketData.Volatility

	// Optimal volatility range: 2-5% for most strategies
	optimalLow := decimal.NewFromFloat(0.02)  // 2%
	optimalHigh := decimal.NewFromFloat(0.05) // 5%
	tooLow := decimal.NewFromFloat(0.005)     // 0.5%
	tooHigh := decimal.NewFromFloat(0.15)     // 15%

	if volatility.LessThan(tooLow) {
		return decimal.NewFromFloat(0.3) // Too low volatility
	} else if volatility.LessThan(optimalLow) {
		// Increasing score as volatility approaches optimal range
		ratio := volatility.Div(optimalLow)
		return decimal.NewFromFloat(0.3).Add(ratio.Mul(decimal.NewFromFloat(0.5)))
	} else if volatility.LessThanOrEqual(optimalHigh) {
		return decimal.NewFromFloat(0.8) // Optimal range
	} else if volatility.LessThan(tooHigh) {
		// Decreasing score as volatility gets too high
		ratio := decimal.NewFromFloat(1.0).Sub(volatility.Sub(optimalHigh).Div(tooHigh.Sub(optimalHigh)))
		return decimal.NewFromFloat(0.8).Mul(ratio)
	} else {
		return decimal.NewFromFloat(0.2) // Too high volatility
	}
}

// calculateTimingScore assesses how fresh the signal is relative to the current time.
func (sqs *SignalQualityScorer) calculateTimingScore(input *SignalQualityInput) decimal.Decimal {
	now := time.Now()
	age := now.Sub(input.Timestamp)

	// Signal freshness scoring
	if age < time.Minute {
		return decimal.NewFromFloat(1.0) // Very fresh
	} else if age < 5*time.Minute {
		return decimal.NewFromFloat(0.9) // Fresh
	} else if age < 15*time.Minute {
		return decimal.NewFromFloat(0.7) // Acceptable
	} else if age < 30*time.Minute {
		return decimal.NewFromFloat(0.5) // Getting stale
	} else if age < time.Hour {
		return decimal.NewFromFloat(0.3) // Stale
	} else {
		return decimal.NewFromFloat(0.1) // Very stale
	}
}

// calculateRiskScore estimates the risk associated with the signal based on various factors.
func (sqs *SignalQualityScorer) calculateRiskScore(input *SignalQualityInput) decimal.Decimal {
	riskFactors := []decimal.Decimal{}

	// Exchange risk
	exchangeRisk := sqs.calculateExchangeRisk(input.Exchanges)
	riskFactors = append(riskFactors, exchangeRisk)

	// Volume risk (low volume = high risk)
	volumeRisk := decimal.NewFromFloat(1.0).Sub(sqs.calculateVolumeScore(input))
	riskFactors = append(riskFactors, volumeRisk)

	// Volatility risk
	if input.MarketData != nil {
		volatilityRisk := sqs.calculateVolatilityRisk(input.MarketData.Volatility)
		riskFactors = append(riskFactors, volatilityRisk)
	}

	// Timing risk (older signals = higher risk)
	timingRisk := decimal.NewFromFloat(1.0).Sub(sqs.calculateTimingScore(input))
	riskFactors = append(riskFactors, timingRisk)

	// Calculate average risk
	totalRisk := decimal.Zero
	for _, risk := range riskFactors {
		totalRisk = totalRisk.Add(risk)
	}

	return totalRisk.Div(decimal.NewFromInt(int64(len(riskFactors))))
}

// calculateDataFreshnessScore evaluates the recency of the market data used for the signal.
func (sqs *SignalQualityScorer) calculateDataFreshnessScore(input *SignalQualityInput) decimal.Decimal {
	if input.MarketData == nil {
		return decimal.NewFromFloat(0.5)
	}

	now := time.Now()
	age := now.Sub(input.MarketData.LastTradeTime)

	if age < time.Minute {
		return decimal.NewFromFloat(1.0)
	} else if age < 5*time.Minute {
		return decimal.NewFromFloat(0.9)
	} else if age < 15*time.Minute {
		return decimal.NewFromFloat(0.7)
	} else if age < 30*time.Minute {
		return decimal.NewFromFloat(0.5)
	} else {
		return decimal.NewFromFloat(0.2)
	}
}

// calculateMarketConditionScore assesses the overall stability and favorability of market conditions.
func (sqs *SignalQualityScorer) calculateMarketConditionScore(input *SignalQualityInput) decimal.Decimal {
	if input.MarketData == nil {
		return decimal.NewFromFloat(0.5)
	}

	// Assess market stability based on 24h price change
	priceChange := input.MarketData.PriceChange24h.Abs()
	stabilityThreshold := decimal.NewFromFloat(0.1) // 10%

	if priceChange.LessThan(decimal.NewFromFloat(0.02)) {
		return decimal.NewFromFloat(0.6) // Very stable, but maybe too quiet
	} else if priceChange.LessThan(decimal.NewFromFloat(0.05)) {
		return decimal.NewFromFloat(0.8) // Good stability with some movement
	} else if priceChange.LessThan(stabilityThreshold) {
		return decimal.NewFromFloat(0.9) // Optimal movement
	} else if priceChange.LessThan(decimal.NewFromFloat(0.2)) {
		return decimal.NewFromFloat(0.7) // High volatility
	} else {
		return decimal.NewFromFloat(0.4) // Extreme volatility
	}
}

// calculateMultiSignalScore provides a score boost for aggregated signals confirmed by multiple sources.
func (sqs *SignalQualityScorer) calculateMultiSignalScore(input *SignalQualityInput) decimal.Decimal {
	// Base score for single signals
	baseScore := decimal.NewFromFloat(0.5)

	// If no signal count information, return base score
	if input.SignalCount <= 1 {
		return baseScore
	}

	// Calculate bonus based on number of confirming signals
	// Each additional signal adds diminishing returns
	bonusPerSignal := decimal.NewFromFloat(0.15) // 15% bonus per additional signal
	maxBonus := decimal.NewFromFloat(0.4)        // Cap at 40% total bonus

	additionalSignals := input.SignalCount - 1
	totalBonus := decimal.NewFromInt(int64(additionalSignals)).Mul(bonusPerSignal)

	// Apply diminishing returns for many signals
	if additionalSignals > 2 {
		// Reduce bonus for signals beyond the third
		excessSignals := additionalSignals - 2
		reduction := decimal.NewFromFloat(float64(excessSignals) * 0.05) // 5% reduction per excess signal
		totalBonus = totalBonus.Sub(reduction)
	}

	// Cap the bonus
	if totalBonus.GreaterThan(maxBonus) {
		totalBonus = maxBonus
	}

	// Ensure bonus doesn't go negative
	if totalBonus.LessThan(decimal.Zero) {
		totalBonus = decimal.Zero
	}

	finalScore := baseScore.Add(totalBonus)

	// Cap final score at 1.0
	if finalScore.GreaterThan(decimal.NewFromFloat(1.0)) {
		finalScore = decimal.NewFromFloat(1.0)
	}

	return finalScore
}

// Helper functions

// calculateOverallScore computes the weighted average of individual quality scores.
func (sqs *SignalQualityScorer) calculateOverallScore(scores map[string]decimal.Decimal) decimal.Decimal {
	// Define weights for different factors
	weights := map[string]decimal.Decimal{
		"exchange":         decimal.NewFromFloat(0.20),
		"volume":           decimal.NewFromFloat(0.15),
		"liquidity":        decimal.NewFromFloat(0.15),
		"volatility":       decimal.NewFromFloat(0.10),
		"timing":           decimal.NewFromFloat(0.15),
		"confidence":       decimal.NewFromFloat(0.10),
		"risk":             decimal.NewFromFloat(0.10),
		"data_freshness":   decimal.NewFromFloat(0.03),
		"market_condition": decimal.NewFromFloat(0.02),
	}

	weightedSum := decimal.Zero
	totalWeight := decimal.Zero

	for factor, score := range scores {
		if weight, exists := weights[factor]; exists {
			weightedSum = weightedSum.Add(score.Mul(weight))
			totalWeight = totalWeight.Add(weight)
		}
	}

	if totalWeight.IsZero() {
		return decimal.Zero
	}

	return weightedSum.Div(totalWeight)
}

// assessOrderBookDepth normalizes the order book depth into a score.
func (sqs *SignalQualityScorer) assessOrderBookDepth(depth decimal.Decimal) decimal.Decimal {
	// Define depth thresholds
	minDepth := decimal.NewFromFloat(10000)         // $10,000
	goodDepth := decimal.NewFromFloat(100000)       // $100,000
	excellentDepth := decimal.NewFromFloat(1000000) // $1,000,000

	if depth.LessThan(minDepth) {
		return decimal.NewFromFloat(0.2)
	} else if depth.LessThan(goodDepth) {
		ratio := depth.Div(goodDepth)
		return decimal.NewFromFloat(0.2).Add(ratio.Mul(decimal.NewFromFloat(0.5)))
	} else if depth.LessThan(excellentDepth) {
		ratio := depth.Div(excellentDepth)
		return decimal.NewFromFloat(0.7).Add(ratio.Mul(decimal.NewFromFloat(0.3)))
	} else {
		return decimal.NewFromFloat(1.0)
	}
}

// assessSpread normalizes the bid-ask spread into a score.
func (sqs *SignalQualityScorer) assessSpread(spread, price decimal.Decimal) decimal.Decimal {
	if price.IsZero() {
		return decimal.Zero
	}

	// Calculate spread percentage
	spreadPercent := spread.Div(price)

	// Define spread thresholds
	excellentSpread := decimal.NewFromFloat(0.001) // 0.1%
	goodSpread := decimal.NewFromFloat(0.005)      // 0.5%
	acceptableSpread := decimal.NewFromFloat(0.01) // 1%
	poorSpread := decimal.NewFromFloat(0.02)       // 2%

	if spreadPercent.LessThan(excellentSpread) {
		return decimal.NewFromFloat(1.0)
	} else if spreadPercent.LessThan(goodSpread) {
		return decimal.NewFromFloat(0.8)
	} else if spreadPercent.LessThan(acceptableSpread) {
		return decimal.NewFromFloat(0.6)
	} else if spreadPercent.LessThan(poorSpread) {
		return decimal.NewFromFloat(0.4)
	} else {
		return decimal.NewFromFloat(0.2)
	}
}

// calculateExchangeRisk estimates risk based on exchange reliability.
func (sqs *SignalQualityScorer) calculateExchangeRisk(exchanges []string) decimal.Decimal {
	if len(exchanges) == 0 {
		return decimal.NewFromFloat(1.0) // Maximum risk
	}

	totalRisk := decimal.Zero
	for _, exchange := range exchanges {
		if reliability, exists := sqs.exchangeReliabilityCache[exchange]; exists {
			// Higher reliability = lower risk
			exchangeRisk := decimal.NewFromFloat(1.0).Sub(reliability.ReliabilityScore)
			totalRisk = totalRisk.Add(exchangeRisk)
		} else {
			// Unknown exchange = medium-high risk
			totalRisk = totalRisk.Add(decimal.NewFromFloat(0.7))
		}
	}

	return totalRisk.Div(decimal.NewFromInt(int64(len(exchanges))))
}

// calculateVolatilityRisk estimates risk based on market volatility.
func (sqs *SignalQualityScorer) calculateVolatilityRisk(volatility decimal.Decimal) decimal.Decimal {
	// Define volatility risk thresholds
	lowRisk := decimal.NewFromFloat(0.05)    // 5%
	mediumRisk := decimal.NewFromFloat(0.15) // 15%
	highRisk := decimal.NewFromFloat(0.30)   // 30%

	if volatility.LessThan(lowRisk) {
		return decimal.NewFromFloat(0.2)
	} else if volatility.LessThan(mediumRisk) {
		return decimal.NewFromFloat(0.5)
	} else if volatility.LessThan(highRisk) {
		return decimal.NewFromFloat(0.8)
	} else {
		return decimal.NewFromFloat(1.0)
	}
}

// refreshExchangeReliabilityCache updates the internal cache of exchange reliability metrics.
func (sqs *SignalQualityScorer) refreshExchangeReliabilityCache(ctx context.Context) error {
	// Check if cache is still valid (refresh every hour)
	if time.Now().Before(sqs.cacheExpiry) {
		return nil
	}

	sqs.logger.Info("Refreshing exchange reliability cache")

	// Fetch exchange statistics from database
	exchangeStats, err := sqs.fetchExchangeStatistics(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch exchange statistics: %w", err)
	}

	// Calculate reliability scores for each exchange
	newCache := make(map[string]*ExchangeReliability)
	for exchangeName, stats := range exchangeStats {
		reliability := sqs.calculateExchangeReliability(stats)
		newCache[exchangeName] = reliability
	}

	// Update cache
	sqs.exchangeReliabilityCache = newCache
	sqs.cacheExpiry = time.Now().Add(time.Hour)

	sqs.logger.WithField("exchanges_cached", len(newCache)).Info("Exchange reliability cache refreshed")
	return nil
}

// fetchExchangeStatistics retrieves raw exchange statistics from the database (or mock data).
func (sqs *SignalQualityScorer) fetchExchangeStatistics(ctx context.Context) (map[string]*ExchangeMetrics, error) {
	// Query database for exchange statistics
	stats := make(map[string]*ExchangeMetrics)

	// In a real implementation, this would query the database
	// For now, we'll use default values for common exchanges
	defaultStats := map[string]*ExchangeMetrics{
		"binance": {
			TotalTrades:      1000000,
			AvgDailyVolume:   decimal.NewFromFloat(1000000000), // $1B
			AvgSpread:        decimal.NewFromFloat(0.001),      // 0.1%
			AvgLatency:       50 * time.Millisecond,
			UptimePercentage: decimal.NewFromFloat(0.999), // 99.9%
			DataGaps:         5,
			LastDataUpdate:   time.Now().Add(-1 * time.Minute),
			SupportedPairs:   500,
			APIResponseTime:  100 * time.Millisecond,
			ErrorRate:        decimal.NewFromFloat(0.001), // 0.1%
		},
		"coinbase": {
			TotalTrades:      500000,
			AvgDailyVolume:   decimal.NewFromFloat(500000000), // $500M
			AvgSpread:        decimal.NewFromFloat(0.002),     // 0.2%
			AvgLatency:       100 * time.Millisecond,
			UptimePercentage: decimal.NewFromFloat(0.995), // 99.5%
			DataGaps:         10,
			LastDataUpdate:   time.Now().Add(-2 * time.Minute),
			SupportedPairs:   200,
			APIResponseTime:  150 * time.Millisecond,
			ErrorRate:        decimal.NewFromFloat(0.002), // 0.2%
		},
		"kraken": {
			TotalTrades:      300000,
			AvgDailyVolume:   decimal.NewFromFloat(200000000), // $200M
			AvgSpread:        decimal.NewFromFloat(0.003),     // 0.3%
			AvgLatency:       200 * time.Millisecond,
			UptimePercentage: decimal.NewFromFloat(0.990), // 99.0%
			DataGaps:         15,
			LastDataUpdate:   time.Now().Add(-3 * time.Minute),
			SupportedPairs:   150,
			APIResponseTime:  200 * time.Millisecond,
			ErrorRate:        decimal.NewFromFloat(0.005), // 0.5%
		},
	}

	// Copy default stats to the return map
	for exchange, metrics := range defaultStats {
		stats[exchange] = metrics
	}

	return stats, nil
}

// calculateExchangeReliability calculates a normalized reliability score from raw exchange metrics.
func (sqs *SignalQualityScorer) calculateExchangeReliability(metrics *ExchangeMetrics) *ExchangeReliability {
	// Calculate individual scores
	uptimeScore := metrics.UptimePercentage

	// Volume score (normalized)
	volumeScore := sqs.normalizeVolumeScore(metrics.AvgDailyVolume)

	// Latency score (lower latency = higher score)
	latencyScore := sqs.normalizeLatencyScore(metrics.AvgLatency)

	// Spread score (lower spread = higher score)
	spreadScore := sqs.normalizeSpreadScore(metrics.AvgSpread)

	// Data quality score
	dataQualityScore := sqs.calculateDataQualityScore(metrics)

	// Calculate overall reliability score
	reliabilityScore := uptimeScore.Mul(decimal.NewFromFloat(0.3)).
		Add(volumeScore.Mul(decimal.NewFromFloat(0.2))).
		Add(latencyScore.Mul(decimal.NewFromFloat(0.2))).
		Add(spreadScore.Mul(decimal.NewFromFloat(0.15))).
		Add(dataQualityScore.Mul(decimal.NewFromFloat(0.15)))

	return &ExchangeReliability{
		ExchangeName:     "", // Will be set by caller
		ReliabilityScore: reliabilityScore,
		UptimeScore:      uptimeScore,
		VolumeScore:      volumeScore,
		LatencyScore:     latencyScore,
		SpreadScore:      spreadScore,
		DataQualityScore: dataQualityScore,
		LastUpdated:      time.Now(),
		Metrics:          metrics,
	}
}

// normalizeVolumeScore normalizes trading volume to a 0-1 score.
func (sqs *SignalQualityScorer) normalizeVolumeScore(volume decimal.Decimal) decimal.Decimal {
	// Normalize volume to 0-1 scale
	maxVolume := decimal.NewFromFloat(2000000000) // $2B as max reference
	score := volume.Div(maxVolume)
	if score.GreaterThan(decimal.NewFromFloat(1.0)) {
		score = decimal.NewFromFloat(1.0)
	}
	return score
}

// normalizeLatencyScore normalizes latency to a 0-1 score (lower is better).
func (sqs *SignalQualityScorer) normalizeLatencyScore(latency time.Duration) decimal.Decimal {
	// Convert latency to score (lower is better)
	latencyMs := float64(latency.Nanoseconds()) / 1000000 // Convert to milliseconds
	maxLatency := 1000.0                                  // 1 second as max acceptable

	score := 1.0 - (latencyMs / maxLatency)
	if score < 0 {
		score = 0
	}
	return decimal.NewFromFloat(score)
}

// normalizeSpreadScore normalizes spread to a 0-1 score (lower is better).
func (sqs *SignalQualityScorer) normalizeSpreadScore(spread decimal.Decimal) decimal.Decimal {
	// Convert spread to score (lower is better)
	maxSpread := decimal.NewFromFloat(0.01) // 1% as max acceptable

	score := decimal.NewFromFloat(1.0).Sub(spread.Div(maxSpread))
	if score.LessThan(decimal.Zero) {
		score = decimal.Zero
	}
	return score
}

// calculateDataQualityScore computes a score based on data gaps, freshness, and error rates.
func (sqs *SignalQualityScorer) calculateDataQualityScore(metrics *ExchangeMetrics) decimal.Decimal {
	// Calculate data quality based on gaps, freshness, and error rate
	gapScore := decimal.NewFromFloat(1.0)
	if metrics.DataGaps > 0 {
		// Penalize data gaps (max 50 gaps for 0 score)
		gapPenalty := decimal.NewFromFloat(float64(metrics.DataGaps) / 50.0)
		if gapPenalty.GreaterThan(decimal.NewFromFloat(1.0)) {
			gapPenalty = decimal.NewFromFloat(1.0)
		}
		gapScore = decimal.NewFromFloat(1.0).Sub(gapPenalty)
	}

	// Freshness score
	freshnessAge := time.Since(metrics.LastDataUpdate)
	freshnessScore := decimal.NewFromFloat(1.0)
	if freshnessAge > 5*time.Minute {
		// Penalize stale data
		penalty := decimal.NewFromFloat(float64(freshnessAge.Minutes()) / 60.0) // 1 hour = 0 score
		if penalty.GreaterThan(decimal.NewFromFloat(1.0)) {
			penalty = decimal.NewFromFloat(1.0)
		}
		freshnessScore = decimal.NewFromFloat(1.0).Sub(penalty)
	}

	// Error rate score
	errorScore := decimal.NewFromFloat(1.0).Sub(metrics.ErrorRate.Mul(decimal.NewFromFloat(100))) // 1% error = 0 score
	if errorScore.LessThan(decimal.Zero) {
		errorScore = decimal.Zero
	}

	// Weighted average
	return gapScore.Mul(decimal.NewFromFloat(0.4)).
		Add(freshnessScore.Mul(decimal.NewFromFloat(0.4))).
		Add(errorScore.Mul(decimal.NewFromFloat(0.2)))
}

// GetExchangeReliability returns the cached reliability metrics for a specific exchange.
//
// Parameters:
//   - exchangeName: The name of the exchange.
//
// Returns:
//   - The reliability metrics, and a boolean indicating if the exchange was found.
func (sqs *SignalQualityScorer) GetExchangeReliability(exchangeName string) (*ExchangeReliability, bool) {
	reliability, exists := sqs.exchangeReliabilityCache[exchangeName]
	return reliability, exists
}

// GetAllExchangeReliabilities returns all cached exchange reliability metrics.
//
// Returns:
//   - A map of exchange names to reliability metrics.
func (sqs *SignalQualityScorer) GetAllExchangeReliabilities() map[string]*ExchangeReliability {
	// Return a copy to prevent external modification
	result := make(map[string]*ExchangeReliability)
	for k, v := range sqs.exchangeReliabilityCache {
		result[k] = v
	}
	return result
}

// ForceRefreshCache invalidates the current cache and triggers an immediate refresh.
//
// Returns:
//   - An error if the refresh fails.
func (sqs *SignalQualityScorer) ForceRefreshCache(ctx context.Context) error {
	sqs.cacheExpiry = time.Now().Add(-time.Hour) // Force expiry
	return sqs.refreshExchangeReliabilityCache(ctx)
}
