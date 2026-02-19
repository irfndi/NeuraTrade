package services

import (
	"context"
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
	err             error
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
	if m.err != nil {
		return nil, m.err
	}
	return m.balanceResponse, nil
}

func TestDefaultPortfolioSafetyConfig(t *testing.T) {
	config := DefaultPortfolioSafetyConfig()
	assert.Equal(t, 0.10, config.MaxPositionSizePct)
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
	assert.Len(t, snapshot.ExchangeExposures, 1)
	assert.Equal(t, "binance", snapshot.ExchangeExposures[0].Exchange)
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
		AvailableFunds: decimal.NewFromFloat(2000.0),
		ExposurePct:    0.80,
		OpenPositions:  5,
		CalculatedAt:   time.Now(),
	}

	status, err := service.CheckSafety(ctx, "test-chat", snapshot)

	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.True(t, status.IsSafe)
	assert.NotEmpty(t, status.Warnings)
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

	allowed, reason, err := service.CanExecuteTrade(ctx, "test-chat", "binance", "BTC/USDT", decimal.NewFromFloat(500))
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)

	allowed, reason, err = service.CanExecuteTrade(ctx, "test-chat", "binance", "BTC/USDT", decimal.NewFromFloat(50000))
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
		MaxExposurePct:       0.30,
		DefaultQuoteCurrency: "BTC",
		CacheTTL:             1 * time.Minute,
	}

	service.SetConfig(newConfig)
	assert.Equal(t, newConfig, service.GetConfig())
}
