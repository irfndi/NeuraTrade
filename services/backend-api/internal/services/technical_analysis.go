package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/getsentry/sentry-go"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/talib"
	"github.com/shopspring/decimal"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
)

// TechnicalAnalysisService provides technical analysis capabilities, calculating various indicators
// and interpreting them to generate trading signals.
type TechnicalAnalysisService struct {
	config               *config.Config
	db                   DBPool
	logger               *zaplogrus.Logger
	errorRecoveryManager *ErrorRecoveryManager
	resourceManager      *ResourceManager
	performanceMonitor   *PerformanceMonitor
}

// IndicatorResult represents the result of a single technical indicator calculation.
type IndicatorResult struct {
	Name      string            `json:"name"`
	Values    []decimal.Decimal `json:"values"`
	Signal    string            `json:"signal"` // "buy", "sell", "hold"
	Strength  decimal.Decimal   `json:"strength"`
	Timestamp time.Time         `json:"timestamp"`
}

// TechnicalAnalysisResult aggregates all calculated indicators and the overall signal for a symbol.
type TechnicalAnalysisResult struct {
	Symbol        string             `json:"symbol"`
	Exchange      string             `json:"exchange"`
	Timeframe     string             `json:"timeframe"`
	Indicators    []*IndicatorResult `json:"indicators"`
	OverallSignal string             `json:"overall_signal"`
	Confidence    decimal.Decimal    `json:"confidence"`
	CalculatedAt  time.Time          `json:"calculated_at"`
}

// PriceData holds the historical price and volume data used for analysis.
type PriceData struct {
	Symbol     string            `json:"symbol"`
	Exchange   string            `json:"exchange"`
	Open       []decimal.Decimal `json:"open"`
	High       []decimal.Decimal `json:"high"`
	Low        []decimal.Decimal `json:"low"`
	Close      []decimal.Decimal `json:"close"`
	Volume     []decimal.Decimal `json:"volume"`
	Timestamps []time.Time       `json:"timestamps"`
}

// IndicatorConfig allows configuration of parameters for various technical indicators.
type IndicatorConfig struct {
	// Moving Averages
	SMAPeriods []int `json:"sma_periods"`
	EMAPeriods []int `json:"ema_periods"`

	// Momentum Indicators
	RSIPeriod    int `json:"rsi_period"`
	StochKPeriod int `json:"stoch_k_period"`
	StochDPeriod int `json:"stoch_d_period"`

	// Trend Indicators
	MACDFast   int `json:"macd_fast"`
	MACDSlow   int `json:"macd_slow"`
	MACDSignal int `json:"macd_signal"`

	// Volatility Indicators
	BBPeriod  int     `json:"bb_period"`
	BBStdDev  float64 `json:"bb_std_dev"`
	ATRPeriod int     `json:"atr_period"`

	// Volume Indicators
	OBVEnabled  bool `json:"obv_enabled"`
	VWAPEnabled bool `json:"vwap_enabled"`
}

// NewTechnicalAnalysisService creates a new instance of TechnicalAnalysisService.
//
// Parameters:
//   - cfg: Application configuration.
//   - db: Database connection.
//   - logger: Logger instance.
//   - errorRecoveryManager: Service for error recovery.
//   - resourceManager: Service for resource management.
//   - performanceMonitor: Service for performance monitoring.
//
// Returns:
//   - A pointer to the initialized TechnicalAnalysisService.
func NewTechnicalAnalysisService(
	cfg *config.Config,
	db DBPool,
	logger *zaplogrus.Logger,
	errorRecoveryManager *ErrorRecoveryManager,
	resourceManager *ResourceManager,
	performanceMonitor *PerformanceMonitor,
) *TechnicalAnalysisService {
	return &TechnicalAnalysisService{
		config:               cfg,
		db:                   db,
		logger:               logger,
		errorRecoveryManager: errorRecoveryManager,
		resourceManager:      resourceManager,
		performanceMonitor:   performanceMonitor,
	}
}

