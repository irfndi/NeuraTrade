package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/api/handlers/testmocks"
	"github.com/stretchr/testify/assert"
)

func TestTechnicalIndicator_Struct(t *testing.T) {
	indicator := TechnicalIndicator{
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Timeframe:  "1h",
		Timestamp:  time.Now(),
		Price:      50000.0,
		Indicators: map[string]interface{}{"rsi": 65.5, "macd": 0.25},
	}

	assert.Equal(t, "BTC/USDT", indicator.Symbol)
	assert.Equal(t, "binance", indicator.Exchange)
	assert.Equal(t, "1h", indicator.Timeframe)
	assert.Equal(t, 50000.0, indicator.Price)
	assert.NotZero(t, indicator.Timestamp)
	assert.Contains(t, indicator.Indicators, "rsi")
	assert.Contains(t, indicator.Indicators, "macd")
}

func TestTradingSignal_Struct(t *testing.T) {
	signal := TradingSignal{
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		SignalType: "BUY",
		Strength:   "STRONG",
		Price:      50000.0,
		Reason:     "RSI oversold",
		Confidence: 0.8,
		Timestamp:  time.Now(),
		Indicators: []string{"RSI", "MACD"},
	}

	assert.Equal(t, "BTC/USDT", signal.Symbol)
	assert.Equal(t, "binance", signal.Exchange)
	assert.Equal(t, "BUY", signal.SignalType)
	assert.Equal(t, "STRONG", signal.Strength)
	assert.Equal(t, 50000.0, signal.Price)
	assert.Equal(t, "RSI oversold", signal.Reason)
	assert.Equal(t, 0.8, signal.Confidence)
	assert.NotZero(t, signal.Timestamp)
	assert.Contains(t, signal.Indicators, "RSI")
	assert.Contains(t, signal.Indicators, "MACD")
}

func TestIndicatorsResponse_Struct(t *testing.T) {
	indicators := []TechnicalIndicator{
		{
			Symbol:     "BTC/USDT",
			Exchange:   "binance",
			Timeframe:  "1h",
			Timestamp:  time.Now(),
			Price:      50000.0,
			Indicators: map[string]interface{}{"rsi": 65.5},
		},
		{
			Symbol:     "ETH/USDT",
			Exchange:   "binance",
			Timeframe:  "1h",
			Timestamp:  time.Now(),
			Price:      3000.0,
			Indicators: map[string]interface{}{"macd": 0.25},
		},
	}

	response := IndicatorsResponse{
		Indicators: indicators,
		Count:      2,
		Timestamp:  time.Now(),
	}

	assert.Len(t, response.Indicators, 2)
	assert.Equal(t, 2, response.Count)
	assert.NotZero(t, response.Timestamp)
	assert.Equal(t, "BTC/USDT", response.Indicators[0].Symbol)
	assert.Equal(t, "ETH/USDT", response.Indicators[1].Symbol)
}

func TestSignalsResponse_Struct(t *testing.T) {
	signals := []TradingSignal{
		{
			Symbol:     "BTC/USDT",
			Exchange:   "binance",
			SignalType: "BUY",
			Strength:   "STRONG",
			Price:      50000.0,
			Reason:     "RSI oversold",
			Confidence: 0.8,
			Timestamp:  time.Now(),
			Indicators: []string{"RSI"},
		},
		{
			Symbol:     "ETH/USDT",
			Exchange:   "binance",
			SignalType: "SELL",
			Strength:   "MODERATE",
			Price:      3000.0,
			Reason:     "MACD bearish",
			Confidence: 0.6,
			Timestamp:  time.Now(),
			Indicators: []string{"MACD"},
		},
	}

	response := SignalsResponse{
		Signals:   signals,
		Count:     2,
		Timestamp: time.Now(),
	}

	assert.Len(t, response.Signals, 2)
	assert.Equal(t, 2, response.Count)
	assert.NotZero(t, response.Timestamp)
	assert.Equal(t, "BTC/USDT", response.Signals[0].Symbol)
	assert.Equal(t, "BUY", response.Signals[0].SignalType)
	assert.Equal(t, "ETH/USDT", response.Signals[1].Symbol)
	assert.Equal(t, "SELL", response.Signals[1].SignalType)
}

func TestOHLCV_Struct(t *testing.T) {
	ohlcv := OHLCV{
		Timestamp: time.Now(),
		Open:      50000.0,
		High:      51000.0,
		Low:       49500.0,
		Close:     50500.0,
		Volume:    1000.0,
	}

	assert.NotZero(t, ohlcv.Timestamp)
	assert.Equal(t, 50000.0, ohlcv.Open)
	assert.Equal(t, 51000.0, ohlcv.High)
	assert.Equal(t, 49500.0, ohlcv.Low)
	assert.Equal(t, 50500.0, ohlcv.Close)
	assert.Equal(t, 1000.0, ohlcv.Volume)
}

