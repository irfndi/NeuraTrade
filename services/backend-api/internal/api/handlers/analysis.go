package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
	"github.com/irfandi/celebrum-ai-go/internal/services"
)

// AnalysisHandler manages technical analysis and signal generation endpoints.
type AnalysisHandler struct {
	db          DBQuerier
	ccxtService ccxt.CCXTService
	analytics   *services.AnalyticsService
}

// TechnicalIndicator represents a set of calculated indicators for a trading pair.
type TechnicalIndicator struct {
	// Symbol is the trading pair identifier.
	Symbol string `json:"symbol"`
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Timeframe is the candle timeframe used (e.g., "1h").
	Timeframe string `json:"timeframe"`
	// Timestamp is the calculation time.
	Timestamp time.Time `json:"timestamp"`
	// Price is the current asset price.
	Price float64 `json:"current_price"`
	// Indicators contains key-value pairs of indicator names and their values.
	Indicators map[string]interface{} `json:"indicators"`
}

// TradingSignal represents a generated buy/sell/hold signal.
type TradingSignal struct {
	// Symbol is the trading pair identifier.
	Symbol string `json:"symbol"`
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// SignalType indicates the action: "BUY", "SELL", or "HOLD".
	SignalType string `json:"signal_type"`
	// Strength indicates the signal strength: "WEAK", "MODERATE", "STRONG".
	Strength string `json:"strength"`
	// Price is the price at signal generation.
	Price float64 `json:"price"`
	// Reason describes the rationale behind the signal.
	Reason string `json:"reason"`
	// Confidence is a score from 0.0 to 1.0 indicating algorithm confidence.
	Confidence float64 `json:"confidence"`
	// Timestamp is when the signal was generated.
	Timestamp time.Time `json:"timestamp"`
	// Indicators lists the technical indicators used to generate the signal.
	Indicators []string `json:"indicators_used"`
}

// IndicatorsResponse is the API response structure for technical indicators.
type IndicatorsResponse struct {
	// Indicators is the list of calculated indicators.
	Indicators []TechnicalIndicator `json:"indicators"`
	// Count is the number of items returned.
	Count int `json:"count"`
	// Timestamp is the response generation time.
	Timestamp time.Time `json:"timestamp"`
}

// SignalsResponse is the API response structure for trading signals.
type SignalsResponse struct {
	// Signals is the list of generated signals.
	Signals []TradingSignal `json:"signals"`
	// Count is the number of signals returned.
	Count int `json:"count"`
	// Timestamp is the response generation time.
	Timestamp time.Time `json:"timestamp"`
}

// OHLCV represents a single candlestick data point.
type OHLCV struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// NewAnalysisHandler creates a new instance of AnalysisHandler.
//
// Parameters:
//
//	db: The database connection.
//	ccxtService: The CCXT service for market data.
//
// Returns:
//
//	*AnalysisHandler: The initialized handler.
func NewAnalysisHandler(db DBQuerier, ccxtService ccxt.CCXTService, analytics ...*services.AnalyticsService) *AnalysisHandler {
	var analyticsService *services.AnalyticsService
	if len(analytics) > 0 {
		analyticsService = analytics[0]
	}

	return &AnalysisHandler{
		db:          db,
		ccxtService: ccxtService,
		analytics:   analyticsService,
	}
}