// GetDefaultIndicatorConfig returns a standard configuration for technical indicators.
func (tas *TechnicalAnalysisService) GetDefaultIndicatorConfig() *IndicatorConfig {
	return &IndicatorConfig{
		SMAPeriods:   []int{10, 20, 50},
		EMAPeriods:   []int{12, 26},
		RSIPeriod:    14,
		StochKPeriod: 14,
		StochDPeriod: 3,
		MACDFast:     12,
		MACDSlow:     26,
		MACDSignal:   9,
		BBPeriod:     20,
		BBStdDev:     2.0,
		ATRPeriod:    14,
		OBVEnabled:   true,
		VWAPEnabled:  true,
	}
}

// AnalyzeSymbol performs a comprehensive technical analysis on a specific symbol and exchange.
// It fetches historical data, calculates indicators, and determines an overall trading signal.
//
// Parameters:
//   - ctx: Context for the operation.
//   - symbol: The trading symbol to analyze.
//   - exchange: The exchange where the symbol is traded.
//   - config: Configuration for the indicators.
//
// Returns:
//   - A TechnicalAnalysisResult containing all calculated data, or an error if analysis fails.
func (tas *TechnicalAnalysisService) AnalyzeSymbol(ctx context.Context, symbol, exchange string, config *IndicatorConfig) (*TechnicalAnalysisResult, error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpTechnicalAnalys, "TechnicalAnalysisService.AnalyzeSymbol", map[string]string{
		"symbol":   symbol,
		"exchange": exchange,
	})
	defer func() {
		observability.RecoverAndCapture(spanCtx, "AnalyzeSymbol")
	}()

	observability.AddBreadcrumb(spanCtx, "technical_analysis", "Starting technical analysis", sentry.LevelInfo)

	// Log analysis start with structured logging
	tas.logger.WithFields(zaplogrus.Fields{
		"symbol":    symbol,
		"exchange":  exchange,
		"operation": "technical_analysis_start",
	}).Debug("Starting technical analysis calculation")

	// Register operation with resource manager
	operationID := fmt.Sprintf("technical_analysis_%s_%s_%d", symbol, exchange, time.Now().UnixNano())
	tas.resourceManager.RegisterResource(operationID, GoroutineResource, func() error {
		return nil
	}, map[string]interface{}{"symbol": symbol, "exchange": exchange, "operation": "technical_analysis"})
	defer func() {
		if err := tas.resourceManager.CleanupResource(operationID); err != nil {
			tas.logger.WithFields(zaplogrus.Fields{"operation_id": operationID}).Warnf("Failed to cleanup resource: %v", err)
		}
	}()

	// Create timeout context for analysis
	analysisCtx, cancel := context.WithTimeout(spanCtx, 5*time.Minute)
	defer cancel()

	tas.logger.WithFields(zaplogrus.Fields{
		"symbol":   symbol,
		"exchange": exchange,
	}).Info("Starting technical analysis")

	// Fetch price data from database with error recovery
	var priceData *PriceData
	err := tas.errorRecoveryManager.ExecuteWithRetry(analysisCtx, "fetch_price_data", func() error {
		var err error
		priceData, err = tas.fetchPriceData(analysisCtx, symbol, exchange)
		return err
	})
	if err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "fetch_price_data", map[string]interface{}{
			"symbol":   symbol,
			"exchange": exchange,
		})
		observability.FinishSpan(span, err)
		return nil, fmt.Errorf("failed to fetch price data: %w", err)
	}

	if len(priceData.Close) < 50 {
		dataErr := fmt.Errorf("insufficient price data: need at least 50 points, got %d", len(priceData.Close))
		observability.AddBreadcrumb(spanCtx, "technical_analysis", "Insufficient price data", sentry.LevelWarning)
		// Log insufficient data error with structured logging
		tas.logger.WithFields(zaplogrus.Fields{
			"symbol":          symbol,
			"exchange":        exchange,
			"data_points":     len(priceData.Close),
			"required_points": 50,
			"operation":       "technical_analysis",
		}).Warn("Insufficient price data for technical analysis")
		return nil, dataErr
	}

	// Log price data summary with structured logging
	tas.logger.WithFields(zaplogrus.Fields{
		"symbol":      symbol,
		"exchange":    exchange,
		"data_points": len(priceData.Close),
		"operation":   "technical_analysis",
	}).Debug("Successfully fetched price data")

	// Convert to float slices for go-talib
	open, high, low, close, volume := tas.convertPriceDataToFloats(priceData)

	// Calculate all indicators with error recovery
	var indicators []*IndicatorResult
	calcErr := tas.errorRecoveryManager.ExecuteWithRetry(ctx, "calculate_indicators", func() error {
		indicators = tas.calculateAllIndicators(open, high, low, close, volume, config)
		return nil
	})
	if calcErr != nil {
		// Log indicator calculation error with structured logging
		tas.logger.WithFields(zaplogrus.Fields{
			"symbol":    symbol,
			"exchange":  exchange,
			"error":     calcErr.Error(),
			"operation": "technical_analysis",
		}).Error("Failed to calculate technical indicators")
		return nil, fmt.Errorf("failed to calculate indicators: %w", calcErr)
	}

	// Determine overall signal and confidence
	overallSignal, confidence := tas.determineOverallSignal(indicators)

	// Log analysis completion with structured logging
	tas.logger.WithFields(zaplogrus.Fields{
		"symbol":         symbol,
		"exchange":       exchange,
		"indicators":     len(indicators),
		"overall_signal": overallSignal,
		"confidence":     confidence.InexactFloat64(),
		"operation":      "technical_analysis",
	}).Info("Technical analysis completed successfully")

	return &TechnicalAnalysisResult{
		Symbol:        symbol,
		Exchange:      exchange,
		Timeframe:     "1h", // Default timeframe
		Indicators:    indicators,
		OverallSignal: overallSignal,
		Confidence:    confidence,
		CalculatedAt:  time.Now(),
	}, nil
}

