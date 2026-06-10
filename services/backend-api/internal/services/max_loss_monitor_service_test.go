package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockTicker implements ccxt.MarketPriceInterface for testing.
type mockTicker struct {
	price decimal.Decimal
}

func (m *mockTicker) GetPrice() decimal.Decimal { return m.price }
func (m *mockTicker) GetVolume() decimal.Decimal { return decimal.NewFromInt(1000) }
func (m *mockTicker) GetTimestamp() time.Time { return time.Now() }
func (m *mockTicker) GetExchangeName() string  { return "binance" }
func (m *mockTicker) GetSymbol() string    { return "BTC/USDT" }
func (m *mockTicker) GetBid() decimal.Decimal   { return m.price }
func (m *mockTicker) GetAsk() decimal.Decimal   { return m.price }
func (m *mockTicker) GetHigh() decimal.Decimal   { return m.price }
func (m *mockTicker) GetLow() decimal.Decimal    { return m.price }
func (m *mockTicker) GetPriceChange24h() float64 { return 0.0 }

// mockCCXTForMaxLoss implements ccxt.CCXTService for MaxLossMonitor testing.
type mockCCXTForMaxLoss struct {
	mock.Mock
	positions     *ccxt.PositionsResponse
	ticker        ccxt.MarketPriceInterface
	tickerError   error
	positionsErr  error
}

func (m *mockCCXTForMaxLoss) Initialize(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockCCXTForMaxLoss) IsHealthy(ctx context.Context) bool {
	return true
}

func (m *mockCCXTForMaxLoss) Close() error {
	return nil
}

func (m *mockCCXTForMaxLoss) GetServiceURL() string {
	return "http://mock"
}

func (m *mockCCXTForMaxLoss) GetSupportedExchanges() []string {
	return []string{"binance"}
}

func (m *mockCCXTForMaxLoss) GetExchangeInfo(exchangeID string) (ccxt.ExchangeInfo, bool) {
	return ccxt.ExchangeInfo{}, true
}

func (m *mockCCXTForMaxLoss) GetExchangeConfig(ctx context.Context) (*ccxt.ExchangeConfigResponse, error) {
	return &ccxt.ExchangeConfigResponse{}, nil
}

func (m *mockCCXTForMaxLoss) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	return &ccxt.ExchangeManagementResponse{}, nil
}

func (m *mockCCXTForMaxLoss) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	return &ccxt.ExchangeManagementResponse{}, nil
}

func (m *mockCCXTForMaxLoss) RefreshExchanges(ctx context.Context) (*ccxt.ExchangeManagementResponse, error) {
	return &ccxt.ExchangeManagementResponse{}, nil
}

func (m *mockCCXTForMaxLoss) AddExchange(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	return &ccxt.ExchangeManagementResponse{}, nil
}

func (m *mockCCXTForMaxLoss) FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]ccxt.MarketPriceInterface, error) {
	return nil, nil
}

func (m *mockCCXTForMaxLoss) FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error) {
	if m.tickerError != nil {
		return nil, m.tickerError
	}
	return m.ticker, nil
}

func (m *mockCCXTForMaxLoss) FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookResponse, error) {
	return &ccxt.OrderBookResponse{}, nil
}

func (m *mockCCXTForMaxLoss) CalculateOrderBookMetrics(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookMetrics, error) {
	return &ccxt.OrderBookMetrics{}, nil
}

func (m *mockCCXTForMaxLoss) FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*ccxt.OHLCVResponse, error) {
	return &ccxt.OHLCVResponse{}, nil
}

func (m *mockCCXTForMaxLoss) FetchTrades(ctx context.Context, exchange, symbol string, limit int) (*ccxt.TradesResponse, error) {
	return &ccxt.TradesResponse{}, nil
}

func (m *mockCCXTForMaxLoss) FetchMarkets(ctx context.Context, exchange string) (*ccxt.MarketsResponse, error) {
	return &ccxt.MarketsResponse{}, nil
}

