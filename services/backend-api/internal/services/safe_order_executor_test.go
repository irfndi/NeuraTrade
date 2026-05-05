package services

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockScalpingOrderExecutor is a mock implementation of ScalpingOrderExecutor
type MockScalpingOrderExecutor struct {
	mock.Mock
}

func (m *MockScalpingOrderExecutor) PlaceOrder(
	ctx context.Context,
	exchange, symbol, side, orderType string,
	amount decimal.Decimal,
	price *decimal.Decimal,
) (string, error) {
	args := m.Called(ctx, exchange, symbol, side, orderType, amount, price)
	return args.String(0), args.Error(1)
}

func (m *MockScalpingOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	args := m.Called(ctx, exchange, symbol)
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

func (m *MockScalpingOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

func (m *MockScalpingOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	args := m.Called(ctx, exchange, orderID)
	return args.Error(0)
}

func (m *MockScalpingOrderExecutor) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	args := m.Called(ctx, details)
	return args.String(0), args.Error(1)
}

func (m *MockScalpingOrderExecutor) IsPaperTrading() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockScalpingOrderExecutor) SyncPositionProtection(
	ctx context.Context,
	exchange string,
	position ManagedOpenPosition,
	stopLoss decimal.Decimal,
	takeProfit decimal.Decimal,
) error {
	args := m.Called(ctx, exchange, position, stopLoss, takeProfit)
	return args.Error(0)
}

// mockSafetyChecker implements PortfolioSafetyChecker for testing
type mockSafetyChecker struct {
	mock.Mock
}

func (m *mockSafetyChecker) CanExecuteTrade(ctx context.Context, chatID, exchange, symbol, marketType string, size decimal.Decimal) (bool, string, error) {
	args := m.Called(ctx, chatID, exchange, symbol, marketType, size)
	return args.Bool(0), args.String(1), args.Error(2)
}

type mockLeverageSafetyChecker struct {
	mock.Mock
}

func (m *mockLeverageSafetyChecker) CanExecuteTrade(ctx context.Context, chatID, exchange, symbol, marketType string, size decimal.Decimal) (bool, string, error) {
	args := m.Called(ctx, chatID, exchange, symbol, marketType, size)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *mockLeverageSafetyChecker) CanExecuteTradeWithLeverage(ctx context.Context, chatID, exchange, symbol, marketType string, leverage int, size decimal.Decimal) (bool, string, error) {
	args := m.Called(ctx, chatID, exchange, symbol, marketType, leverage, size)
	return args.Bool(0), args.String(1), args.Error(2)
}

type mockDetailedSafetyChecker struct {
	mock.Mock
}

func (m *mockDetailedSafetyChecker) CanExecuteTrade(ctx context.Context, chatID, exchange, symbol, marketType string, size decimal.Decimal) (bool, string, error) {
	args := m.Called(ctx, chatID, exchange, symbol, marketType, size)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *mockDetailedSafetyChecker) EvaluateTradeWithLeverage(ctx context.Context, chatID, exchange, symbol, marketType string, leverage int, size decimal.Decimal) (TradeSafetyDecision, error) {
	args := m.Called(ctx, chatID, exchange, symbol, marketType, leverage, size)
	return args.Get(0).(TradeSafetyDecision), args.Error(1)
}

func TestSafeOrderExecutor_AllowsWhenNoSafetyService(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	safeExec := NewSafeOrderExecutor(mockExecutor, nil, "test-chat")

	mockExecutor.On("PlaceOrder", mock.Anything, "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(100), (*decimal.Decimal)(nil)).Return("order-123", nil)

	orderID, err := safeExec.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(100), nil)

	assert.NoError(t, err)
	assert.Equal(t, "order-123", orderID)
	mockExecutor.AssertExpectations(t)
}

func TestSafeOrderExecutor_BlocksWhenSafetyCheckFails(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(100)).
		Return(false, "Trading halted due to max drawdown", nil)

	orderID, err := safeExec.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(100), nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio safety blocked")
	assert.Contains(t, err.Error(), "Trading halted due to max drawdown")
	assert.Equal(t, "", orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrder")
}