// calculateAllIndicators orchestrates the calculation of all enabled technical indicators.
func (tas *TechnicalAnalysisService) calculateAllIndicators(open, high, low, close, volume []float64, config *IndicatorConfig) []*IndicatorResult {
	// Log indicator calculation start with structured logging
	tas.logger.WithFields(zaplogrus.Fields{
		"data_points": len(close),
		"operation":   "calculate_indicators",
	}).Debug("Starting technical indicator calculations")

	var indicators []*IndicatorResult

	// Moving Averages
	for _, period := range config.SMAPeriods {
		if result := tas.calculateSMA(close, period); result != nil {
			indicators = append(indicators, result)
		}
	}

	for _, period := range config.EMAPeriods {
		if result := tas.calculateEMA(close, period); result != nil {
			indicators = append(indicators, result)
		}
	}

	// Momentum Indicators
	if result := tas.calculateRSI(close, config.RSIPeriod); result != nil {
		indicators = append(indicators, result)
	}

	if result := tas.calculateStochastic(high, low, close, config.StochKPeriod, config.StochDPeriod); result != nil {
		indicators = append(indicators, result)
	}

	// Trend Indicators
	if result := tas.calculateMACD(close, config.MACDFast, config.MACDSlow, config.MACDSignal); result != nil {
		indicators = append(indicators, result)
	}

	// Volatility Indicators
	if result := tas.calculateBollingerBands(close, config.BBPeriod, config.BBStdDev); result != nil {
		indicators = append(indicators, result)
	}

	if result := tas.calculateATR(high, low, close, config.ATRPeriod); result != nil {
		indicators = append(indicators, result)
	}

	// Volume Indicators
	if config.OBVEnabled {
		if result := tas.calculateOBV(close, volume); result != nil {
			indicators = append(indicators, result)
		}
	}

	// Log indicator calculation completion with structured logging
	tas.logger.WithFields(zaplogrus.Fields{
		"indicators_count": len(indicators),
		"operation":        "calculate_indicators",
	}).Debug("Technical indicator calculations completed")

	return indicators
}

