package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCCXTForPortfolioSafety struct {
	balanceResponse *ccxt.BalanceResponse
	balanceByExch   map[string]*ccxt.BalanceResponse
	positionsByExch map[string]*ccxt.PositionsResponse
	err             error
	fetchCalls      int
}

func (m *mockCCXTForPortfolioSafety) Initialize(ctx context.Context) error { return nil }
func (m *mockCCXTForPortfolioSafety) IsHealthy(ctx context.Context) bool   { return true }
func (m *mockCCXTForPortfolioSafety) Close() error                         { return nil }
func (m *mockCCXTForPortfolioSafety) GetServiceURL() string                { return "http://localhost" }
func (m *mockCCXTForPortfolioSafety) GetSupportedExchanges() []string      { return []string{"binance"} }
func (m *mockCCXTForPortfolioSafety) GetExchangeInfo(exchangeID string) (ccxt.ExchangeInfo, bool) {
	return ccxt.ExchangeInfo{}, false
}
func (m *mockCCXTForPortfolioSafety) GetExchangeConfig(ctx context.Context) (*ccxt.ExchangeConfigResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) AddExchangeToBlacklist(ctx context.Context, ex string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) RemoveExchangeFromBlacklist(ctx context.Context, ex string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) RefreshExchanges(ctx context.Context) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) AddExchange(ctx context.Context, ex string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchMarketData(ctx context.Context, ex []string, syms []string) ([]ccxt.MarketPriceInterface, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchSingleTicker(ctx context.Context, ex, sym string) (ccxt.MarketPriceInterface, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchOrderBook(ctx context.Context, ex, sym string, limit int) (*ccxt.OrderBookResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) CalculateOrderBookMetrics(ctx context.Context, ex, sym string, limit int) (*ccxt.OrderBookMetrics, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchOHLCV(ctx context.Context, ex, sym, timeframe string, limit int) (*ccxt.OHLCVResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchTrades(ctx context.Context, ex, sym string, limit int) (*ccxt.TradesResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchMarkets(ctx context.Context, ex string) (*ccxt.MarketsResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchFundingRate(ctx context.Context, ex, sym string) (*ccxt.FundingRate, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchFundingRates(ctx context.Context, ex string, syms []string) ([]ccxt.FundingRate, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchAllFundingRates(ctx context.Context, ex string) ([]ccxt.FundingRate, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) CalculateArbitrageOpportunities(ctx context.Context, ex []string, syms []string, min decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) CalculateFundingRateArbitrage(ctx context.Context, syms []string, ex []string, min float64) ([]ccxt.FundingArbitrageOpportunity, error) {
	return nil, nil
}
func (m *mockCCXTForPortfolioSafety) FetchBalance(ctx context.Context, exchange string) (*ccxt.BalanceResponse, error) {
	m.fetchCalls++
	if m.err != nil {
		return nil, m.err
	}
	if m.balanceByExch != nil {
		if response, ok := m.balanceByExch[exchange]; ok {
			return response, nil
		}
	}
	return m.balanceResponse, nil
}

func (m *mockCCXTForPortfolioSafety) CancelOrder(ctx context.Context, exchange, orderID, symbol string) error {
	return nil
}

func (m *mockCCXTForPortfolioSafety) FetchOrder(ctx context.Context, exchange, orderID, symbol string) (*ccxt.OrderResponse, error) {
	return nil, nil
}

func (m *mockCCXTForPortfolioSafety) FetchOpenOrders(ctx context.Context, exchange string) (*ccxt.OpenOrdersResponse, error) {
	return nil, nil
}

func (m *mockCCXTForPortfolioSafety) FetchOpenOrdersForSymbol(ctx context.Context, exchange, symbol string) (*ccxt.OpenOrdersResponse, error) {
	return nil, nil
}

func (m *mockCCXTForPortfolioSafety) FetchPositions(ctx context.Context, exchange string) (*ccxt.PositionsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.positionsByExch != nil {
		if response, ok := m.positionsByExch[exchange]; ok {
			return response, nil
		}
	}
	return nil, nil
}

func TestDefaultPortfolioSafetyConfig(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	assert.Equal(t, 0.10, config.MaxPositionSizePct)
	assert.Equal(t, 0.20, config.MaxPositionFloorPct)
	assert.Equal(t, 0.50, config.MaxExposurePct)
	assert.Equal(t, "USDT", config.DefaultQuoteCurrency)
	assert.Equal(t, 30*time.Second, config.CacheTTL)
}

func TestNewPortfolioSafetyService(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	assert.NotNil(t, service)
	assert.Equal(t, config, service.GetConfig())
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_NoExchanges(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()
	snapshot, err := service.GetPortfolioSnapshot(ctx, "test-chat", []string{})

	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.True(t, snapshot.TotalEquity.IsZero())
	assert.True(t, snapshot.AvailableFunds.IsZero())
	assert.Equal(t, 0, snapshot.OpenPositions)
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_WithBalance(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceResponse: &ccxt.BalanceResponse{
			Exchange:  "binance",
			Timestamp: time.Now(),
			Total:     map[string]float64{"USDT": 10000.0},
			Free:      map[string]float64{"USDT": 8000.0},
			Used:      map[string]float64{"USDT": 2000.0},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()
	snapshot, err := service.GetPortfolioSnapshot(ctx, "test-chat", []string{"binance"})

	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.True(t, snapshot.TotalEquity.Equal(decimal.NewFromFloat(10000.0)))
	assert.True(t, snapshot.AvailableFunds.Equal(decimal.NewFromFloat(8000.0)))
	assert.True(t, snapshot.TotalExposure.Equal(decimal.NewFromFloat(2000.0)))
	assert.InDelta(t, 0.2, snapshot.ExposurePct, 0.00001)
	assert.Len(t, snapshot.ExchangeExposures, 1)
	assert.Equal(t, "binance", snapshot.ExchangeExposures[0].Exchange)
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_WithLowercaseQuoteCurrency(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceResponse: &ccxt.BalanceResponse{
			Exchange:  "bitget",
			Timestamp: time.Now(),
			Total:     map[string]float64{"usdt": 1000.0},
			Free:      map[string]float64{"UsDt": 700.0},
			Used:      map[string]float64{"USDT": 300.0},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	snapshot, err := service.GetPortfolioSnapshot(context.Background(), "chat-case", []string{"bitget"})
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.True(t, snapshot.TotalEquity.Equal(decimal.NewFromFloat(1000.0)))
	assert.True(t, snapshot.AvailableFunds.Equal(decimal.NewFromFloat(700.0)))
	assert.True(t, snapshot.TotalExposure.Equal(decimal.NewFromFloat(300.0)))
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_AllExchangeBalancesFailWithoutCache_ReturnsError(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{err: errors.New("Too Many Requests")}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	_, err := service.GetPortfolioSnapshot(context.Background(), "chat-rate-limit", []string{"bitget"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch balance from all requested exchanges")
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_UsesRecentStaleCacheOnRefreshFailure(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.CacheTTL = 10 * time.Millisecond
	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceResponse: &ccxt.BalanceResponse{
			Exchange:  "bitget",
			Timestamp: time.Now(),
			Total:     map[string]float64{"USDT": 46.93},
			Free:      map[string]float64{"USDT": 46.93},
			Used:      map[string]float64{"USDT": 0},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()
	snapshot, err := service.GetPortfolioSnapshot(ctx, "chat-stale", []string{"bitget"})
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.True(t, snapshot.TotalEquity.Equal(decimal.NewFromFloat(46.93)))

	time.Sleep(20 * time.Millisecond)
	mockCCXT.err = errors.New("Too Many Requests")

	fallbackSnapshot, err := service.GetPortfolioSnapshot(ctx, "chat-stale", []string{"bitget"})
	require.NoError(t, err)
	require.NotNil(t, fallbackSnapshot)
	assert.True(t, fallbackSnapshot.TotalEquity.Equal(decimal.NewFromFloat(46.93)))
	assert.True(t, fallbackSnapshot.AvailableFunds.Equal(decimal.NewFromFloat(46.93)))
}

func TestPortfolioSafetyService_CheckSafety_Allowed(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()
	snapshot := &SafetyPortfolioSnapshot{
		TotalEquity:    decimal.NewFromFloat(10000.0),
		AvailableFunds: decimal.NewFromFloat(8000.0),
		ExposurePct:    0.20,
		OpenPositions:  1,
		CalculatedAt:   time.Now(),
	}

	status, err := service.CheckSafety(ctx, "test-chat", snapshot)

	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.True(t, status.IsSafe)
	assert.True(t, status.TradingAllowed)
	assert.True(t, status.MaxPositionSize.GreaterThan(decimal.Zero))
}

func TestPortfolioSafetyService_CheckSafety_ExposureWarning(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.MaxExposurePct = 0.30
	mockCCXT := &mockCCXTForPortfolioSafety{}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()
	snapshot := &SafetyPortfolioSnapshot{
		TotalEquity:    decimal.NewFromFloat(10000.0),
		AvailableFunds: decimal.NewFromFloat(5500.0),
		ExposurePct:    0.45,
		OpenPositions:  5,
		CalculatedAt:   time.Now(),
	}

	status, err := service.CheckSafety(ctx, "test-chat", snapshot)

	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.True(t, status.IsSafe)
	assert.True(t, status.TradingAllowed)
	assert.NotEmpty(t, status.Warnings)
}

func TestPortfolioSafetyService_CheckSafety_ExposureHardLimitBlocksTrading(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.MaxExposurePct = 0.30
	mockCCXT := &mockCCXTForPortfolioSafety{}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()
	snapshot := &SafetyPortfolioSnapshot{
		TotalEquity:    decimal.NewFromFloat(10000.0),
		AvailableFunds: decimal.NewFromFloat(1000.0),
		ExposurePct:    0.80,
		OpenPositions:  7,
		CalculatedAt:   time.Now(),
	}

	status, err := service.CheckSafety(ctx, "test-chat", snapshot)

	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.False(t, status.IsSafe)
	assert.False(t, status.TradingAllowed)
	assert.NotEmpty(t, status.Warnings)
	assert.NotEmpty(t, status.Reasons)
	assert.Contains(t, strings.Join(status.Reasons, " | "), "Exposure hard limit")
}

func TestPortfolioSafetyService_CanExecuteTrade(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceResponse: &ccxt.BalanceResponse{
			Exchange:  "binance",
			Timestamp: time.Now(),
			Total:     map[string]float64{"USDT": 10000.0},
			Free:      map[string]float64{"USDT": 8000.0},
			Used:      map[string]float64{"USDT": 2000.0},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()

	allowed, reason, err := service.CanExecuteTrade(ctx, "test-chat", "binance", "BTC/USDT", "spot", decimal.NewFromFloat(500))
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)

	allowed, reason, err = service.CanExecuteTrade(ctx, "test-chat", "binance", "BTC/USDT", "spot", decimal.NewFromFloat(50000))
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.NotEmpty(t, reason)
}

func TestPortfolioSafetyService_InvalidateCache(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.CacheTTL = 1 * time.Hour
	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceResponse: &ccxt.BalanceResponse{
			Exchange:  "binance",
			Timestamp: time.Now(),
			Total:     map[string]float64{"USDT": 10000.0},
			Free:      map[string]float64{"USDT": 8000.0},
			Used:      map[string]float64{"USDT": 2000.0},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()

	snapshot1, err := service.GetPortfolioSnapshot(ctx, "test-chat", []string{"binance"})
	require.NoError(t, err)
	assert.NotNil(t, snapshot1)

	service.InvalidateCache()

	mockCCXT.balanceResponse.Total["USDT"] = 20000.0
	snapshot2, err := service.GetPortfolioSnapshot(ctx, "test-chat", []string{"binance"})
	require.NoError(t, err)
	assert.NotNil(t, snapshot2)

	assert.True(t, snapshot2.TotalEquity.GreaterThan(snapshot1.TotalEquity))
}

func TestPortfolioSafetyService_SetConfig(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	newConfig := PortfolioSafetyConfig{
		MaxPositionSizePct:   0.05,
		MaxPositionFloorPct:  0.15,
		MaxExposurePct:       0.30,
		DefaultQuoteCurrency: "BTC",
		CacheTTL:             1 * time.Minute,
	}

	service.SetConfig(newConfig)
	assert.Equal(t, newConfig, service.GetConfig())
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_CacheKeyIncludesChatAndExchanges(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.CacheTTL = time.Hour

	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceByExch: map[string]*ccxt.BalanceResponse{
			"binance": {
				Exchange:  "binance",
				Timestamp: time.Now(),
				Total:     map[string]float64{"USDT": 10000.0},
				Free:      map[string]float64{"USDT": 8000.0},
				Used:      map[string]float64{"USDT": 2000.0},
			},
			"bybit": {
				Exchange:  "bybit",
				Timestamp: time.Now(),
				Total:     map[string]float64{"USDT": 5000.0},
				Free:      map[string]float64{"USDT": 4000.0},
				Used:      map[string]float64{"USDT": 1000.0},
			},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()

	snapshotA, err := service.GetPortfolioSnapshot(ctx, "chat-a", []string{"binance"})
	require.NoError(t, err)
	assert.True(t, snapshotA.TotalEquity.Equal(decimal.NewFromFloat(10000.0)))
	assert.Len(t, snapshotA.ExchangeExposures, 1)
	assert.Equal(t, "binance", snapshotA.ExchangeExposures[0].Exchange)

	snapshotB, err := service.GetPortfolioSnapshot(ctx, "chat-b", []string{"bybit"})
	require.NoError(t, err)
	assert.True(t, snapshotB.TotalEquity.Equal(decimal.NewFromFloat(5000.0)))
	assert.Len(t, snapshotB.ExchangeExposures, 1)
	assert.Equal(t, "bybit", snapshotB.ExchangeExposures[0].Exchange)
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_CacheKeyNormalizesExchangeOrder(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.CacheTTL = time.Hour

	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceByExch: map[string]*ccxt.BalanceResponse{
			"binance": {
				Exchange:  "binance",
				Timestamp: time.Now(),
				Total:     map[string]float64{"USDT": 10000.0},
				Free:      map[string]float64{"USDT": 9000.0},
				Used:      map[string]float64{"USDT": 1000.0},
			},
			"bybit": {
				Exchange:  "bybit",
				Timestamp: time.Now(),
				Total:     map[string]float64{"USDT": 3000.0},
				Free:      map[string]float64{"USDT": 2000.0},
				Used:      map[string]float64{"USDT": 1000.0},
			},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	ctx := context.Background()

	snapshot1, err := service.GetPortfolioSnapshot(ctx, "chat-a", []string{"binance", "bybit"})
	require.NoError(t, err)
	assert.True(t, snapshot1.TotalEquity.Equal(decimal.NewFromFloat(13000.0)))
	assert.Equal(t, 2, mockCCXT.fetchCalls)

	snapshot2, err := service.GetPortfolioSnapshot(ctx, "chat-a", []string{"bybit", "binance"})
	require.NoError(t, err)
	assert.True(t, snapshot2.TotalEquity.Equal(decimal.NewFromFloat(13000.0)))
	assert.Equal(t, 2, mockCCXT.fetchCalls)
}

func TestPortfolioSafetyService_GetPortfolioSnapshot_FallsBackToExchangePositions(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceByExch: map[string]*ccxt.BalanceResponse{
			"bitget": {
				Exchange:  "bitget",
				Timestamp: time.Now(),
				Total:     map[string]float64{"USDT": 1000.0},
				Free:      map[string]float64{"USDT": 900.0},
				Used:      map[string]float64{"USDT": 100.0},
			},
		},
		positionsByExch: map[string]*ccxt.PositionsResponse{
			"bitget": {
				Exchange: "bitget",
				Positions: []ccxt.Position{
					{
						ID:            "pos-1",
						Symbol:        "ADA/USDT",
						Side:          "long",
						Size:          decimal.NewFromInt(10),
						EntryPrice:    decimal.NewFromFloat(0.30),
						MarkPrice:     decimal.NewFromFloat(0.31),
						UnrealizedPnl: decimal.NewFromFloat(0.10),
					},
				},
			},
		},
	}

	service := NewPortfolioSafetyService(
		config,
		mockCCXT,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	snapshot, err := service.GetPortfolioSnapshot(context.Background(), "chat-1", []string{"bitget"})
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Len(t, snapshot.Positions, 1)
	assert.Equal(t, 1, snapshot.OpenPositions)
	assert.True(t, snapshot.TotalExposure.Equal(decimal.NewFromFloat(3.1)))
	assert.True(t, snapshot.UnrealizedPnL.Equal(decimal.NewFromFloat(0.1)))
	assert.Equal(t, "ADA/USDT", snapshot.Positions[0].Symbol)
	assert.Equal(t, "long", snapshot.Positions[0].Side)
}

func TestPortfolioSafetyService_CanExecuteTrade_BitgetFuturesMinNotionalScenarios(t *testing.T) {
	tests := []struct {
		name                string
		symbol              string
		marketType          string
		balanceTotal        float64
		maxPositionSizePct  float64
		maxPositionFloorPct float64
		requestedNotional   decimal.Decimal
		wantAllowed         bool
		wantReasonSubstring []string
	}{
		{
			name:                "allows_min_notional_within_floor_cap",
			symbol:              "OPN/USDT:USDT",
			marketType:          "futures",
			balanceTotal:        46.93,
			maxPositionSizePct:  0.10,
			maxPositionFloorPct: 0.20,
			requestedNotional:   decimal.NewFromFloat(6.0),
			wantAllowed:         true,
		},
		{
			name:                "blocks_min_notional_beyond_floor_cap",
			symbol:              "OPN/USDT:USDT",
			marketType:          "futures",
			balanceTotal:        20.00,
			maxPositionSizePct:  0.10,
			maxPositionFloorPct: 0.20,
			requestedNotional:   decimal.NewFromFloat(6.0),
			wantAllowed:         false,
			wantReasonSubstring: []string{"exceeds maximum allowed"},
		},
		{
			name:                "blocks_below_exchange_min_notional",
			symbol:              "OPN/USDT:USDT",
			marketType:          "futures",
			balanceTotal:        100.00,
			maxPositionSizePct:  0.10,
			maxPositionFloorPct: 0.20,
			requestedNotional:   decimal.NewFromFloat(5.0),
			wantAllowed:         false,
			wantReasonSubstring: []string{"below exchange minimum notional 6.00"},
		},
		{
			name:                "reports_effective_throttle_after_floor_override",
			symbol:              "OPN/USDT:USDT",
			marketType:          "futures",
			balanceTotal:        46.93,
			maxPositionSizePct:  0.10,
			maxPositionFloorPct: 0.20,
			requestedNotional:   decimal.NewFromFloat(7.0),
			wantAllowed:         false,
			wantReasonSubstring: []string{"maximum allowed 6.00", "set to 128%"},
		},
		{
			name:                "zero_floor_disables_notional_override",
			symbol:              "OPN/USDT:USDT",
			marketType:          "futures",
			balanceTotal:        46.93,
			maxPositionSizePct:  0.10,
			maxPositionFloorPct: 0,
			requestedNotional:   decimal.NewFromFloat(6.0),
			wantAllowed:         false,
			wantReasonSubstring: []string{"maximum allowed 4.69", "throttled to 100%"},
		},
		{
			name:                "spot_symbol_skips_futures_notional_floor",
			symbol:              "OPN/USDT",
			marketType:          "spot",
			balanceTotal:        100.00,
			maxPositionSizePct:  0.10,
			maxPositionFloorPct: 0.20,
			requestedNotional:   decimal.NewFromFloat(5.0),
			wantAllowed:         true,
		},
		{
			name:                "futures_market_type_applies_floor_for_bare_symbol",
			symbol:              "PEPE/USDT",
			marketType:          "futures",
			balanceTotal:        46.93,
			maxPositionSizePct:  0.10,
			maxPositionFloorPct: 0.20,
			requestedNotional:   decimal.NewFromFloat(6.0),
			wantAllowed:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultPortfolioSafetyConfig()
			config.MaxPositionSizePct = tt.maxPositionSizePct
			config.MaxPositionFloorPct = tt.maxPositionFloorPct

			mockCCXT := &mockCCXTForPortfolioSafety{
				balanceResponse: &ccxt.BalanceResponse{
					Exchange:  "bitget",
					Timestamp: time.Now(),
					Total:     map[string]float64{"USDT": tt.balanceTotal},
					Free:      map[string]float64{"USDT": tt.balanceTotal},
					Used:      map[string]float64{"USDT": 0},
				},
			}

			service := NewPortfolioSafetyService(config, mockCCXT, nil, nil, nil, nil, nil, nil, nil)
			allowed, reason, err := service.CanExecuteTrade(context.Background(), "chat-bitget", "bitget", tt.symbol, tt.marketType, tt.requestedNotional)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, allowed)
			for _, fragment := range tt.wantReasonSubstring {
				assert.Contains(t, reason, fragment)
			}
			if tt.wantAllowed {
				assert.Empty(t, reason)
			}
		})
	}
}

func TestPortfolioSafetyService_CanExecuteTradeWithLeverage_BitgetFuturesAllowsMinNotionalWhenMarginSupportsIt(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.MaxPositionSizePct = 0.10
	config.MaxPositionFloorPct = 0.20

	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceResponse: &ccxt.BalanceResponse{
			Exchange:  "bitget",
			Timestamp: time.Now(),
			Total:     map[string]float64{"USDT": 46.93},
			Free:      map[string]float64{"USDT": 0.61},
			Used:      map[string]float64{"USDT": 46.32},
		},
	}

	service := NewPortfolioSafetyService(config, mockCCXT, nil, nil, nil, nil, nil, nil, nil)
	allowed, reason, err := service.CanExecuteTradeWithLeverage(
		context.Background(),
		"chat-bitget",
		"bitget",
		"OPN/USDT:USDT",
		"futures",
		10,
		decimal.NewFromFloat(6.0),
	)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)
	assert.Equal(t, 1, mockCCXT.fetchCalls)
}

func TestPortfolioSafetyService_CanExecuteTrade_BitgetFuturesAllowsMinNotionalWhenBalanceSnapshotIsZero(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()

	mockCCXT := &mockCCXTForPortfolioSafety{
		balanceResponse: &ccxt.BalanceResponse{
			Exchange:  "bitget",
			Timestamp: time.Now(),
			Total:     map[string]float64{"USDT": 0},
			Free:      map[string]float64{"USDT": 0},
			Used:      map[string]float64{"USDT": 0},
		},
	}

	service := NewPortfolioSafetyService(config, mockCCXT, nil, nil, nil, nil, nil, nil, nil)
	allowed, reason, err := service.CanExecuteTrade(
		context.Background(),
		"chat-bitget",
		"bitget",
		"OPN/USDT:USDT",
		"futures",
		decimal.NewFromFloat(6.0),
	)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)
}

func TestPortfolioSafetyService_ResolveEffectiveMaxPositionSize_UsesScopedEquityForFloorCap(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	config.MaxPositionFloorPct = 0.20

	service := NewPortfolioSafetyService(config, nil, nil, nil, nil, nil, nil, nil, nil)

	effective := service.resolveEffectiveMaxPositionSize(
		"bitget",
		"OPN/USDT:USDT",
		"futures",
		decimal.NewFromFloat(3.0),
		decimal.NewFromFloat(3.0),
	)

	assert.True(t, effective.Equal(decimal.NewFromFloat(3.0)))
}

func TestPortfolioSafetyService_ResolveScopedMarketFunds_UsesCachedSnapshotBalance(t *testing.T) {
	service := NewPortfolioSafetyService(DefaultPortfolioSafetyConfig(), nil, nil, nil, nil, nil, nil, nil, nil)
	snapshot := &SafetyPortfolioSnapshot{
		balancesByExchange: map[string]*ccxt.BalanceResponse{
			"bitget": {
				Exchange: "bitget",
				Total: map[string]float64{
					"USDT":              100,
					"USDT_FUTURES_USDT": 3,
				},
				Free: map[string]float64{
					"USDT":              80,
					"USDT_FUTURES_USDT": 2,
				},
			},
		},
	}

	total, free, ok := service.resolveScopedMarketFunds(snapshot, "bitget", "futures")

	assert.True(t, ok)
	assert.True(t, total.Equal(decimal.NewFromFloat(3)))
	assert.True(t, free.Equal(decimal.NewFromFloat(2)))
}

func TestFuturesSizeWithinRoundedEffectiveMax(t *testing.T) {
	assert.True(t, futuresSizeWithinRoundedEffectiveMax(decimal.NewFromFloat(6.00), decimal.NewFromFloat(5.996)))
	assert.True(t, futuresSizeWithinRoundedEffectiveMax(decimal.NewFromFloat(6.00), decimal.NewFromFloat(6.00)))
	assert.False(t, futuresSizeWithinRoundedEffectiveMax(decimal.NewFromFloat(6.00), decimal.NewFromFloat(5.98)))
	assert.False(t, futuresSizeWithinRoundedEffectiveMax(decimal.Zero, decimal.NewFromFloat(6.00)))
	assert.False(t, futuresSizeWithinRoundedEffectiveMax(decimal.NewFromFloat(6.00), decimal.Zero))
}

func TestPortfolioSafetyService_SetConfig_NormalizesDefaultsButPreservesZeroFloor(t *testing.T) {
	service := NewPortfolioSafetyService(DefaultPortfolioSafetyConfig(), nil, nil, nil, nil, nil, nil, nil, nil)

	service.SetConfig(PortfolioSafetyConfig{
		MaxPositionSizePct:   0,
		MaxPositionFloorPct:  0,
		MaxExposurePct:       0,
		DefaultQuoteCurrency: "",
		CacheTTL:             0,
	})

	cfg := service.GetConfig()
	defaults := DefaultPortfolioSafetyConfig()
	assert.Equal(t, defaults.MaxPositionSizePct, cfg.MaxPositionSizePct)
	assert.Equal(t, defaults.MaxExposurePct, cfg.MaxExposurePct)
	assert.Equal(t, defaults.DefaultQuoteCurrency, cfg.DefaultQuoteCurrency)
	assert.Equal(t, defaults.CacheTTL, cfg.CacheTTL)
	assert.Equal(t, 0.0, cfg.MaxPositionFloorPct)
}
