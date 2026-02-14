package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestOrderBookImbalanceDetector_Detect_BullishSignal(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()
	config := DefaultOrderBookImbalanceConfig()
	detector := NewOrderBookImbalanceDetector(config, mockService, logger)

	// Setup mock for bullish imbalance (more bids than asks)
	metrics := &ccxt.OrderBookMetrics{
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		BidDepth1Pct:  decimal.NewFromInt(300000),
		AskDepth1Pct:  decimal.NewFromInt(100000),
		Imbalance1Pct: decimal.NewFromFloat(0.5), // 50% bullish imbalance
		BestBid:       decimal.NewFromFloat(50000),
		BestAsk:       decimal.NewFromFloat(50010),
		MidPrice:      decimal.NewFromFloat(50005),
		BidLevels:     10,
		AskLevels:     10,
		Timestamp:     time.Now(),
	}

	mockService.On("CalculateOrderBookMetrics", mock.Anything, "binance", "BTC/USDT", 50).Return(metrics, nil)

	signal, err := detector.Detect(context.Background(), "binance", "BTC/USDT")

	assert.NoError(t, err)
	assert.NotNil(t, signal)
	assert.Equal(t, "bullish", signal.Direction)
	assert.Equal(t, "BTC/USDT", signal.Symbol)
	assert.True(t, signal.ImbalancePct.GreaterThan(decimal.NewFromFloat(0.2)))
	mockService.AssertExpectations(t)
}

func TestOrderBookImbalanceDetector_Detect_BearishSignal(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()
	config := DefaultOrderBookImbalanceConfig()
	detector := NewOrderBookImbalanceDetector(config, mockService, logger)

	// Setup mock for bearish imbalance (more asks than bids)
	metrics := &ccxt.OrderBookMetrics{
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		BidDepth1Pct:  decimal.NewFromInt(100000),
		AskDepth1Pct:  decimal.NewFromInt(300000),
		Imbalance1Pct: decimal.NewFromFloat(-0.5), // 50% bearish imbalance
		BestBid:       decimal.NewFromFloat(50000),
		BestAsk:       decimal.NewFromFloat(50010),
		MidPrice:      decimal.NewFromFloat(50005),
		BidLevels:     10,
		AskLevels:     10,
		Timestamp:     time.Now(),
	}

	mockService.On("CalculateOrderBookMetrics", mock.Anything, "binance", "BTC/USDT", 50).Return(metrics, nil)

	signal, err := detector.Detect(context.Background(), "binance", "BTC/USDT")

	assert.NoError(t, err)
	assert.NotNil(t, signal)
	assert.Equal(t, "bearish", signal.Direction)
	mockService.AssertExpectations(t)
}

func TestOrderBookImbalanceDetector_Detect_NoSignal(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()
	config := DefaultOrderBookImbalanceConfig()
	detector := NewOrderBookImbalanceDetector(config, mockService, logger)

	// Setup mock for neutral imbalance (no significant imbalance)
	metrics := &ccxt.OrderBookMetrics{
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		BidDepth1Pct:  decimal.NewFromInt(150000),
		AskDepth1Pct:  decimal.NewFromInt(150000),
		Imbalance1Pct: decimal.NewFromFloat(0.0), // Neutral
		BestBid:       decimal.NewFromFloat(50000),
		BestAsk:       decimal.NewFromFloat(50010),
		MidPrice:      decimal.NewFromFloat(50005),
		BidLevels:     10,
		AskLevels:     10,
		Timestamp:     time.Now(),
	}

	mockService.On("CalculateOrderBookMetrics", mock.Anything, "binance", "BTC/USDT", 50).Return(metrics, nil)

	signal, err := detector.Detect(context.Background(), "binance", "BTC/USDT")

	assert.NoError(t, err)
	assert.Nil(t, signal) // No signal should be generated
	mockService.AssertExpectations(t)
}

func TestOrderBookImbalanceDetector_Detect_InsufficientDepth(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()
	config := DefaultOrderBookImbalanceConfig()
	detector := NewOrderBookImbalanceDetector(config, mockService, logger)

	// Setup mock for insufficient depth
	metrics := &ccxt.OrderBookMetrics{
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		BidDepth1Pct:  decimal.NewFromInt(10000),
		AskDepth1Pct:  decimal.NewFromInt(10000),
		Imbalance1Pct: decimal.NewFromFloat(0.5),
		BestBid:       decimal.NewFromFloat(50000),
		BestAsk:       decimal.NewFromFloat(50010),
		MidPrice:      decimal.NewFromFloat(50005),
		BidLevels:     10,
		AskLevels:     10,
		Timestamp:     time.Now(),
	}

	mockService.On("CalculateOrderBookMetrics", mock.Anything, "binance", "BTC/USDT", 50).Return(metrics, nil)

	signal, err := detector.Detect(context.Background(), "binance", "BTC/USDT")

	assert.NoError(t, err)
	assert.Nil(t, signal) // No signal due to insufficient depth
	mockService.AssertExpectations(t)
}