// calculateSMA computes the Simple Moving Average and interprets it for signals.
func (tas *TechnicalAnalysisService) calculateSMA(prices []float64, period int) *IndicatorResult {
	if len(prices) < period {
		return nil
	}

	// Calculate SMA
	result := talib.Sma(prices, period)

	// Convert to decimal and determine signal
	values := make([]decimal.Decimal, len(result))
	for i, val := range result {
		values[i] = decimal.NewFromFloat(val)
	}

	signal, strength := tas.analyzeSMASignal(prices, result, period)

	return &IndicatorResult{
		Name:      fmt.Sprintf("SMA_%d", period),
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}
}

// calculateEMA computes the Exponential Moving Average and interprets it for signals.
func (tas *TechnicalAnalysisService) calculateEMA(prices []float64, period int) *IndicatorResult {
	if len(prices) < period {
		return nil
	}

	// Calculate EMA
	result := talib.Ema(prices, period)

	values := make([]decimal.Decimal, len(result))
	for i, val := range result {
		values[i] = decimal.NewFromFloat(val)
	}

	signal, strength := tas.analyzeEMASignal(prices, result, period)

	return &IndicatorResult{
		Name:      fmt.Sprintf("EMA_%d", period),
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}
}

// calculateRSI computes the Relative Strength Index and interprets it for signals.
func (tas *TechnicalAnalysisService) calculateRSI(prices []float64, period int) *IndicatorResult {
	if len(prices) < period+1 {
		return nil
	}

	// Calculate RSI
	result := talib.Rsi(prices, period)

	values := make([]decimal.Decimal, len(result))
	for i, val := range result {
		values[i] = decimal.NewFromFloat(val)
	}

	signal, strength := tas.analyzeRSISignal(result)

	return &IndicatorResult{
		Name:      fmt.Sprintf("RSI_%d", period),
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}
}

// calculateMACD computes the Moving Average Convergence Divergence and interprets it for signals.
func (tas *TechnicalAnalysisService) calculateMACD(prices []float64, fastPeriod, slowPeriod, signalPeriod int) *IndicatorResult {
	if len(prices) < slowPeriod+signalPeriod {
		return nil
	}

	// Calculate MACD with configurable periods
	// Macd returns macd, signal, hist
	macdLine, _, _ := talib.Macd(prices, fastPeriod, slowPeriod, signalPeriod)

	result := macdLine

	values := make([]decimal.Decimal, len(result))
	for i, val := range result {
		values[i] = decimal.NewFromFloat(val)
	}

	signal, strength := tas.analyzeMACDSignal(result)

	return &IndicatorResult{
		Name:      "MACD",
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}
}

// calculateBollingerBands computes Bollinger Bands and interprets them for signals.
func (tas *TechnicalAnalysisService) calculateBollingerBands(prices []float64, period int, stdDev float64) *IndicatorResult {
	if len(prices) < period {
		return nil
	}

	// Bollinger Bands calculation with configurable standard deviation
	// Bbands(inReal []float64, optInTimePeriod int, optInNbDevUp float64, optInNbDevDn float64, optInMatType int)
	upperBand, middleBand, lowerBand := talib.BBands(prices, period, stdDev, stdDev, talib.SMA)

	// Use middle band (SMA) as the primary value to return (as per original logic, though returning all 3 would be better in future)
	// Original logic returned SMA values as "Values"
	values := make([]decimal.Decimal, len(middleBand))
	for i, val := range middleBand {
		values[i] = decimal.NewFromFloat(val)
	}

	signal, strength := tas.analyzeBollingerBandsSignal(prices, middleBand, upperBand, lowerBand, period)

	return &IndicatorResult{
		Name:      "BB",
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}
}

// calculateStandardDeviation computes the standard deviation for a given slice of prices.
// Deprecated: used by old implementation, kept if needed but not used by talib BB
//
//nolint:unused // Kept for potential future use
func (tas *TechnicalAnalysisService) calculateStandardDeviation(window []float64, mean float64) float64 {
	if len(window) == 0 {
		return 0
	}

	// Calculate variance
	variance := 0.0
	for _, price := range window {
		diff := price - mean
		variance += diff * diff
	}
	variance /= float64(len(window))

	// Return standard deviation
	return math.Sqrt(variance)
}

