package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/logging"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockCCXTClient is a mock implementation of CCXTClient
type MockCCXTClient struct {
	mock.Mock
}

func (m *MockCCXTClient) GetFundingRates(ctx context.Context, exchange string, symbols []string) ([]ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange, symbols)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTClient) HealthCheck(ctx context.Context) (*ccxt.HealthResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.HealthResponse), args.Error(1)
}

func (m *MockCCXTClient) GetExchanges(ctx context.Context) (*ccxt.ExchangesResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.ExchangesResponse), args.Error(1)
}

func (m *MockCCXTClient) GetExchangeConfig(ctx context.Context) (*ccxt.ExchangeConfigResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.ExchangeConfigResponse), args.Error(1)
}

func (m *MockCCXTClient) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTClient) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTClient) RefreshExchanges(ctx context.Context) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTClient) AddExchange(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTClient) GetTicker(ctx context.Context, exchange, symbol string) (*ccxt.TickerResponse, error) {
	args := m.Called(ctx, exchange, symbol)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.TickerResponse), args.Error(1)
}

func (m *MockCCXTClient) GetTickers(ctx context.Context, req *ccxt.TickersRequest) (*ccxt.TickersResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.TickersResponse), args.Error(1)
}

func (m *MockCCXTClient) GetOrderBook(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookResponse, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.OrderBookResponse), args.Error(1)
}

func (m *MockCCXTClient) GetTrades(ctx context.Context, exchange, symbol string, limit int) (*ccxt.TradesResponse, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.TradesResponse), args.Error(1)
}

func (m *MockCCXTClient) GetOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*ccxt.OHLCVResponse, error) {
	args := m.Called(ctx, exchange, symbol, timeframe, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.OHLCVResponse), args.Error(1)
}

func (m *MockCCXTClient) GetMarkets(ctx context.Context, exchange string) (*ccxt.MarketsResponse, error) {
	args := m.Called(ctx, exchange)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.MarketsResponse), args.Error(1)
}

func (m *MockCCXTClient) GetFundingRate(ctx context.Context, exchange, symbol string) (*ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange, symbol)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTClient) GetAllFundingRates(ctx context.Context, exchange string) ([]ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTClient) GetFundingRateHistory(ctx context.Context, exchange, symbol string, since time.Time) ([]ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange, symbol, since)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCCXTClient) BaseURL() string {
	args := m.Called()
	return args.String(0)
}

func TestFundingRateCollector_Integration_Collect(t *testing.T) {
	// Setup Mock DB
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()
	dbPool := database.NewMockDBPool(mockDB)

	// Setup Mock CCXT
	mockCCXT := &MockCCXTClient{}

	// Setup Config & Logger
	cfg := &config.Config{}
	logger := logging.NewStandardLogger("debug", "testing")

	// Create Collector with default config (binance, bybit)
	collector := NewFundingRateCollector(dbPool, nil, mockCCXT, cfg, nil, logger)

	// Define Test Data
	now := time.Now().Truncate(time.Second) // Truncate for DB comparison matching
	testRates := []ccxt.FundingRate{
		{
			Symbol:           "BTC/USDT",
			FundingRate:      0.0001,
			FundingTimestamp: ccxt.UnixTimestamp(now),
			MarkPrice:        50000.0,
			IndexPrice:       50000.0,
		},
	}

	// Expectations
	// 1. CCXT Call - Expect calls for both default exchanges
	mockCCXT.On("GetFundingRates", mock.Anything, "binance", ([]string)(nil)).Return(testRates, nil)
	mockCCXT.On("GetFundingRates", mock.Anything, "bybit", ([]string)(nil)).Return([]ccxt.FundingRate{}, nil)

	// 2. DB Insert (for binance results)
	// We match roughly on the query string and specific arguments
	mockDB.ExpectExec("INSERT INTO funding_rate_history").
		WithArgs("BTC/USDT", "binance", 0.0001, now, 50000.0, 50000.0, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err = collector.collectFundingRates(context.Background())
	assert.NoError(t, err)

	// Verify
	mockCCXT.AssertExpectations(t)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestFundingRateCollector_Integration_GetStats_MockDB(t *testing.T) {
	// Setup Mock DB
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()
	dbPool := database.NewMockDBPool(mockDB)

	// Setup Config & Logger
	cfg := &config.Config{}
	logger := logging.NewStandardLogger("debug", "testing")

	// We don't need CCXT for this test
	mockCCXT := &MockCCXTClient{}

	collector := NewFundingRateCollector(dbPool, nil, mockCCXT, cfg, nil, logger)

	// Data for GetFundingRateStats
	// 1. getCurrentFundingRate
	mockDB.ExpectQuery("SELECT funding_rate FROM funding_rate_history").
		WithArgs("BTC/USDT", "binance").
		WillReturnRows(pgxmock.NewRows([]string{"funding_rate"}).AddRow(decimal.NewFromFloat(0.0001)))

	// 2. getHistoricalRates (7 days)
	mockDB.ExpectQuery("SELECT funding_rate FROM funding_rate_history").
		WithArgs("BTC/USDT", "binance", "7 days").
		WillReturnRows(pgxmock.NewRows([]string{"funding_rate"}).
			AddRow(decimal.NewFromFloat(0.0001)).
			AddRow(decimal.NewFromFloat(0.0002)))

	// 3. getHistoricalRates (30 days)
	mockDB.ExpectQuery("SELECT funding_rate FROM funding_rate_history").
		WithArgs("BTC/USDT", "binance", "30 days").
		WillReturnRows(pgxmock.NewRows([]string{"funding_rate"}).
			AddRow(decimal.NewFromFloat(0.0001)).
			AddRow(decimal.NewFromFloat(0.0002)).
			AddRow(decimal.NewFromFloat(0.0003)))

	// Execute
	stats, err := collector.GetFundingRateStats(context.Background(), "BTC/USDT", "binance")
	assert.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Equal(t, "BTC/USDT", stats.Symbol)
	assert.Equal(t, "binance", stats.Exchange)

	assert.NoError(t, mockDB.ExpectationsWereMet())
}