func TestOrderBookImbalanceDetector_Detect_WideSpread(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()
	config := DefaultOrderBookImbalanceConfig()
	detector := NewOrderBookImbalanceDetector(config, mockService, logger)

	// Setup mock for wide spread (>0.5%)
	metrics := &ccxt.OrderBookMetrics{
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		BidDepth1Pct:  decimal.NewFromInt(300000),
		AskDepth1Pct:  decimal.NewFromInt(100000),
		Imbalance1Pct: decimal.NewFromFloat(0.5),
		BestBid:       decimal.NewFromFloat(50000),
		BestAsk:       decimal.NewFromFloat(51000), // 2% spread
		MidPrice:      decimal.NewFromFloat(50500),
		BidLevels:     10,
		AskLevels:     10,
		Timestamp:     time.Now(),
	}

	mockService.On("CalculateOrderBookMetrics", mock.Anything, "binance", "BTC/USDT", 50).Return(metrics, nil)

	signal, err := detector.Detect(context.Background(), "binance", "BTC/USDT")

	assert.NoError(t, err)
	assert.Nil(t, signal) // No signal due to wide spread
	mockService.AssertExpectations(t)
}

func TestOrderBookImbalanceSignal_IsValid(t *testing.T) {
	now := time.Now()

	// Valid signal
	validSignal := &OrderBookImbalanceSignal{
		ID:        "test-1",
		Symbol:    "BTC/USDT",
		ExpiresAt: now.Add(5 * time.Minute),
	}
	assert.True(t, validSignal.IsValid())

	// Expired signal
	expiredSignal := &OrderBookImbalanceSignal{
		ID:        "test-2",
		Symbol:    "BTC/USDT",
		ExpiresAt: now.Add(-5 * time.Minute),
	}
	assert.False(t, expiredSignal.IsValid())
}

func TestOrderBookImbalanceDetector_GetSignalHistory(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()
	config := DefaultOrderBookImbalanceConfig()
	detector := NewOrderBookImbalanceDetector(config, mockService, logger)

	// Create test signals by directly recording them
	signal1 := &OrderBookImbalanceSignal{
		ID:         "signal-1",
		Symbol:     "BTC/USDT",
		DetectedAt: time.Now().Add(-2 * time.Hour),
	}
	signal2 := &OrderBookImbalanceSignal{
		ID:         "signal-2",
		Symbol:     "BTC/USDT",
		DetectedAt: time.Now().Add(-1 * time.Hour),
	}

	detector.recordSignal("BTC/USDT", signal1)
	detector.recordSignal("BTC/USDT", signal2)

	history := detector.GetSignalHistory("BTC/USDT", 10)
	assert.Len(t, history, 2)

	// Test with limit
	limitedHistory := detector.GetSignalHistory("BTC/USDT", 1)
	assert.Len(t, limitedHistory, 1)

	// Test non-existent symbol
	emptyHistory := detector.GetSignalHistory("ETH/USDT", 10)
	assert.Len(t, emptyHistory, 0)
}

func TestOrderBookImbalanceDetector_shouldMonitorSymbol(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()

	// Test with empty monitored symbols (monitor all)
	config1 := DefaultOrderBookImbalanceConfig()
	config1.MonitoredSymbols = []string{}
	detector1 := NewOrderBookImbalanceDetector(config1, mockService, logger)
	assert.True(t, detector1.shouldMonitorSymbol("BTC/USDT"))
	assert.True(t, detector1.shouldMonitorSymbol("ETH/USDT"))

	// Test with specific monitored symbols
	config2 := DefaultOrderBookImbalanceConfig()
	config2.MonitoredSymbols = []string{"BTC/USDT", "ETH/USDT"}
	detector2 := NewOrderBookImbalanceDetector(config2, mockService, logger)
	assert.True(t, detector2.shouldMonitorSymbol("BTC/USDT"))
	assert.True(t, detector2.shouldMonitorSymbol("ETH/USDT"))
	assert.False(t, detector2.shouldMonitorSymbol("SOL/USDT"))
}

func TestOrderBookImbalanceDetector_canEmitSignal(t *testing.T) {
	mockService := new(MockCCXTService)
	logger := zaplogrus.New()
	config := DefaultOrderBookImbalanceConfig()
	config.MinSignalInterval = 1 * time.Minute
	detector := NewOrderBookImbalanceDetector(config, mockService, logger)

	symbol := "BTC/USDT"

	// Initially should be able to emit
	assert.True(t, detector.canEmitSignal(symbol))

	// Update last signal time
	detector.updateLastSignalTime(symbol)

	// Should not be able to emit immediately
	assert.False(t, detector.canEmitSignal(symbol))

	// After enough time, should be able to emit again
	detector.lastSignalTime[symbol] = time.Now().Add(-2 * time.Minute)
	assert.True(t, detector.canEmitSignal(symbol))
}