func (m *mockCCXTForMaxLoss) FetchBalance(ctx context.Context, exchange string) (*ccxt.BalanceResponse, error) {
	return &ccxt.BalanceResponse{}, nil
}

func (m *mockCCXTForMaxLoss) FetchOpenOrders(ctx context.Context, exchange string) (*ccxt.OpenOrdersResponse, error) {
	return &ccxt.OpenOrdersResponse{}, nil
}

func (m *mockCCXTForMaxLoss) FetchOpenOrdersForSymbol(ctx context.Context, exchange, symbol string) (*ccxt.OpenOrdersResponse, error) {
	return &ccxt.OpenOrdersResponse{}, nil
}

func (m *mockCCXTForMaxLoss) CancelOrder(ctx context.Context, exchange, orderID, symbol string) error {
	return nil
}

func (m *mockCCXTForMaxLoss) FetchOrder(ctx context.Context, exchange, orderID, symbol string) (*ccxt.OrderResponse, error) {
	return &ccxt.OrderResponse{}, nil
}

func (m *mockCCXTForMaxLoss) FetchPositions(ctx context.Context, exchange string) (*ccxt.PositionsResponse, error) {
	if m.positionsErr != nil {
		return nil, m.positionsErr
	}
	return m.positions, nil
}

func (m *mockCCXTForMaxLoss) FetchFundingRate(ctx context.Context, exchange, symbol string) (*ccxt.FundingRate, error) {
	return &ccxt.FundingRate{}, nil
}

func (m *mockCCXTForMaxLoss) FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]ccxt.FundingRate, error) {
	return nil, nil
}

func (m *mockCCXTForMaxLoss) FetchAllFundingRates(ctx context.Context, exchange string) ([]ccxt.FundingRate, error) {
	return nil, nil
}

func (m *mockCCXTForMaxLoss) CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	return nil, nil
}

func (m *mockCCXTForMaxLoss) CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]ccxt.FundingArbitrageOpportunity, error) {
	return nil, nil
}

func TestMaxLossMonitorService_TracksPositions(t *testing.T) {
	mockCCXT := &mockCCXTForMaxLoss{
		positions: &ccxt.PositionsResponse{
			Exchange: "binance",
			Positions: []ccxt.Position{
				{
					ID:         "pos-1",
					Symbol:     "BTC/USDT",
					Side:       "buy",
					Size:       decimal.NewFromFloat(0.1),
					EntryPrice: decimal.NewFromFloat(50000),
				},
			},
		},
	}

	var called atomic.Int32
	callback := func(_ context.Context, exchange, symbol, side string, quantity decimal.Decimal) (*MaxLossExecutionResult, error) {
		called.Add(1)
		return &MaxLossExecutionResult{Success: true, ExecutedAt: time.Now().UTC()}, nil
	}

	config := MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 50 * time.Millisecond,
	}

	service := NewMaxLossMonitorService(config, mockCCXT, zaplogrus.New(), callback)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)

	require.Eventually(t, func() bool {
		return service.TrackedCount() > 0
	}, 2*time.Second, 10*time.Millisecond, "position should be tracked")

	service.Stop()
	assert.GreaterOrEqual(t, service.TrackedCount(), 1, "at least one position should be tracked")
}

func TestMaxLossMonitorService_FiresOnMaxLoss(t *testing.T) {
	entryPrice := decimal.NewFromFloat(100)
	breachPrice := decimal.NewFromFloat(98)

	mockCCXT := &mockCCXTForMaxLoss{
		positions: &ccxt.PositionsResponse{
			Exchange: "binance",
			Positions: []ccxt.Position{
				{
					ID:         "pos-2",
					Symbol:     "ETH/USDT",
					Side:       "buy",
					Size:       decimal.NewFromFloat(1),
					EntryPrice: entryPrice,
				},
			},
		},
		ticker: &mockTicker{price: breachPrice},
	}

	var (
		mu     sync.Mutex
		called bool
		params struct {
			exchange, symbol, side string
			quantity               decimal.Decimal
		}
	)
	callback := func(_ context.Context, exchange, symbol, side string, quantity decimal.Decimal) (*MaxLossExecutionResult, error) {
		mu.Lock()
		defer mu.Unlock()
		called = true
		params.exchange = exchange
		params.symbol = symbol
		params.side = side
		params.quantity = quantity
		return &MaxLossExecutionResult{Success: true, ExecutedAt: time.Now().UTC()}, nil
	}

	config := MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 50 * time.Millisecond,
	}

	service := NewMaxLossMonitorService(config, mockCCXT, zaplogrus.New(), callback)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return called
	}, 2*time.Second, 10*time.Millisecond, "execution callback should be called on max-loss breach")

	service.Stop()
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, called, "callback should have been called")
	assert.Equal(t, "binance", params.exchange)
	assert.Equal(t, "ETH/USDT", params.symbol)
	assert.Equal(t, "sell", params.side, "should be opposite side for long position")
	assert.True(t, params.quantity.Equal(decimal.NewFromFloat(1)))
}