func TestNewAnalysisHandler(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	assert.NotNil(t, handler)
	assert.Equal(t, mockCCXT, handler.ccxtService)
}

func TestAnalysisHandler_GetTechnicalIndicators(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		symbol         string
		exchange       string
		timeframe      string
		expectedStatus int
		expectError    bool
	}{
		{
			name:           "missing symbol",
			symbol:         "",
			exchange:       "binance",
			timeframe:      "1h",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
		{
			name:           "missing exchange",
			symbol:         "BTC/USDT",
			exchange:       "",
			timeframe:      "1h",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCCXT := &testmocks.MockCCXTService{}
			handler := NewAnalysisHandler(nil, mockCCXT, nil)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set query parameters
			c.Request = httptest.NewRequest("GET", "/analysis/indicators", nil)
			q := c.Request.URL.Query()
			if tt.symbol != "" {
				q.Add("symbol", tt.symbol)
			}
			if tt.exchange != "" {
				q.Add("exchange", tt.exchange)
			}
			if tt.timeframe != "" {
				q.Add("timeframe", tt.timeframe)
			}
			c.Request.URL.RawQuery = q.Encode()

			handler.GetTechnicalIndicators(c)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectError {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Contains(t, response, "error")
			}
		})
	}
}

func TestAnalysisHandler_GetTradingSignals(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		symbol         string
		exchange       string
		timeframe      string
		minConfidence  string
		expectedStatus int
		expectError    bool
	}{
		{
			name:           "invalid min_confidence",
			symbol:         "BTC/USDT",
			exchange:       "binance",
			timeframe:      "1h",
			minConfidence:  "invalid",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCCXT := &testmocks.MockCCXTService{}
			handler := NewAnalysisHandler(nil, mockCCXT, nil)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set query parameters
			c.Request = httptest.NewRequest("GET", "/analysis/signals", nil)
			q := c.Request.URL.Query()
			if tt.symbol != "" {
				q.Add("symbol", tt.symbol)
			}
			if tt.exchange != "" {
				q.Add("exchange", tt.exchange)
			}
			if tt.timeframe != "" {
				q.Add("timeframe", tt.timeframe)
			}
			if tt.minConfidence != "" {
				q.Add("min_confidence", tt.minConfidence)
			}
			c.Request.URL.RawQuery = q.Encode()

			handler.GetTradingSignals(c)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectError {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Contains(t, response, "error")
			}
		})
	}
}

// Test helper functions for technical indicators
func TestAnalysisHandler_calculateSMA(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test data with known values
	data := []OHLCV{
		{Close: 100.0}, {Close: 110.0}, {Close: 120.0}, {Close: 130.0}, {Close: 140.0},
	}

	// Test SMA calculation
	sma := handler.calculateSMA(data, 5)
	expected := (100.0 + 110.0 + 120.0 + 130.0 + 140.0) / 5.0
	assert.Equal(t, expected, sma)

	// Test with insufficient data
	smaShort := handler.calculateSMA(data[:2], 5)
	assert.Equal(t, 0.0, smaShort)

	// Test with exact period
	smaExact := handler.calculateSMA(data, 3)
	expectedExact := (120.0 + 130.0 + 140.0) / 3.0
	assert.Equal(t, expectedExact, smaExact)
}

func TestAnalysisHandler_calculateEMA(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test data
	data := []OHLCV{
		{Close: 100.0}, {Close: 110.0}, {Close: 120.0}, {Close: 130.0}, {Close: 140.0},
	}

	// Test EMA calculation
	ema := handler.calculateEMA(data, 3)
	assert.Greater(t, ema, 0.0)
	assert.Less(t, ema, 200.0) // Reasonable bounds

	// Test with insufficient data
	emaShort := handler.calculateEMA(data[:1], 5)
	assert.Equal(t, 0.0, emaShort)

	// Test that EMA is different from SMA
	sma := handler.calculateSMA(data, 3)
	assert.NotEqual(t, sma, ema)
}

