package risk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMarketDataGateway implements ports.MarketDataGateway for tests.
type mockMarketDataGateway struct {
	tick        ports.Tick
	err         error
	callCount   int
	mu          sync.Mutex
}

func (m *mockMarketDataGateway) FetchTick(_ context.Context, _, _ string) (ports.Tick, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return ports.Tick{}, m.err
	}
	return m.tick, nil
}

func (m *mockMarketDataGateway) FetchOHLCV(_ context.Context, _, _, _ string, _ time.Time, _ int) ([]ports.Candle, error) {
	return nil, nil
}
func (m *mockMarketDataGateway) FetchOrderBook(_ context.Context, _, _ string, _ int) (ports.OrderBook, error) {
	return ports.OrderBook{}, nil
}
func (m *mockMarketDataGateway) FetchFundingRate(_ context.Context, _, _ string) (ports.FundingRate, error) {
	return ports.FundingRate{}, nil
}
func (m *mockMarketDataGateway) FetchFundingRates(_ context.Context, _ string, _ []string) ([]ports.FundingRate, error) {
	return nil, nil
}
func (m *mockMarketDataGateway) IsHealthy(_ context.Context) bool { return true }

// mockTradingGateway implements ports.TradingGateway for tests.
type mockTradingGateway struct {
	placed   []ports.OrderRequest
	err      error
	mu       sync.Mutex
}

func (m *mockTradingGateway) PlaceOrder(_ context.Context, req ports.OrderRequest) (ports.OrderResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placed = append(m.placed, req)
	if m.err != nil {
		return ports.OrderResult{}, m.err
	}
	return ports.OrderResult{
		Exchange: req.Exchange,
		OrderID:  "test-order-1",
		Symbol:   req.Symbol,
		Side:     req.Side,
		Type:     req.Type,
		Amount:   req.Amount,
		Status:   ports.OrderStatusFilled,
	}, nil
}
func (m *mockTradingGateway) CancelOrder(_ context.Context, _, _ string) error    { return nil }
func (m *mockTradingGateway) CancelAllOrders(_ context.Context, _, _ string) error { return nil }
func (m *mockTradingGateway) FetchOrder(_ context.Context, _, _ string) (ports.Order, error) {
	return ports.Order{}, nil
}
func (m *mockTradingGateway) FetchOpenOrders(_ context.Context, _, _ string) ([]ports.Order, error) {
	return nil, nil
}
func (m *mockTradingGateway) FetchPositions(_ context.Context, _ string) ([]ports.Position, error) {
	return nil, nil
}
func (m *mockTradingGateway) FetchBalances(_ context.Context, _ string) ([]ports.Balance, error) {
	return nil, nil
}
func (m *mockTradingGateway) IsHealthy(_ context.Context) bool { return true }

// mockExchangeRegistry implements ports.ExchangeRegistry for tests.
type mockExchangeRegistry struct {
	md map[string]ports.MarketDataGateway
	tg map[string]ports.TradingGateway
}

func (m *mockExchangeRegistry) GetGateway(_ string) (ports.ExchangeGateway, error) {
	return nil, nil
}
func (m *mockExchangeRegistry) GetMarketDataGateway(exchange string) (ports.MarketDataGateway, error) {
	if gw, ok := m.md[exchange]; ok {
		return gw, nil
	}
	return nil, errors.New("not found")
}
func (m *mockExchangeRegistry) GetTradingGateway(exchange string) (ports.TradingGateway, error) {
	if gw, ok := m.tg[exchange]; ok {
		return gw, nil
	}
	return nil, errors.New("not found")
}
func (m *mockExchangeRegistry) ListExchanges(_ context.Context) ([]ports.ExchangeInfo, error) {
	return nil, nil
}
func (m *mockExchangeRegistry) IsExchangeEnabled(_ string) bool { return true }

func TestExchangePriceProvider_GetPrice_Success(t *testing.T) {
	md := &mockMarketDataGateway{
		tick: ports.Tick{Exchange: "binance", Symbol: "BTC/USDT", Last: decimal.NewFromFloat(50000)},
	}
	registry := &mockExchangeRegistry{md: map[string]ports.MarketDataGateway{"binance": md}}
	provider := NewExchangePriceProvider(registry)

	price, err := provider.GetPrice(context.Background(), "binance", "BTC/USDT")
	require.NoError(t, err)
	assert.True(t, price.Equal(decimal.NewFromFloat(50000)))
	assert.Equal(t, 1, md.callCount)
}

func TestExchangePriceProvider_GetPrice_NilRegistry(t *testing.T) {
	provider := NewExchangePriceProvider(nil)
	_, err := provider.GetPrice(context.Background(), "binance", "BTC/USDT")
	assert.Error(t, err)
}

func TestExchangePriceProvider_GetPrice_GatewayNotFound(t *testing.T) {
	registry := &mockExchangeRegistry{md: map[string]ports.MarketDataGateway{}}
	provider := NewExchangePriceProvider(registry)
	_, err := provider.GetPrice(context.Background(), "binance", "BTC/USDT")
	assert.Error(t, err)
}