// GetTechnicalIndicators calculates and returns technical indicators for a specific symbol.
// It supports query parameters: symbol, exchange, timeframe (default "1h").
//
// Parameters:
//
//	c: The Gin context.
func (h *AnalysisHandler) GetTechnicalIndicators(c *gin.Context) {
	symbol := c.Query("symbol")
	exchange := c.Query("exchange")
	timeframe := c.DefaultQuery("timeframe", "1h")

	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange parameter is required"})
		return
	}

	// Get OHLCV data for calculations
	ohlcvData, err := h.getOHLCVData(c.Request.Context(), symbol, exchange, timeframe, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get market data"})
		return
	}

	if len(ohlcvData) < 20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Insufficient data for technical analysis"})
		return
	}

	// Calculate indicators
	indicators := h.calculateIndicators(ohlcvData)
	currentPrice := ohlcvData[len(ohlcvData)-1].Close

	techIndicator := TechnicalIndicator{
		Symbol:     symbol,
		Exchange:   exchange,
		Timeframe:  timeframe,
		Timestamp:  time.Now(),
		Price:      currentPrice,
		Indicators: indicators,
	}

	response := IndicatorsResponse{
		Indicators: []TechnicalIndicator{techIndicator},
		Count:      1,
		Timestamp:  time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// GetCorrelationMatrix returns correlation results for multiple symbols.
func (h *AnalysisHandler) GetCorrelationMatrix(c *gin.Context) {
	if h.analytics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Analytics service unavailable"})
		return
	}

	exchange := c.Query("exchange")
	symbolsParam := c.Query("symbols")
	if exchange == "" || symbolsParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange and symbols parameters are required"})
		return
	}

	symbols := strings.Split(symbolsParam, ",")
	limit := parseIntQuery(c, "limit", 0)

	result, err := h.analytics.CalculateCorrelationMatrix(c.Request.Context(), exchange, symbols, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetMarketRegime returns the current regime classification for a symbol.
func (h *AnalysisHandler) GetMarketRegime(c *gin.Context) {
	if h.analytics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Analytics service unavailable"})
		return
	}

	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange and symbol parameters are required"})
		return
	}

	limit := parseIntQuery(c, "limit", 0)
	result, err := h.analytics.DetectMarketRegime(c.Request.Context(), exchange, symbol, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetForecast returns a short-term forecast for prices or funding rates.
func (h *AnalysisHandler) GetForecast(c *gin.Context) {
	if h.analytics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Analytics service unavailable"})
		return
	}

	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	seriesType := strings.ToLower(c.DefaultQuery("type", "price"))
	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange and symbol parameters are required"})
		return
	}

	horizon := parseIntQuery(c, "horizon", 0)
	limit := parseIntQuery(c, "limit", 0)

	result, err := h.analytics.ForecastSeries(c.Request.Context(), exchange, symbol, seriesType, horizon, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func parseIntQuery(c *gin.Context, key string, fallback int) int {
	valueStr := c.Query(key)
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return fallback
	}
	// Bounds check to prevent excessive memory allocation
	// Negative values get fallback, and cap at reasonable maximum (10000)
	const maxQueryInt = 10000
	if value < 0 {
		return fallback
	}
	if value > maxQueryInt {
		return maxQueryInt
	}
	return value
}