func TestAnalysisHandler_calculateRSI(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test data with upward trend
	upwardData := []OHLCV{
		{Close: 100.0}, {Close: 105.0}, {Close: 110.0}, {Close: 115.0}, {Close: 120.0},
		{Close: 125.0}, {Close: 130.0}, {Close: 135.0}, {Close: 140.0}, {Close: 145.0},
		{Close: 150.0}, {Close: 155.0}, {Close: 160.0}, {Close: 165.0}, {Close: 170.0},
	}

	rsi := handler.calculateRSI(upwardData, 14)
	assert.Greater(t, rsi, 50.0) // Should be above 50 for upward trend
	assert.LessOrEqual(t, rsi, 100.0)

	// Test with insufficient data
	rsiShort := handler.calculateRSI(upwardData[:5], 14)
	assert.Equal(t, 50.0, rsiShort) // Should return neutral RSI

	// Test with downward trend
	downwardData := []OHLCV{
		{Close: 170.0}, {Close: 165.0}, {Close: 160.0}, {Close: 155.0}, {Close: 150.0},
		{Close: 145.0}, {Close: 140.0}, {Close: 135.0}, {Close: 130.0}, {Close: 125.0},
		{Close: 120.0}, {Close: 115.0}, {Close: 110.0}, {Close: 105.0}, {Close: 100.0},
	}

	rsiDown := handler.calculateRSI(downwardData, 14)
	assert.Less(t, rsiDown, 50.0) // Should be below 50 for downward trend
	assert.GreaterOrEqual(t, rsiDown, 0.0)
}

func TestAnalysisHandler_calculateMACD(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test data
	data := []OHLCV{
		{Close: 100.0}, {Close: 102.0}, {Close: 104.0}, {Close: 106.0}, {Close: 108.0},
		{Close: 110.0}, {Close: 112.0}, {Close: 114.0}, {Close: 116.0}, {Close: 118.0},
		{Close: 120.0}, {Close: 122.0}, {Close: 124.0}, {Close: 126.0}, {Close: 128.0},
		{Close: 130.0}, {Close: 132.0}, {Close: 134.0}, {Close: 136.0}, {Close: 138.0},
		{Close: 140.0}, {Close: 142.0}, {Close: 144.0}, {Close: 146.0}, {Close: 148.0},
		{Close: 150.0}, {Close: 152.0},
	}

	macd, signal, histogram := handler.calculateMACD(data)

	// MACD should be positive for upward trend
	assert.Greater(t, macd, 0.0)
	// Signal should be related to MACD
	assert.NotZero(t, signal)
	// Histogram should be the difference (with tolerance for floating point precision)
	expectedHistogram := macd - signal
	assert.InDelta(t, expectedHistogram, histogram, 0.0000001)
}

func TestAnalysisHandler_calculateBollingerBands(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test data with some volatility
	data := []OHLCV{
		{Close: 100.0}, {Close: 105.0}, {Close: 95.0}, {Close: 110.0}, {Close: 90.0},
		{Close: 115.0}, {Close: 85.0}, {Close: 120.0}, {Close: 80.0}, {Close: 125.0},
		{Close: 75.0}, {Close: 130.0}, {Close: 70.0}, {Close: 135.0}, {Close: 65.0},
		{Close: 140.0}, {Close: 60.0}, {Close: 145.0}, {Close: 55.0}, {Close: 150.0},
	}

	upper, middle, lower := handler.calculateBollingerBands(data, 20, 2.0)

	// Upper band should be greater than middle
	assert.Greater(t, upper, middle)
	// Middle should be greater than lower
	assert.Greater(t, middle, lower)
	// All values should be positive
	assert.Greater(t, upper, 0.0)
	assert.Greater(t, middle, 0.0)
	assert.Greater(t, lower, 0.0)

	// Test with insufficient data
	shortData := data[:5]
	upperShort, middleShort, lowerShort := handler.calculateBollingerBands(shortData, 20, 2.0)
	// Should return middle value for all bands
	assert.Equal(t, middleShort, upperShort)
	assert.Equal(t, middleShort, lowerShort)
}

func TestAnalysisHandler_calculateSupportResistance(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test data
	data := []OHLCV{
		{Close: 100.0}, {Close: 105.0}, {Close: 110.0}, {Close: 115.0}, {Close: 120.0},
		{Close: 125.0}, {Close: 130.0}, {Close: 135.0}, {Close: 140.0}, {Close: 145.0},
		{Close: 150.0},
	}

	support, resistance := handler.calculateSupportResistance(data)

	// Support should be less than resistance
	assert.Less(t, support, resistance)
	// Both should be positive
	assert.Greater(t, support, 0.0)
	assert.Greater(t, resistance, 0.0)

	// Test with insufficient data
	shortData := data[:5]
	supportShort, resistanceShort := handler.calculateSupportResistance(shortData)
	current := shortData[len(shortData)-1].Close
	assert.Equal(t, current*0.95, supportShort)
	assert.Equal(t, current*1.05, resistanceShort)
}