func TestExchangePriceProvider_GetPrice_FetchError(t *testing.T) {
	md := &mockMarketDataGateway{err: errors.New("network failure")}
	registry := &mockExchangeRegistry{md: map[string]ports.MarketDataGateway{"binance": md}}
	provider := NewExchangePriceProvider(registry)
	_, err := provider.GetPrice(context.Background(), "binance", "BTC/USDT")
	assert.Error(t, err)
}

func TestExchangePriceProvider_GetPrice_ZeroPrice(t *testing.T) {
	md := &mockMarketDataGateway{tick: ports.Tick{Last: decimal.Zero}}
	registry := &mockExchangeRegistry{md: map[string]ports.MarketDataGateway{"binance": md}}
	provider := NewExchangePriceProvider(registry)
	_, err := provider.GetPrice(context.Background(), "binance", "BTC/USDT")
	assert.Error(t, err)
}

func TestGatewayPositionCloser_ClosePosition_Buy(t *testing.T) {
	tg := &mockTradingGateway{}
	registry := &mockExchangeRegistry{tg: map[string]ports.TradingGateway{"binance": tg}}
	closer := NewGatewayPositionCloser(registry)

	pos := PositionSnapshot{
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		EntryPrice: decimal.NewFromFloat(50000),
		Quantity:   decimal.NewFromFloat(0.1),
	}
	err := closer.ClosePosition(context.Background(), pos, "test")
	require.NoError(t, err)
	require.Len(t, tg.placed, 1)
	assert.Equal(t, ports.OrderSideSell, tg.placed[0].Side)
	assert.Equal(t, ports.OrderTypeMarket, tg.placed[0].Type)
	assert.True(t, tg.placed[0].ReduceOnly)
}

func TestGatewayPositionCloser_ClosePosition_Sell(t *testing.T) {
	tg := &mockTradingGateway{}
	registry := &mockExchangeRegistry{tg: map[string]ports.TradingGateway{"binance": tg}}
	closer := NewGatewayPositionCloser(registry)

	pos := PositionSnapshot{
		Exchange:   "binance",
		Symbol:     "ETH/USDT",
		Side:       "sell",
		EntryPrice: decimal.NewFromFloat(3000),
		Quantity:   decimal.NewFromFloat(1),
	}
	err := closer.ClosePosition(context.Background(), pos, "test")
	require.NoError(t, err)
	require.Len(t, tg.placed, 1)
	assert.Equal(t, ports.OrderSideBuy, tg.placed[0].Side)
}

func TestGatewayPositionCloser_ClosePosition_NilRegistry(t *testing.T) {
	closer := NewGatewayPositionCloser(nil)
	pos := PositionSnapshot{Exchange: "binance", Symbol: "BTC/USDT", Side: "buy"}
	err := closer.ClosePosition(context.Background(), pos, "test")
	assert.Error(t, err)
}

func TestGatewayPositionCloser_ClosePosition_GatewayNotFound(t *testing.T) {
	registry := &mockExchangeRegistry{tg: map[string]ports.TradingGateway{}}
	closer := NewGatewayPositionCloser(registry)
	pos := PositionSnapshot{Exchange: "binance", Symbol: "BTC/USDT", Side: "buy"}
	err := closer.ClosePosition(context.Background(), pos, "test")
	assert.Error(t, err)
}

func TestGatewayPositionCloser_ClosePosition_PlaceOrderError(t *testing.T) {
	tg := &mockTradingGateway{err: errors.New("order rejected")}
	registry := &mockExchangeRegistry{tg: map[string]ports.TradingGateway{"binance": tg}}
	closer := NewGatewayPositionCloser(registry)
	pos := PositionSnapshot{Exchange: "binance", Symbol: "BTC/USDT", Side: "buy"}
	err := closer.ClosePosition(context.Background(), pos, "test")
	assert.Error(t, err)
}

func TestMaxLossMonitorWiring_EndToEnd(t *testing.T) {
	md := &mockMarketDataGateway{
		tick: ports.Tick{Last: decimal.NewFromFloat(98)},
	}
	tg := &mockTradingGateway{}
	registry := &mockExchangeRegistry{
		md: map[string]ports.MarketDataGateway{"binance": md},
		tg: map[string]ports.TradingGateway{"binance": tg},
	}
	priceProvider := NewExchangePriceProvider(registry)
	positionCloser := NewGatewayPositionCloser(registry)

	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, priceProvider.GetPrice, positionCloser.ClosePosition)

	monitor.TrackPosition(PositionSnapshot{
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		EntryPrice: decimal.NewFromFloat(100),
		Quantity:   decimal.NewFromFloat(0.1),
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	require.Eventually(t, func() bool {
		return len(tg.placed) > 0
	}, 500*time.Millisecond, 10*time.Millisecond)

	cancel()
	<-errCh

	require.Len(t, tg.placed, 1)
	assert.Equal(t, ports.OrderSideSell, tg.placed[0].Side)
	assert.Equal(t, "binance", tg.placed[0].Exchange)
	assert.Equal(t, "BTC/USDT", tg.placed[0].Symbol)
	assert.Equal(t, 0, monitor.TrackedCount(), "position should be untracked after close")
}
