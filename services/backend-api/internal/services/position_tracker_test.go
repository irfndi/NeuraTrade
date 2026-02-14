package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/pkg/interfaces"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockCCXTForTracker struct {
	mock.Mock
}

func (m *MockCCXTForTracker) Initialize(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockCCXTForTracker) IsHealthy(ctx context.Context) bool {
	args := m.Called(ctx)
	return args.Bool(0)
}

func (m *MockCCXTForTracker) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCCXTForTracker) GetServiceURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockCCXTForTracker) GetSupportedExchanges() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *MockCCXTForTracker) GetExchangeInfo(exchangeID string) (ccxt.ExchangeInfo, bool) {
	args := m.Called(exchangeID)
	return args.Get(0).(ccxt.ExchangeInfo), args.Bool(1)
}

func (m *MockCCXTForTracker) GetExchangeConfig(ctx context.Context) (*ccxt.ExchangeConfigResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(*ccxt.ExchangeConfigResponse), args.Error(1)
}

func (m *MockCCXTForTracker) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTForTracker) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTForTracker) RefreshExchanges(ctx context.Context) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTForTracker) AddExchange(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTForTracker) FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]ccxt.MarketPriceInterface, error) {
	args := m.Called(ctx, exchanges, symbols)
	return args.Get(0).([]ccxt.MarketPriceInterface), args.Error(1)
}

func (m *MockCCXTForTracker) FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error) {
	args := m.Called(ctx, exchange, symbol)
	return args.Get(0).(ccxt.MarketPriceInterface), args.Error(1)
}

func (m *MockCCXTForTracker) FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookResponse, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	return args.Get(0).(*ccxt.OrderBookResponse), args.Error(1)
}

func (m *MockCCXTForTracker) CalculateOrderBookMetrics(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookMetrics, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	return args.Get(0).(*ccxt.OrderBookMetrics), args.Error(1)
}

func (m *MockCCXTForTracker) FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*ccxt.OHLCVResponse, error) {
	args := m.Called(ctx, exchange, symbol, timeframe, limit)
	return args.Get(0).(*ccxt.OHLCVResponse), args.Error(1)
}

func (m *MockCCXTForTracker) FetchTrades(ctx context.Context, exchange, symbol string, limit int) (*ccxt.TradesResponse, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	return args.Get(0).(*ccxt.TradesResponse), args.Error(1)
}

func (m *MockCCXTForTracker) FetchMarkets(ctx context.Context, exchange string) (*ccxt.MarketsResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.MarketsResponse), args.Error(1)
}

func (m *MockCCXTForTracker) FetchFundingRate(ctx context.Context, exchange, symbol string) (*ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange, symbol)
	return args.Get(0).(*ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTForTracker) FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange, symbols)
	return args.Get(0).([]ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTForTracker) FetchAllFundingRates(ctx context.Context, exchange string) ([]ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).([]ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTForTracker) CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	args := m.Called(ctx, exchanges, symbols, minProfitPercent)
	return args.Get(0).([]models.ArbitrageOpportunityResponse), args.Error(1)
}

func (m *MockCCXTForTracker) CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]ccxt.FundingArbitrageOpportunity, error) {
	args := m.Called(ctx, symbols, exchanges, minProfit)
	return args.Get(0).([]ccxt.FundingArbitrageOpportunity), args.Error(1)
}

func setupPositionTrackerTest(t *testing.T) (*PositionTracker, *MockCCXTForTracker, func()) {
	logrusLogger := zaplogrus.New()

	config := DefaultPositionTrackerConfig()
	config.SyncInterval = 10 * time.Second
	config.EnableRealTimeSync = false

	mockCCXT := new(MockCCXTForTracker)

	tracker := NewPositionTracker(config, mockCCXT, nil, logrusLogger)

	cleanup := func() {}
	return tracker, mockCCXT, cleanup
}

func TestPositionTracker_OnFill_NewPosition(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill)
	require.NoError(t, err)

	position, exists := tracker.GetPosition("pos-123")
	require.True(t, exists)
	assert.Equal(t, "pos-123", position.PositionID)
	assert.Equal(t, "BTC/USDT", position.Symbol)
	assert.Equal(t, "BUY", position.Side)
	assert.True(t, position.EntryPrice.Equal(decimal.NewFromFloat(50000)))
	assert.True(t, position.Size.Equal(decimal.NewFromFloat(0.5)))
	assert.Equal(t, interfaces.PositionStatusOpen, position.Status)
}