// GetTradingSignals generates trading signals based on technical analysis.
// It supports query parameters: symbol (optional), exchange (optional), timeframe (default "1h"), min_confidence (default "0.6").
//
// Parameters:
//
//	c: The Gin context.
func (h *AnalysisHandler) GetTradingSignals(c *gin.Context) {
	symbol := c.Query("symbol")
	exchange := c.Query("exchange")
	timeframe := c.DefaultQuery("timeframe", "1h")
	minConfidenceStr := c.DefaultQuery("min_confidence", "0.6")

	minConfidence, err := strconv.ParseFloat(minConfidenceStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid min_confidence parameter"})
		return
	}

	var signals []TradingSignal

	if symbol != "" && exchange != "" {
		// Get signal for specific symbol/exchange
		signal, err := h.generateTradingSignal(c.Request.Context(), symbol, exchange, timeframe)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate trading signal"})
			return
		}
		if signal != nil && signal.Confidence >= minConfidence {
			signals = append(signals, *signal)
		}
	} else {
		// Get signals for all active symbols
		signals, err = h.getAllTradingSignals(c.Request.Context(), timeframe, minConfidence)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate trading signals"})
			return
		}
	}

	response := SignalsResponse{
		Signals:   signals,
		Count:     len(signals),
		Timestamp: time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// getOHLCVData retrieves OHLCV data from database or CCXT service
func (h *AnalysisHandler) getOHLCVData(ctx context.Context, symbol, exchange, timeframe string, limit int) ([]OHLCV, error) {
	if h.db == nil {
		return nil, fmt.Errorf("failed to query OHLCV data: database connection is nil")
	}

	// Try to get data from database first (market_data table)
	query := `
		SELECT timestamp, bid as close, ask as open,
		       GREATEST(bid, ask) as high, LEAST(bid, ask) as low, volume
		FROM market_data
		WHERE symbol = $1 AND exchange = $2
		  AND timestamp > NOW() - INTERVAL '24 hours'
		ORDER BY timestamp ASC
		LIMIT $3
	`

	rows, err := h.db.Query(ctx, query, symbol, exchange, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query OHLCV data: %w", err)
	}
	defer rows.Close()

	var ohlcvData []OHLCV
	for rows.Next() {
		var ohlcv OHLCV
		if err := rows.Scan(&ohlcv.Timestamp, &ohlcv.Close, &ohlcv.Open, &ohlcv.High, &ohlcv.Low, &ohlcv.Volume); err != nil {
			continue
		}
		ohlcvData = append(ohlcvData, ohlcv)
	}

	// If we don't have enough data, try to get from CCXT service
	if len(ohlcvData) < 20 {
		// Fallback to CCXT service for OHLCV data
		// This would require implementing OHLCV endpoint in CCXT service
		// For now, we'll simulate some data based on recent tickers
		return h.simulateOHLCVFromTickers(ctx, symbol, exchange, limit)
	}

	return ohlcvData, nil
}

// simulateOHLCVFromTickers creates OHLCV data from recent ticker data
func (h *AnalysisHandler) simulateOHLCVFromTickers(ctx context.Context, symbol, exchange string, limit int) ([]OHLCV, error) {
	if h.db == nil {
		return nil, fmt.Errorf("failed to query ticker data: database connection is nil")
	}

	query := `
		SELECT timestamp, bid, ask, volume
		FROM market_data
		WHERE symbol = $1 AND exchange = $2
		  AND timestamp > NOW() - INTERVAL '6 hours'
		ORDER BY timestamp ASC
	`

	rows, err := h.db.Query(ctx, query, symbol, exchange)
	if err != nil {
		return nil, fmt.Errorf("failed to query ticker data: %w", err)
	}
	defer rows.Close()

	var ohlcvData []OHLCV
	var lastPrice float64

	for rows.Next() {
		var timestamp time.Time
		var bid, ask, volume float64

		if err := rows.Scan(&timestamp, &bid, &ask, &volume); err != nil {
			continue
		}

		midPrice := (bid + ask) / 2
		if lastPrice == 0 {
			lastPrice = midPrice
		}

		ohlcv := OHLCV{
			Timestamp: timestamp,
			Open:      lastPrice,
			High:      math.Max(lastPrice, midPrice),
			Low:       math.Min(lastPrice, midPrice),
			Close:     midPrice,
			Volume:    volume,
		}

		ohlcvData = append(ohlcvData, ohlcv)
		lastPrice = midPrice

		if len(ohlcvData) >= limit {
			break
		}
	}

	return ohlcvData, nil
}

// calculateIndicators computes various technical indicators
func (h *AnalysisHandler) calculateIndicators(data []OHLCV) map[string]interface{} {
	indicators := make(map[string]interface{})

	if len(data) < 20 {
		return indicators
	}

	// Simple Moving Averages
	indicators["sma_20"] = h.calculateSMA(data, 20)
	indicators["sma_50"] = h.calculateSMA(data, 50)

	// Exponential Moving Averages
	indicators["ema_12"] = h.calculateEMA(data, 12)
	indicators["ema_26"] = h.calculateEMA(data, 26)

	// RSI
	indicators["rsi_14"] = h.calculateRSI(data, 14)

	// MACD
	macd, signal, histogram := h.calculateMACD(data)
	indicators["macd"] = map[string]float64{
		"macd":      macd,
		"signal":    signal,
		"histogram": histogram,
	}

	// Bollinger Bands
	upper, middle, lower := h.calculateBollingerBands(data, 20, 2.0)
	indicators["bollinger_bands"] = map[string]float64{
		"upper":  upper,
		"middle": middle,
		"lower":  lower,
	}

	// Support and Resistance levels
	support, resistance := h.calculateSupportResistance(data)
	indicators["support_resistance"] = map[string]float64{
		"support":    support,
		"resistance": resistance,
	}

	return indicators
}

// calculateSMA calculates Simple Moving Average
func (h *AnalysisHandler) calculateSMA(data []OHLCV, period int) float64 {
	if len(data) < period {
		return 0
	}

	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i].Close
	}
	return sum / float64(period)
}