func TestSafeOrderExecutor_PlaceOrder_InferFuturesMarketTypeFromSymbol(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "bitget", "BTC/USDT:USDT", "futures", decimal.NewFromFloat(100)).
		Return(true, "", nil)
	mockExecutor.On("PlaceOrder", mock.Anything, "bitget", "BTC/USDT:USDT", "buy", "market", decimal.NewFromFloat(100), (*decimal.Decimal)(nil)).Return("order-futures", nil)

	orderID, err := safeExec.PlaceOrder(context.Background(), "bitget", "BTC/USDT:USDT", "buy", "market", decimal.NewFromFloat(100), nil)

	assert.NoError(t, err)
	assert.Equal(t, "order-futures", orderID)
	mockExecutor.AssertExpectations(t)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_BlocksOnDailyLossLimit(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(100)).
		Return(false, "Daily loss limit exceeded: 150.00/100.00", nil)

	orderID, err := safeExec.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(100), nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Daily loss limit exceeded")
	assert.Equal(t, "", orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrder")
}

func TestSafeOrderExecutor_BlocksOnOversizedPosition(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(1000)).
		Return(false, "Position size 1000.00 exceeds maximum allowed 500.00 (throttled to 50%)", nil)

	orderID, err := safeExec.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(1000), nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed")
	assert.Equal(t, "", orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrder")
}

func TestSafeOrderExecutor_AllowsWhenSafetyPasses(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(100)).
		Return(true, "", nil)

	mockExecutor.On("PlaceOrder", mock.Anything, "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(100), (*decimal.Decimal)(nil)).Return("order-456", nil)

	orderID, err := safeExec.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(100), nil)

	assert.NoError(t, err)
	assert.Equal(t, "order-456", orderID)
	mockExecutor.AssertExpectations(t)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_FailsClosedOnError(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(100)).
		Return(false, "", errors.New("safety service unavailable"))

	orderID, err := safeExec.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(100), nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "safety check failed")
	assert.Equal(t, "", orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrder")
}

func TestSafeOrderExecutor_CheckSafety(t *testing.T) {
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(nil, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(50)).
		Return(false, "Drawdown halt active", nil)

	allowed, reason, err := safeExec.CheckSafety(context.Background(), "binance", "BTC/USDT", decimal.NewFromFloat(50))

	assert.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, "Drawdown halt active", reason)
}

func TestSafeOrderExecutor_PlaceOrderWithSafetyCheck(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(100)).
		Return(false, "Trading not allowed", nil)

	orderID, result, err := safeExec.PlaceOrderWithSafetyCheck(
		context.Background(),
		"binance", "BTC/USDT", "buy", "market",
		decimal.NewFromFloat(100), nil,
	)

	assert.NoError(t, err)
	assert.Empty(t, orderID)
	assert.NotNil(t, result)
	assert.False(t, result.Allowed)
	assert.Equal(t, "Trading not allowed", result.Reason)
}