func TestPositionTracker_OnFill_UpdateExistingPosition(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill1 := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill1)
	require.NoError(t, err)

	fill2 := FillData{
		PositionID: "pos-123",
		OrderID:    "order-789",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(51000),
		FillSize:   decimal.NewFromFloat(0.8),
		Timestamp:  time.Now().UTC(),
	}

	err = tracker.OnFill(context.Background(), fill2)
	require.NoError(t, err)

	position, exists := tracker.GetPosition("pos-123")
	require.True(t, exists)
	assert.Equal(t, "order-789", position.OrderID)
	assert.True(t, position.EntryPrice.Equal(decimal.NewFromFloat(51000)))
	assert.True(t, position.Size.Equal(decimal.NewFromFloat(0.8)))
}

func TestPositionTracker_OnPriceUpdate(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill)
	require.NoError(t, err)

	newPrice := decimal.NewFromFloat(52000)
	err = tracker.OnPriceUpdate(context.Background(), "pos-123", newPrice)
	require.NoError(t, err)

	position, exists := tracker.GetPosition("pos-123")
	require.True(t, exists)
	assert.True(t, position.CurrentPrice.Equal(newPrice))
}

func TestPositionTracker_CalculateUnrealizedPL_Long(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill)
	require.NoError(t, err)

	newPrice := decimal.NewFromFloat(52000)
	err = tracker.OnPriceUpdate(context.Background(), "pos-123", newPrice)
	require.NoError(t, err)

	position, _ := tracker.GetPosition("pos-123")
	assert.True(t, position.UnrealizedPL.Equal(decimal.NewFromFloat(1000)))
}

func TestPositionTracker_CalculateUnrealizedPL_Short(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "SELL",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill)
	require.NoError(t, err)

	newPrice := decimal.NewFromFloat(48000)
	err = tracker.OnPriceUpdate(context.Background(), "pos-123", newPrice)
	require.NoError(t, err)

	position, _ := tracker.GetPosition("pos-123")
	assert.True(t, position.UnrealizedPL.Equal(decimal.NewFromFloat(1000)))
}

func TestPositionTracker_GetOpenPositions(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill1 := FillData{
		PositionID: "pos-1",
		OrderID:    "order-1",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	fill2 := FillData{
		PositionID: "pos-2",
		OrderID:    "order-2",
		Symbol:     "ETH/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(3000),
		FillSize:   decimal.NewFromFloat(2),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill1)
	require.NoError(t, err)

	err = tracker.OnFill(context.Background(), fill2)
	require.NoError(t, err)

	openPositions := tracker.GetOpenPositions()
	assert.Len(t, openPositions, 2)
}

func TestPositionTracker_ClosePosition(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill)
	require.NoError(t, err)

	err = tracker.ClosePosition(context.Background(), "pos-123")
	require.NoError(t, err)

	position, exists := tracker.GetPosition("pos-123")
	require.True(t, exists)
	assert.Equal(t, interfaces.PositionStatusClosed, position.Status)
}

func TestPositionTracker_LiquidatePosition(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill)
	require.NoError(t, err)

	err = tracker.LiquidatePosition(context.Background(), "pos-123")
	require.NoError(t, err)

	position, exists := tracker.GetPosition("pos-123")
	require.True(t, exists)
	assert.Equal(t, interfaces.PositionStatusLiquidated, position.Status)
}

func TestPositionTracker_GetAllPositions(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill1 := FillData{
		PositionID: "pos-1",
		OrderID:    "order-1",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now().UTC(),
	}

	fill2 := FillData{
		PositionID: "pos-2",
		OrderID:    "order-2",
		Symbol:     "ETH/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(3000),
		FillSize:   decimal.NewFromFloat(2),
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill1)
	require.NoError(t, err)

	err = tracker.OnFill(context.Background(), fill2)
	require.NoError(t, err)

	tracker.ClosePosition(context.Background(), "pos-1")

	allPositions := tracker.GetAllPositions()
	assert.Len(t, allPositions, 2)
}

func TestPositionTracker_GetPosition_NotFound(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	_, exists := tracker.GetPosition("nonexistent")
	assert.False(t, exists)
}

func TestPositionTracker_CalculateUnrealizedPL_ZeroSize(t *testing.T) {
	tracker, _, cleanup := setupPositionTrackerTest(t)
	defer cleanup()

	fill := FillData{
		PositionID: "pos-123",
		OrderID:    "order-456",
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       "BUY",
		FillPrice:  decimal.NewFromFloat(50000),
		FillSize:   decimal.Zero,
		Timestamp:  time.Now().UTC(),
	}

	err := tracker.OnFill(context.Background(), fill)
	require.NoError(t, err)

	position, _ := tracker.GetPosition("pos-123")
	assert.True(t, position.UnrealizedPL.IsZero())
}

func TestDefaultPositionTrackerConfig(t *testing.T) {
	config := DefaultPositionTrackerConfig()

	assert.Equal(t, 30*time.Second, config.SyncInterval)
	assert.Equal(t, "position_tracker", config.RedisKeyPrefix)
	assert.True(t, config.EnableRealTimeSync)
}
