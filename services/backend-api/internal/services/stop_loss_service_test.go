package services

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type slTestCCXT struct {
	tickerPrice float64
}

func (m *slTestCCXT) Initialize(_ context.Context) error { return nil }
func (m *slTestCCXT) IsHealthy(_ context.Context) bool   { return true }
func (m *slTestCCXT) Close() error                       { return nil }
func (m *slTestCCXT) GetServiceURL() string              { return "" }
func (m *slTestCCXT) GetSupportedExchanges() []string    { return nil }
func (m *slTestCCXT) GetExchangeInfo(_ string) (ccxt.ExchangeInfo, bool) {
	return ccxt.ExchangeInfo{}, false
}
func (m *slTestCCXT) GetExchangeConfig(_ context.Context) (*ccxt.ExchangeConfigResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) AddExchangeToBlacklist(_ context.Context, _ string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) RemoveExchangeFromBlacklist(_ context.Context, _ string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) RefreshExchanges(_ context.Context) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) AddExchange(_ context.Context, _ string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchMarketData(_ context.Context, _ []string, _ []string) ([]ccxt.MarketPriceInterface, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchSingleTicker(_ context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error) {
	return &stubTicker{price: m.tickerPrice}, nil
}
func (m *slTestCCXT) FetchOrderBook(_ context.Context, _ string, _ string, _ int) (*ccxt.OrderBookResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) CalculateOrderBookMetrics(_ context.Context, _ string, _ string, _ int) (*ccxt.OrderBookMetrics, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchOHLCV(_ context.Context, _ string, _ string, _ string, _ int) (*ccxt.OHLCVResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchTrades(_ context.Context, _ string, _ string, _ int) (*ccxt.TradesResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchMarkets(_ context.Context, _ string) (*ccxt.MarketsResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchBalance(_ context.Context, _ string) (*ccxt.BalanceResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchOpenOrders(_ context.Context, _ string) (*ccxt.OpenOrdersResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchOpenOrdersForSymbol(_ context.Context, _ string, _ string) (*ccxt.OpenOrdersResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) CancelOrder(_ context.Context, _ string, _ string, _ string) error { return nil }
func (m *slTestCCXT) FetchOrder(_ context.Context, _ string, _ string, _ string) (*ccxt.OrderResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchPositions(_ context.Context, _ string) (*ccxt.PositionsResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchFundingRate(_ context.Context, _ string, _ string) (*ccxt.FundingRate, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchFundingRates(_ context.Context, _ string, _ []string) ([]ccxt.FundingRate, error) {
	return nil, nil
}
func (m *slTestCCXT) FetchAllFundingRates(_ context.Context, _ string) ([]ccxt.FundingRate, error) {
	return nil, nil
}
func (m *slTestCCXT) CalculateArbitrageOpportunities(_ context.Context, _ []string, _ []string, _ decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	return nil, nil
}
func (m *slTestCCXT) CalculateFundingRateArbitrage(_ context.Context, _ []string, _ []string, _ float64) ([]ccxt.FundingArbitrageOpportunity, error) {
	return nil, nil
}

var _ ccxt.CCXTService = (*slTestCCXT)(nil)

func newTestStopLossService(redisClient *redis.Client) *StopLossService {
	config := DefaultStopLossConfig()
	return NewStopLossService(config, &slTestCCXT{tickerPrice: 50000.0}, zaplogrus.New(), nil, redisClient)
}

func newTestStopLossParams() StopLossParams {
	return StopLossParams{
		PositionID:   "pos-test-1",
		Symbol:       "BTC/USDT",
		Exchange:     "bitget",
		Side:         "long",
		EntryPrice:   decimal.NewFromInt(50000),
		PositionSize: decimal.NewFromInt(1),
		StopLossPct:  decimal.NewFromFloat(0.02),
		IsTrailing:   false,
	}
}

func TestStopLossService_CreateStopLoss_PersistsToRedis(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = rc.Close() }()

	svc := newTestStopLossService(rc)
	ctx := t.Context()
	params := newTestStopLossParams()
	order, err := svc.CreateStopLoss(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, order)

	key := svc.keyForStopLoss(order.ID)
	data, err := rc.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Contains(t, data, order.ID)
	assert.Contains(t, data, "active")
}

func TestStopLossService_GetStopLoss_LazyLoadsFromRedis(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = rc.Close() }()

	svc1 := newTestStopLossService(rc)
	params := newTestStopLossParams()
	order, err := svc1.CreateStopLoss(t.Context(), params)
	require.NoError(t, err)

	svc2 := newTestStopLossService(rc)
	found, exists := svc2.GetStopLoss(order.ID)
	assert.True(t, exists)
	assert.Equal(t, order.PositionID, found.PositionID)
}

func TestStopLossService_RemoveStopLoss_DeletesFromRedis(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = rc.Close() }()

	svc := newTestStopLossService(rc)
	ctx := t.Context()
	params := newTestStopLossParams()
	order, err := svc.CreateStopLoss(ctx, params)
	require.NoError(t, err)

	err = svc.CancelStopLoss(order.ID)
	require.NoError(t, err)

	key := svc.keyForStopLoss(order.ID)
	_, err = rc.Get(ctx, key).Result()
	assert.ErrorIs(t, err, redis.Nil)
}

func TestStopLossService_ReconcileFromRedis_PopulatesCache(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = rc.Close() }()

	svc1 := newTestStopLossService(rc)
	ctx := t.Context()
	params1 := newTestStopLossParams()
	params1.PositionID = "pos-rec-1"
	order1, err := svc1.CreateStopLoss(ctx, params1)
	require.NoError(t, err)

	params2 := newTestStopLossParams()
	params2.PositionID = "pos-rec-2"
	order2, err := svc1.CreateStopLoss(ctx, params2)
	require.NoError(t, err)

	svc2 := newTestStopLossService(rc)
	err = svc2.ReconcileFromRedis(ctx)
	require.NoError(t, err)

	f1, exists := svc2.GetStopLoss(order1.ID)
	assert.True(t, exists)
	assert.Equal(t, order1.PositionID, f1.PositionID)

	f2, exists := svc2.GetStopLoss(order2.ID)
	assert.True(t, exists)
	assert.Equal(t, order2.PositionID, f2.PositionID)
}

func TestStopLossService_DegradedMode_NoRedis(t *testing.T) {
	svc := newTestStopLossService(nil)
	ctx := t.Context()
	params := newTestStopLossParams()
	order, err := svc.CreateStopLoss(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, order)

	_, exists := svc.GetStopLoss(order.ID)
	assert.True(t, exists)

	err = svc.CancelStopLoss(order.ID)
	require.NoError(t, err)

	err = svc.ReconcileFromRedis(ctx)
	assert.NoError(t, err)
}

func TestStopLossService_ExecuteStopLoss_UpdatesRedis(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = rc.Close() }()

	svc := newTestStopLossService(rc)
	ctx := t.Context()
	params := newTestStopLossParams()
	order, err := svc.CreateStopLoss(ctx, params)
	require.NoError(t, err)

	result, err := svc.ExecuteStopLoss(ctx, order.ID, decimal.NewFromInt(49000))
	require.NoError(t, err)
	assert.True(t, result.Success)

	key := svc.keyForStopLoss(order.ID)
	data, err := rc.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Contains(t, data, string(StopLossStatusExecuted))
}

func TestStopLossService_RedisRestart_SystemDegradesGracefully(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	svc := newTestStopLossService(rc)
	ctx := t.Context()
	params := newTestStopLossParams()
	_, err := svc.CreateStopLoss(ctx, params)
	require.NoError(t, err)

	_ = rc.Close()
	s.Close()
	s2 := miniredis.RunT(t)
	defer s2.Close()
	rc2 := redis.NewClient(&redis.Options{Addr: s2.Addr()})
	defer func() { _ = rc2.Close() }()

	svc2 := newTestStopLossService(rc2)
	err = svc2.ReconcileFromRedis(ctx)
	assert.NoError(t, err)
}

func TestStopLossService_TPSLExchangePreference_DoesNotDoubleTrack(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = rc.Close() }()

	svc := newTestStopLossService(rc)
	ctx := t.Context()
	params := newTestStopLossParams()
	order, err := svc.CreateStopLoss(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, order)

	allOrders := make([]*StopLossOrder, 0)
	svc.ordersMu.RLock()
	for _, o := range svc.orders {
		if o.PositionID == params.PositionID {
			allOrders = append(allOrders, o)
		}
	}
	svc.ordersMu.RUnlock()
	assert.Len(t, allOrders, 1, "Should not double-track stop-loss for same position")
}