func TestSafeOrderExecutor_PlaceOrderWithSafetyCheck_InferFuturesMarketTypeFromSymbol(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "bitget", "ETH/USDT:USDT", "futures", decimal.NewFromFloat(25)).
		Return(false, "below exchange minimum notional", nil)

	orderID, result, err := safeExec.PlaceOrderWithSafetyCheck(
		context.Background(),
		"bitget", "ETH/USDT:USDT", "buy", "market",
		decimal.NewFromFloat(25), nil,
	)

	assert.NoError(t, err)
	assert.Empty(t, orderID)
	assert.NotNil(t, result)
	assert.False(t, result.Allowed)
	assert.Equal(t, "below exchange minimum notional", result.Reason)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_CheckSafety_InfersSpotMarketTypeForBitgetPlainSymbolWithoutContext(t *testing.T) {
	mockSafety := &mockSafetyChecker{}
	safeExec := NewSafeOrderExecutor(nil, mockSafety, "test-chat")

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "bitget", "DOGE/USDT", "spot", decimal.NewFromFloat(15)).
		Return(true, "", nil)

	allowed, reason, err := safeExec.CheckSafety(context.Background(), "bitget", "DOGE/USDT", decimal.NewFromFloat(15))

	assert.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_CheckSafety_UsesScopedFuturesMarketTypeForBitgetPlainSymbol(t *testing.T) {
	mockSafety := &mockSafetyChecker{}
	safeExec := NewSafeOrderExecutor(nil, mockSafety, "test-chat")
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:     "test-chat",
		Exchange:   "bitget",
		MarketType: "futures",
	})

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "bitget", "DOGE/USDT", "futures", decimal.NewFromFloat(15)).
		Return(false, "below exchange minimum notional", nil)

	allowed, reason, err := safeExec.CheckSafety(ctx, "bitget", "DOGE/USDT", decimal.NewFromFloat(15))

	assert.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, "below exchange minimum notional", reason)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_PlaceOrderWithSafetyCheck_UsesScopedLeverageAwareSafety(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockLeverageSafetyChecker{}

	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "test-chat",
		Exchange: "bitget",
		Leverage: 5,
	})

	mockSafety.On("CanExecuteTradeWithLeverage", mock.Anything, "test-chat", "bitget", "ETH/USDT:USDT", "futures", 5, decimal.NewFromFloat(25)).
		Return(true, "", nil)
	mockExecutor.On("PlaceOrder", mock.Anything, "bitget", "ETH/USDT:USDT", "buy", "market", decimal.NewFromFloat(25), (*decimal.Decimal)(nil)).Return("order-123", nil)

	orderID, result, err := safeExec.PlaceOrderWithSafetyCheck(
		ctx,
		"bitget", "ETH/USDT:USDT", "buy", "market",
		decimal.NewFromFloat(25), nil,
	)

	assert.NoError(t, err)
	assert.Equal(t, "order-123", orderID)
	assert.NotNil(t, result)
	assert.True(t, result.Allowed)
	assert.Empty(t, result.Reason)
	mockExecutor.AssertExpectations(t)
	mockSafety.AssertExpectations(t)
	mockSafety.AssertNotCalled(t, "CanExecuteTrade", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestSafeOrderExecutor_SetChatID(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	safeExec := NewSafeOrderExecutor(mockExecutor, nil, "original-chat")

	safeExec.SetChatID("new-chat")

	assert.Equal(t, "new-chat", safeExec.GetChatID())
}

func TestSafeOrderExecutor_UsesChatIDFromScopedContext(t *testing.T) {
	mockSafety := &mockSafetyChecker{}
	safeExec := NewSafeOrderExecutor(nil, mockSafety, "default-chat")
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "scoped-chat",
		Exchange: "binance",
	})

	mockSafety.On("CanExecuteTrade", mock.Anything, "scoped-chat", "binance", "BTC/USDT", "", decimal.NewFromFloat(50)).
		Return(true, "", nil)

	allowed, reason, err := safeExec.CheckSafety(ctx, "binance", "BTC/USDT", decimal.NewFromFloat(50))

	assert.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_BlocksWhenSafetyFails(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "BTC/USDT",
		AmountUSDT: decimal.NewFromFloat(100),
	}

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "bitget", "BTC/USDT", "futures", decimal.NewFromFloat(100)).
		Return(false, "Trading halted due to max drawdown", nil)

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), details)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio safety blocked")
	assert.Empty(t, orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrderWithDetails")
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_BypassesZeroMaxSafetyForBitgetFutures(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockDetailedSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "OPN/USDT:USDT",
		AmountUSDT: decimal.NewFromFloat(6.0),
	}

	mockSafety.On("EvaluateTradeWithLeverage", mock.Anything, "test-chat", "bitget", "OPN/USDT:USDT", "futures", 0, decimal.NewFromFloat(6.0)).
		Return(TradeSafetyDecision{
			Allowed:                  true,
			Reason:                   "",
			ZeroMaxMinNotionalBypass: true,
		}, nil)
	mockExecutor.On("PlaceOrderWithDetails", mock.Anything, details).Return("order-fallback", nil)

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), details)

	assert.NoError(t, err)
	assert.Equal(t, "order-fallback", orderID)
	mockExecutor.AssertExpectations(t)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_BypassesSafetyForPaperMode(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockDetailedSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "BTC/USDT",
		AmountUSDT: decimal.NewFromFloat(8.5),
	}

	mockExecutor.On("PlaceOrderWithDetails", mock.Anything, details).Return("paper-order", nil)

	orderID, err := safeExec.PlaceOrderWithDetails(WithOperationalMode(context.Background(), ModePaper), details)

	assert.NoError(t, err)
	assert.Equal(t, "paper-order", orderID)
	mockExecutor.AssertExpectations(t)
	mockSafety.AssertNotCalled(t, "EvaluateTradeWithLeverage")
	mockSafety.AssertNotCalled(t, "CanExecuteTrade")
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_DoesNotBypassWhenAmountExceedsBoundedCap(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockDetailedSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "OPN/USDT:USDT",
		AmountUSDT: decimal.NewFromFloat(100.0),
	}

	mockSafety.On("EvaluateTradeWithLeverage", mock.Anything, "test-chat", "bitget", "OPN/USDT:USDT", "futures", 0, decimal.NewFromFloat(100.0)).
		Return(TradeSafetyDecision{
			Allowed:                  false,
			Reason:                   "Position size 100.00 exceeds maximum allowed 0.00 (throttled to 0%)",
			ZeroMaxMinNotionalBypass: false,
		}, nil)

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), details)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio safety blocked")
	assert.Empty(t, orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrderWithDetails")
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_DoesNotBypassWhenMaximumAllowedIsNonZero(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockDetailedSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "OPN/USDT:USDT",
		AmountUSDT: decimal.NewFromFloat(6.0),
	}

	mockSafety.On("EvaluateTradeWithLeverage", mock.Anything, "test-chat", "bitget", "OPN/USDT:USDT", "futures", 0, decimal.NewFromFloat(6.0)).
		Return(TradeSafetyDecision{
			Allowed:                  false,
			Reason:                   "Position size 6.00 exceeds maximum allowed 0.50 (throttled to 0%)",
			ZeroMaxMinNotionalBypass: false,
		}, nil)

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), details)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio safety blocked")
	assert.Empty(t, orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrderWithDetails")
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_UsesLeverageAwareSafetyWithoutRedundantBaseCheck(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockLeverageSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "ETH/USDT",
		Leverage:   5,
		AmountUSDT: decimal.NewFromFloat(75),
	}

	mockSafety.On("CanExecuteTradeWithLeverage", mock.Anything, "test-chat", "bitget", "ETH/USDT", "futures", 5, decimal.NewFromFloat(75)).
		Return(true, "", nil)
	mockExecutor.On("PlaceOrderWithDetails", mock.Anything, details).Return("order-789", nil)

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), details)

	assert.NoError(t, err)
	assert.Equal(t, "order-789", orderID)
	mockExecutor.AssertExpectations(t)
	mockSafety.AssertExpectations(t)
	mockSafety.AssertNotCalled(t, "CanExecuteTrade", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_LeverageAwareFallbackStillBlocksWithoutDetailedDecision(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockLeverageSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "ETH/USDT",
		Leverage:   5,
		AmountUSDT: decimal.NewFromFloat(75),
	}

	mockSafety.On("CanExecuteTradeWithLeverage", mock.Anything, "test-chat", "bitget", "ETH/USDT", "futures", 5, decimal.NewFromFloat(75)).
		Return(false, "below exchange minimum notional", nil)

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), details)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio safety blocked")
	assert.Empty(t, orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrderWithDetails")
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_AllowsWhenSafetyPasses(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	mockSafety := &mockSafetyChecker{}
	safeExec := NewSafeOrderExecutor(mockExecutor, mockSafety, "test-chat")

	details := TradeDetails{
		Exchange:   "bitget",
		MarketType: "futures",
		Symbol:     "ETH/USDT",
		AmountUSDT: decimal.NewFromFloat(75),
	}

	mockSafety.On("CanExecuteTrade", mock.Anything, "test-chat", "bitget", "ETH/USDT", "futures", decimal.NewFromFloat(75)).
		Return(true, "", nil)
	mockExecutor.On("PlaceOrderWithDetails", mock.Anything, details).Return("order-789", nil)

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), details)

	assert.NoError(t, err)
	assert.Equal(t, "order-789", orderID)
	mockExecutor.AssertExpectations(t)
	mockSafety.AssertExpectations(t)
}