// calculateATR computes the Average True Range for volatility measurement.
func (tas *TechnicalAnalysisService) calculateATR(high, low, close []float64, period int) *IndicatorResult {
	if len(high) < period || len(low) < period || len(close) < period {
		return nil
	}

	// Calculate ATR
	result := talib.Atr(high, low, close, period)

	values := make([]decimal.Decimal, len(result))
	for i, val := range result {
		values[i] = decimal.NewFromFloat(val)
	}

	return &IndicatorResult{
		Name:      fmt.Sprintf("ATR_%d", period),
		Values:    values,
		Signal:    "hold", // ATR is primarily for volatility measurement
		Strength:  decimal.NewFromFloat(0.5),
		Timestamp: time.Now(),
	}
}

// calculateStochastic computes the Stochastic Oscillator and interprets it for signals.
func (tas *TechnicalAnalysisService) calculateStochastic(high, low, close []float64, kPeriod, dPeriod int) *IndicatorResult {
	if len(high) < kPeriod || len(low) < kPeriod || len(close) < kPeriod {
		return nil
	}

	// StochF returns FastK and FastD. FastD is what we want (it's the smoothed K).
	// Equivalently Stoch(..., kPeriod, dPeriod, SMA, dPeriod, SMA) returns SlowK and SlowD.
	// Original code calc method:
	// 1. Calc Fast %K.
	// 2. Calc SMA of Fast %K (len dPeriod).
	// This maps to talib.StochF returning the second value (FastD).

	_, fastD := talib.StochF(high, low, close, kPeriod, dPeriod, talib.SMA)

	// Use FastD as the result
	values := make([]decimal.Decimal, len(fastD))
	for i, val := range fastD {
		values[i] = decimal.NewFromFloat(val)
	}

	signal, strength := tas.analyzeStochasticSignal(fastD)

	return &IndicatorResult{
		Name:      "STOCH",
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}
}

// calculateOBV computes On-Balance Volume and interprets it for signals.
func (tas *TechnicalAnalysisService) calculateOBV(prices, volumes []float64) *IndicatorResult {
	if len(prices) != len(volumes) || len(prices) < 2 {
		return nil
	}

	// Calculate OBV
	result := talib.Obv(prices, volumes)

	values := make([]decimal.Decimal, len(result))
	for i, val := range result {
		values[i] = decimal.NewFromFloat(val)
	}

	signal, strength := tas.analyzeOBVSignal(result, prices)

	return &IndicatorResult{
		Name:      "OBV",
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}
}

func (tas *TechnicalAnalysisService) convertPriceDataToFloats(data *PriceData) ([]float64, []float64, []float64, []float64, []float64) {
	length := len(data.Close)
	open := make([]float64, length)
	high := make([]float64, length)
	low := make([]float64, length)
	close := make([]float64, length)
	volume := make([]float64, length)

	for i := 0; i < length; i++ {
		open[i], _ = data.Open[i].Float64()
		high[i], _ = data.High[i].Float64()
		low[i], _ = data.Low[i].Float64()
		close[i], _ = data.Close[i].Float64()
		volume[i], _ = data.Volume[i].Float64()
	}
	return open, high, low, close, volume
}

// Signal analysis functions