func TestMaxLossMonitorService_NotFiredWithinThreshold(t *testing.T) {
	entryPrice := decimal.NewFromFloat(100)
	priceWithinThreshold := decimal.NewFromFloat(99)

	mockCCXT := &mockCCXTForMaxLoss{
		positions: &ccxt.PositionsResponse{
			Exchange: "binance",
			Positions: []ccxt.Position{
				{
					ID:         "pos-3",
					Symbol:     "SOL/USDT",
					Side:       "buy",
					Size:       decimal.NewFromFloat(10),
					EntryPrice: entryPrice,
				},
			},
		},
		ticker: &mockTicker{price: priceWithinThreshold},
	}

	var called atomic.Int32
	callback := func(_ context.Context, _, _, _ string, _ decimal.Decimal) (*MaxLossExecutionResult, error) {
		called.Add(1)
		return &MaxLossExecutionResult{Success: true, ExecutedAt: time.Now().UTC()}, nil
	}

	config := MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 50 * time.Millisecond,
	}

	service := NewMaxLossMonitorService(config, mockCCXT, zaplogrus.New(), callback)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	service.Stop()
	assert.Equal(t, int32(0), called.Load(), "callback should not be called when loss is within threshold")
}

func TestMaxLossMonitorService_SkipsZeroPricePositions(t *testing.T) {
	mockCCXT := &mockCCXTForMaxLoss{
		positions: &ccxt.PositionsResponse{
			Exchange: "binance",
			Positions: []ccxt.Position{
				{
					ID:         "pos-zero",
					Symbol:     "BNB/USDT",
					Side:       "buy",
					Size:       decimal.Zero,
					EntryPrice: decimal.NewFromFloat(500),
				},
			},
		},
	}

	service := MaxLossMonitorService{
		config:      MaxLossMonitorConfig{MaxLossPct: 0.015, PollInterval: 50 * time.Millisecond},
		ccxtService: mockCCXT,
		monitor:     risk.NewMaxLossMonitor(risk.MaxLossMonitorConfig{MaxLossPct: 0.015, PollInterval: 50 * time.Millisecond}, nil, nil),
		logger:      zaplogrus.New(),
		stopCh:      make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	service.Stop()
	assert.Equal(t, 0, service.TrackedCount(), "zero-size positions should not be tracked")
}

func TestMaxLossMonitorService_HandlesFetchError(t *testing.T) {
	mockCCXT := &mockCCXTForMaxLoss{
		positionsErr: assert.AnError,
	}

	var called atomic.Int32
	callback := func(_ context.Context, _, _, _ string, _ decimal.Decimal) (*MaxLossExecutionResult, error) {
		called.Add(1)
		return &MaxLossExecutionResult{Success: true, ExecutedAt: time.Now().UTC()}, nil
	}

	config := MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 50 * time.Millisecond,
	}

	service := NewMaxLossMonitorService(config, mockCCXT, zaplogrus.New(), callback)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	service.Stop()
	assert.Equal(t, int32(0), called.Load(), "callback should not be called when positions fetch fails")
	assert.Equal(t, 0, service.TrackedCount(), "no positions should be tracked when fetch fails")
}