func TestSafeOrderExecutor_PlaceOrderWithDetails_RejectsNonPositiveSize(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	safeExec := NewSafeOrderExecutor(mockExecutor, nil, "test-chat")

	orderID, err := safeExec.PlaceOrderWithDetails(context.Background(), TradeDetails{
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		AmountUSDT: decimal.Zero,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid order size")
	assert.Empty(t, orderID)
	mockExecutor.AssertNotCalled(t, "PlaceOrderWithDetails")
}

func TestSafetyCheckResult_String(t *testing.T) {
	result := &SafetyCheckResult{
		Allowed: false,
		Reason:  "Daily loss limit exceeded",
	}

	assert.False(t, result.Allowed)
	assert.Equal(t, "Daily loss limit exceeded", result.Reason)
}

func TestSafeOrderExecutor_SyncPositionProtection(t *testing.T) {
	mockExecutor := new(MockScalpingOrderExecutor)
	safeExec := NewSafeOrderExecutor(mockExecutor, nil, "test-chat")

	position := ManagedOpenPosition{
		PositionID: "pos-1",
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
	}
	stopLoss := decimal.NewFromFloat(50000)
	takeProfit := decimal.NewFromFloat(53000)
	mockExecutor.On("SyncPositionProtection", mock.Anything, "bitget", position, stopLoss, takeProfit).Return(nil)

	err := safeExec.SyncPositionProtection(context.Background(), "bitget", position, stopLoss, takeProfit)
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}