// analyzeSMASignal determines the signal based on SMA crossovers and price position.
func (tas *TechnicalAnalysisService) analyzeSMASignal(prices, sma []float64, period int) (string, decimal.Decimal) {
	if len(prices) < 2 || len(sma) < 2 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	currentPrice := prices[len(prices)-1]
	currentSMA := sma[len(sma)-1]
	prevPrice := prices[len(prices)-2]
	prevSMA := sma[len(sma)-2]

	// Calculate distance from SMA as percentage
	distanceFromSMA := math.Abs(currentPrice-currentSMA) / currentSMA

	// Adjust signal strength based on period (longer periods = stronger signals)
	periodMultiplier := math.Min(1.5, float64(period)/20.0) // Scale based on period

	// Price crossing above SMA
	if currentPrice > currentSMA && prevPrice <= prevSMA {
		strength := math.Min(0.8, 0.6+(distanceFromSMA*periodMultiplier))
		return "buy", decimal.NewFromFloat(strength)
	}
	// Price crossing below SMA
	if currentPrice < currentSMA && prevPrice >= prevSMA {
		strength := math.Min(0.8, 0.6+(distanceFromSMA*periodMultiplier))
		return "sell", decimal.NewFromFloat(strength)
	}

	// Price above SMA (bullish) - adjust strength based on period and distance
	if currentPrice > currentSMA {
		strength := math.Min(0.7, 0.4+(distanceFromSMA*periodMultiplier))
		return "buy", decimal.NewFromFloat(strength)
	}
	// Price below SMA (bearish) - adjust strength based on period and distance
	if currentPrice < currentSMA {
		strength := math.Min(0.7, 0.4+(distanceFromSMA*periodMultiplier))
		return "sell", decimal.NewFromFloat(strength)
	}

	return "hold", decimal.NewFromFloat(0.5)
}

// analyzeEMASignal determines the signal based on EMA crossovers and price position.
func (tas *TechnicalAnalysisService) analyzeEMASignal(prices, ema []float64, period int) (string, decimal.Decimal) {
	// Similar logic to SMA but with higher sensitivity
	if len(prices) < 2 || len(ema) < 2 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	currentPrice := prices[len(prices)-1]
	currentEMA := ema[len(ema)-1]
	prevPrice := prices[len(prices)-2]
	prevEMA := ema[len(ema)-2]

	// Calculate distance from EMA as percentage
	distanceFromEMA := math.Abs(currentPrice-currentEMA) / currentEMA

	// EMA is more sensitive than SMA, so use higher base strength
	// Adjust signal strength based on period
	periodMultiplier := math.Min(1.3, float64(period)/15.0) // EMA periods typically shorter

	// Price crossing above EMA (stronger signal due to EMA sensitivity)
	if currentPrice > currentEMA && prevPrice <= prevEMA {
		strength := math.Min(0.85, 0.7+(distanceFromEMA*periodMultiplier))
		return "buy", decimal.NewFromFloat(strength)
	}
	// Price crossing below EMA (stronger signal due to EMA sensitivity)
	if currentPrice < currentEMA && prevPrice >= prevEMA {
		strength := math.Min(0.85, 0.7+(distanceFromEMA*periodMultiplier))
		return "sell", decimal.NewFromFloat(strength)
	}
	// Price above EMA (bullish)
	if currentPrice > currentEMA {
		strength := math.Min(0.75, 0.5+(distanceFromEMA*periodMultiplier))
		return "buy", decimal.NewFromFloat(strength)
	}
	// Price below EMA (bearish)
	if currentPrice < currentEMA {
		strength := math.Min(0.75, 0.5+(distanceFromEMA*periodMultiplier))
		return "sell", decimal.NewFromFloat(strength)
	}

	return "hold", decimal.NewFromFloat(0.5)
}

// analyzeRSISignal determines the signal based on RSI levels (overbought/oversold).
func (tas *TechnicalAnalysisService) analyzeRSISignal(rsi []float64) (string, decimal.Decimal) {
	if len(rsi) == 0 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	currentRSI := rsi[len(rsi)-1]

	if currentRSI < 30 {
		return "buy", decimal.NewFromFloat(0.8) // Oversold
	}
	if currentRSI > 70 {
		return "sell", decimal.NewFromFloat(0.8) // Overbought
	}
	if currentRSI < 40 {
		return "buy", decimal.NewFromFloat(0.6)
	}
	if currentRSI > 60 {
		return "sell", decimal.NewFromFloat(0.6)
	}

	return "hold", decimal.NewFromFloat(0.5)
}