// calculateEMA calculates Exponential Moving Average
func (h *AnalysisHandler) calculateEMA(data []OHLCV, period int) float64 {
	if len(data) < period {
		return 0
	}

	multiplier := 2.0 / (float64(period) + 1.0)
	ema := data[len(data)-period].Close

	for i := len(data) - period + 1; i < len(data); i++ {
		ema = (data[i].Close * multiplier) + (ema * (1 - multiplier))
	}

	return ema
}

// calculateRSI calculates Relative Strength Index
func (h *AnalysisHandler) calculateRSI(data []OHLCV, period int) float64 {
	if len(data) < period+1 {
		return 50 // Neutral RSI
	}

	gains := 0.0
	losses := 0.0

	for i := len(data) - period; i < len(data); i++ {
		change := data[i].Close - data[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += math.Abs(change)
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// calculateMACD calculates MACD indicator
func (h *AnalysisHandler) calculateMACD(data []OHLCV) (float64, float64, float64) {
	ema12 := h.calculateEMA(data, 12)
	ema26 := h.calculateEMA(data, 26)
	macd := ema12 - ema26

	// For signal line, we would need to calculate EMA of MACD
	// Simplified version here
	signal := macd * 0.9 // Approximation
	histogram := macd - signal

	return macd, signal, histogram
}

// calculateBollingerBands calculates Bollinger Bands
func (h *AnalysisHandler) calculateBollingerBands(data []OHLCV, period int, stdDev float64) (float64, float64, float64) {
	middle := h.calculateSMA(data, period)

	if len(data) < period {
		return middle, middle, middle
	}

	// Calculate standard deviation
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		diff := data[i].Close - middle
		sum += diff * diff
	}
	std := math.Sqrt(sum / float64(period))

	upper := middle + (stdDev * std)
	lower := middle - (stdDev * std)

	return upper, middle, lower
}

// calculateSupportResistance finds support and resistance levels
func (h *AnalysisHandler) calculateSupportResistance(data []OHLCV) (float64, float64) {
	if len(data) < 10 {
		current := data[len(data)-1].Close
		return current * 0.95, current * 1.05
	}

	// Find recent highs and lows
	var highs, lows []float64
	for i := 1; i < len(data)-1; i++ {
		if data[i].High > data[i-1].High && data[i].High > data[i+1].High {
			highs = append(highs, data[i].High)
		}
		if data[i].Low < data[i-1].Low && data[i].Low < data[i+1].Low {
			lows = append(lows, data[i].Low)
		}
	}

	// Calculate average support and resistance
	support := 0.0
	resistance := 0.0

	if len(lows) > 0 {
		for _, low := range lows {
			support += low
		}
		support /= float64(len(lows))
	} else {
		support = data[len(data)-1].Close * 0.95
	}

	if len(highs) > 0 {
		for _, high := range highs {
			resistance += high
		}
		resistance /= float64(len(highs))
	} else {
		resistance = data[len(data)-1].Close * 1.05
	}

	return support, resistance
}

// generateTradingSignal creates a trading signal based on technical analysis
func (h *AnalysisHandler) generateTradingSignal(ctx context.Context, symbol, exchange, timeframe string) (*TradingSignal, error) {
	ohlcvData, err := h.getOHLCVData(ctx, symbol, exchange, timeframe, 50)
	if err != nil || len(ohlcvData) < 20 {
		return nil, err
	}

	indicators := h.calculateIndicators(ohlcvData)
	currentPrice := ohlcvData[len(ohlcvData)-1].Close

	// Analyze indicators for signal generation
	signalType := "HOLD"
	strength := "WEAK"
	confidence := 0.5
	reason := "Neutral market conditions"
	usedIndicators := []string{}

	// RSI analysis
	if rsi, ok := indicators["rsi_14"].(float64); ok {
		usedIndicators = append(usedIndicators, "RSI")
		if rsi < 30 {
			signalType = "BUY"
			reason = "RSI indicates oversold conditions"
			confidence += 0.2
		} else if rsi > 70 {
			signalType = "SELL"
			reason = "RSI indicates overbought conditions"
			confidence += 0.2
		}
	}

	// Moving Average analysis
	if sma20, ok := indicators["sma_20"].(float64); ok {
		if sma50, ok := indicators["sma_50"].(float64); ok {
			usedIndicators = append(usedIndicators, "SMA")
			if currentPrice > sma20 && sma20 > sma50 {
				if signalType != "SELL" {
					signalType = "BUY"
					reason += "; Price above moving averages (bullish trend)"
				}
				confidence += 0.15
			} else if currentPrice < sma20 && sma20 < sma50 {
				if signalType != "BUY" {
					signalType = "SELL"
					reason += "; Price below moving averages (bearish trend)"
				}
				confidence += 0.15
			}
		}
	}

	// Bollinger Bands analysis
	if bb, ok := indicators["bollinger_bands"].(map[string]float64); ok {
		usedIndicators = append(usedIndicators, "Bollinger Bands")
		if currentPrice < bb["lower"] {
			if signalType != "SELL" {
				signalType = "BUY"
				reason += "; Price near lower Bollinger Band"
			}
			confidence += 0.1
		} else if currentPrice > bb["upper"] {
			if signalType != "BUY" {
				signalType = "SELL"
				reason += "; Price near upper Bollinger Band"
			}
			confidence += 0.1
		}
	}

	// Determine strength based on confidence
	if confidence >= 0.8 {
		strength = "STRONG"
	} else if confidence >= 0.6 {
		strength = "MODERATE"
	}

	signal := &TradingSignal{
		Symbol:     symbol,
		Exchange:   exchange,
		SignalType: signalType,
		Strength:   strength,
		Price:      currentPrice,
		Reason:     reason,
		Confidence: confidence,
		Timestamp:  time.Now(),
		Indicators: usedIndicators,
	}

	return signal, nil
}

// getAllTradingSignals generates signals for all active trading pairs
func (h *AnalysisHandler) getAllTradingSignals(ctx context.Context, timeframe string, minConfidence float64) ([]TradingSignal, error) {
	if h.db == nil {
		return nil, fmt.Errorf("failed to query active pairs: database connection is nil")
	}

	// Get active trading pairs from recent market data
	query := `
		SELECT DISTINCT symbol, exchange
		FROM market_data
		WHERE timestamp > NOW() - INTERVAL '1 hour'
		LIMIT 20
	`

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active pairs: %w", err)
	}
	defer rows.Close()

	var signals []TradingSignal
	for rows.Next() {
		var symbol, exchange string
		if err := rows.Scan(&symbol, &exchange); err != nil {
			continue
		}

		signal, err := h.generateTradingSignal(ctx, symbol, exchange, timeframe)
		if err != nil {
			continue
		}

		if signal != nil && signal.Confidence >= minConfidence {
			signals = append(signals, *signal)
		}
	}

	return signals, nil
}