func TestAnalysisHandler_calculateIndicators(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test data with sufficient length
	data := make([]OHLCV, 60)
	for i := 0; i < 60; i++ {
		data[i] = OHLCV{Close: 100.0 + float64(i)}
	}

	indicators := handler.calculateIndicators(data)

	// Should contain all expected indicators
	assert.Contains(t, indicators, "sma_20")
	assert.Contains(t, indicators, "sma_50")
	assert.Contains(t, indicators, "ema_12")
	assert.Contains(t, indicators, "ema_26")
	assert.Contains(t, indicators, "rsi_14")
	assert.Contains(t, indicators, "macd")
	assert.Contains(t, indicators, "bollinger_bands")
	assert.Contains(t, indicators, "support_resistance")

	// Test MACD structure
	macdData, ok := indicators["macd"].(map[string]float64)
	assert.True(t, ok)
	assert.Contains(t, macdData, "macd")
	assert.Contains(t, macdData, "signal")
	assert.Contains(t, macdData, "histogram")

	// Test Bollinger Bands structure
	bbData, ok := indicators["bollinger_bands"].(map[string]float64)
	assert.True(t, ok)
	assert.Contains(t, bbData, "upper")
	assert.Contains(t, bbData, "middle")
	assert.Contains(t, bbData, "lower")

	// Test Support/Resistance structure
	srData, ok := indicators["support_resistance"].(map[string]float64)
	assert.True(t, ok)
	assert.Contains(t, srData, "support")
	assert.Contains(t, srData, "resistance")

	// Test with insufficient data
	shortData := data[:10]
	indicatorsShort := handler.calculateIndicators(shortData)
	assert.Empty(t, indicatorsShort) // Should return empty map
}

func TestAnalysisHandler_getOHLCVData(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test with nil database - should return error
	_, err := handler.getOHLCVData(context.Background(), "BTC/USDT", "binance", "1h", 50)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query OHLCV data")
}

func TestAnalysisHandler_simulateOHLCVFromTickers(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test with nil database - should return error
	_, err := handler.simulateOHLCVFromTickers(context.Background(), "BTC/USDT", "binance", 50)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query ticker data")

	t.Run("various symbols", func(t *testing.T) {
		symbols := []string{"BTC/USDT", "ETH/USDT", "BNB/USDT", "ADA/USDT", "XRP/USDT"}
		for _, symbol := range symbols {
			t.Run(symbol, func(t *testing.T) {
				_, err := handler.simulateOHLCVFromTickers(context.Background(), symbol, "binance", 50)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "failed to query ticker data")
			})
		}
	})

	t.Run("various exchanges", func(t *testing.T) {
		exchanges := []string{"binance", "coinbase", "kraken", "kucoin", "bitfinex"}
		for _, exchange := range exchanges {
			t.Run(exchange, func(t *testing.T) {
				_, err := handler.simulateOHLCVFromTickers(context.Background(), "BTC/USDT", exchange, 50)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "failed to query ticker data")
			})
		}
	})

	t.Run("various limits", func(t *testing.T) {
		limits := []int{0, 1, 10, 50, 100, 1000}
		for _, limit := range limits {
			t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
				_, err := handler.simulateOHLCVFromTickers(context.Background(), "BTC/USDT", "binance", limit)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "failed to query ticker data")
			})
		}
	})

	t.Run("negative limit", func(t *testing.T) {
		_, err := handler.simulateOHLCVFromTickers(context.Background(), "BTC/USDT", "binance", -10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query ticker data")
	})

	t.Run("empty symbol", func(t *testing.T) {
		_, err := handler.simulateOHLCVFromTickers(context.Background(), "", "binance", 50)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query ticker data")
	})

	t.Run("empty exchange", func(t *testing.T) {
		_, err := handler.simulateOHLCVFromTickers(context.Background(), "BTC/USDT", "", 50)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query ticker data")
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := handler.simulateOHLCVFromTickers(ctx, "BTC/USDT", "binance", 50)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query ticker data")
	})

	t.Run("timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		time.Sleep(time.Millisecond)

		_, err := handler.simulateOHLCVFromTickers(ctx, "BTC/USDT", "binance", 50)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query ticker data")
	})
}

func TestAnalysisHandler_generateTradingSignal(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test with database error (nil db)
	_, err := handler.generateTradingSignal(context.Background(), "BTC/USDT", "binance", "1h")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query OHLCV data")
}

func TestAnalysisHandler_getAllTradingSignals(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewAnalysisHandler(nil, mockCCXT, nil)

	// Test with database error (nil db)
	_, err := handler.getAllTradingSignals(context.Background(), "1h", 0.6)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query active pairs")
}