// analyzeMACDSignal determines the signal based on MACD crossovers and position.
func (tas *TechnicalAnalysisService) analyzeMACDSignal(macd []float64) (string, decimal.Decimal) {
	if len(macd) < 2 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	current := macd[len(macd)-1]
	previous := macd[len(macd)-2]

	// MACD crossing above zero
	if current > 0 && previous <= 0 {
		return "buy", decimal.NewFromFloat(0.8)
	}
	// MACD crossing below zero
	if current < 0 && previous >= 0 {
		return "sell", decimal.NewFromFloat(0.8)
	}
	// MACD above zero (bullish)
	if current > 0 {
		return "buy", decimal.NewFromFloat(0.6)
	}
	// MACD below zero (bearish)
	if current < 0 {
		return "sell", decimal.NewFromFloat(0.6)
	}

	return "hold", decimal.NewFromFloat(0.5)
}

// analyzeBollingerBandsSignal determines the signal based on price position relative to Bollinger Bands.
func (tas *TechnicalAnalysisService) analyzeBollingerBandsSignal(prices, smaValues, upperBand, lowerBand []float64, period int) (string, decimal.Decimal) {
	// Implement actual Bollinger Bands signal analysis
	if len(prices) == 0 || len(smaValues) == 0 || len(upperBand) == 0 || len(lowerBand) == 0 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	currentPrice := prices[len(prices)-1]
	currentSMA := smaValues[len(smaValues)-1]
	currentUpper := upperBand[len(upperBand)-1]
	currentLower := lowerBand[len(lowerBand)-1]

	// Calculate band width and position
	bandWidth := currentUpper - currentLower
	distanceFromSMA := math.Abs(currentPrice - currentSMA)
	positionInBand := (currentPrice - currentLower) / bandWidth

	// Adjust signal strength based on period (longer periods = more reliable signals)
	periodMultiplier := math.Min(1.4, float64(period)/25.0) // BB typically use longer periods

	// Price near lower band (potential buy) - stronger signal for longer periods
	if currentPrice <= currentLower*1.02 {
		strength := math.Min(0.8, 0.6+(periodMultiplier*0.2))
		return "buy", decimal.NewFromFloat(strength)
	}

	// Price near upper band (potential sell) - stronger signal for longer periods
	if currentPrice >= currentUpper*0.98 {
		strength := math.Min(0.8, 0.6+(periodMultiplier*0.2))
		return "sell", decimal.NewFromFloat(strength)
	}

	// Price near middle band (hold) - very neutral signal
	if distanceFromSMA < bandWidth*0.1 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	// Price between bands - use position and period to determine signal
	if positionInBand < 0.3 {
		// Lower half of bands - slightly bearish unless momentum suggests otherwise
		strength := math.Min(0.6, 0.4+(periodMultiplier*0.15))
		return "sell", decimal.NewFromFloat(strength)
	} else if positionInBand > 0.7 {
		// Upper half of bands - slightly bullish unless momentum suggests otherwise
		strength := math.Min(0.6, 0.4+(periodMultiplier*0.15))
		return "buy", decimal.NewFromFloat(strength)
	} else {
		// Middle range - neutral
		return "hold", decimal.NewFromFloat(0.5)
	}
}

// analyzeStochasticSignal determines the signal based on Stochastic Oscillator levels.
func (tas *TechnicalAnalysisService) analyzeStochasticSignal(stoch []float64) (string, decimal.Decimal) {
	if len(stoch) == 0 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	current := stoch[len(stoch)-1]

	if current < 20 {
		return "buy", decimal.NewFromFloat(0.75) // Oversold
	}
	if current > 80 {
		return "sell", decimal.NewFromFloat(0.75) // Overbought
	}

	return "hold", decimal.NewFromFloat(0.5)
}

// analyzeOBVSignal determines the signal based on OBV trend confirmation.
func (tas *TechnicalAnalysisService) analyzeOBVSignal(obv, prices []float64) (string, decimal.Decimal) {
	if len(obv) < 2 || len(prices) < 2 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	currentOBV := obv[len(obv)-1]
	prevOBV := obv[len(obv)-2]
	currentPrice := prices[len(prices)-1]
	prevPrice := prices[len(prices)-2]

	// Volume confirming price trend
	if currentPrice > prevPrice && currentOBV > prevOBV {
		return "buy", decimal.NewFromFloat(0.7)
	}
	if currentPrice < prevPrice && currentOBV < prevOBV {
		return "sell", decimal.NewFromFloat(0.7)
	}

	return "hold", decimal.NewFromFloat(0.5)
}

// determineOverallSignal aggregates signals from all indicators to form a consensus.
func (tas *TechnicalAnalysisService) determineOverallSignal(indicators []*IndicatorResult) (string, decimal.Decimal) {
	if len(indicators) == 0 {
		return "hold", decimal.NewFromFloat(0.5)
	}

	buyScore := decimal.Zero
	sellScore := decimal.Zero
	totalWeight := decimal.Zero

	for _, indicator := range indicators {
		weight := indicator.Strength
		totalWeight = totalWeight.Add(weight)

		switch indicator.Signal {
		case "buy":
			buyScore = buyScore.Add(weight)
		case "sell":
			sellScore = sellScore.Add(weight)
		}
	}

	if totalWeight.IsZero() {
		return "hold", decimal.NewFromFloat(0.5)
	}

	buyRatio := buyScore.Div(totalWeight)
	sellRatio := sellScore.Div(totalWeight)

	// Determine overall signal
	if buyRatio.GreaterThan(decimal.NewFromFloat(0.6)) {
		return "buy", buyRatio
	}
	if sellRatio.GreaterThan(decimal.NewFromFloat(0.6)) {
		return "sell", sellRatio
	}

	return "hold", decimal.NewFromFloat(0.5)
}

// Helper functions

// fetchPriceData retrieves historical price and volume data for a symbol from the database.
func (tas *TechnicalAnalysisService) fetchPriceData(ctx context.Context, symbol, exchange string) (*PriceData, error) {
	// Fetch market data from database
	var marketData []models.MarketData
	query := `SELECT last_price, volume_24h, timestamp FROM market_data 
			 WHERE trading_pair_id IN (SELECT id FROM trading_pairs WHERE symbol = $1) 
			 AND exchange_id IN (SELECT id FROM exchanges WHERE name = $2) 
			 ORDER BY timestamp DESC LIMIT 200`
	rows, err := tas.db.Query(ctx, query, symbol, exchange)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch market data: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var md models.MarketData
		scanErr := rows.Scan(&md.LastPrice, &md.Volume24h, &md.Timestamp)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan market data: %w", scanErr)
		}
		marketData = append(marketData, md)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("error iterating market data rows: %w", rowsErr)
	}

	if len(marketData) == 0 {
		return nil, fmt.Errorf("no market data found for %s on %s", symbol, exchange)
	}

	// Reverse to get chronological order
	for i, j := 0, len(marketData)-1; i < j; i, j = i+1, j-1 {
		marketData[i], marketData[j] = marketData[j], marketData[i]
	}

	// Convert to PriceData
	priceData := &PriceData{
		Symbol:     symbol,
		Exchange:   exchange,
		Open:       make([]decimal.Decimal, len(marketData)),
		High:       make([]decimal.Decimal, len(marketData)),
		Low:        make([]decimal.Decimal, len(marketData)),
		Close:      make([]decimal.Decimal, len(marketData)),
		Volume:     make([]decimal.Decimal, len(marketData)),
		Timestamps: make([]time.Time, len(marketData)),
	}

	for i, data := range marketData {
		priceData.Open[i] = data.LastPrice // Using last price as OHLC for now
		priceData.High[i] = data.LastPrice
		priceData.Low[i] = data.LastPrice
		priceData.Close[i] = data.LastPrice
		priceData.Volume[i] = data.Volume24h
		priceData.Timestamps[i] = data.Timestamp
	}

	return priceData, nil
}
