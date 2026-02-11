package ccxt

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/cache"
	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/shopspring/decimal"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
)

// MockClient implements the Client interface for testing
type MockClient struct {
	HealthCheckFunc                 func(ctx context.Context) error
	GetExchangesFunc                func(ctx context.Context) (*ExchangesResponse, error)
	GetTickerFunc                   func(ctx context.Context, exchange, symbol string) (*TickerResponse, error)
	GetTickersFunc                  func(ctx context.Context, req *TickersRequest) (*TickersResponse, error)
	GetOrderBookFunc                func(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error)
	GetOHLCVFunc                    func(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error)
	GetTradesFunc                   func(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error)
	GetMarketsFunc                  func(ctx context.Context, exchange string) (*MarketsResponse, error)
	GetFundingRateFunc              func(ctx context.Context, exchange, symbol string) (*FundingRate, error)
	GetFundingRatesFunc             func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error)
	GetAllFundingRatesFunc          func(ctx context.Context, exchange string) ([]FundingRate, error)
	GetExchangeConfigFunc           func(ctx context.Context) (*ExchangeConfigResponse, error)
	AddExchangeToBlacklistFunc      func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	RemoveExchangeFromBlacklistFunc func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	RefreshExchangesFunc            func(ctx context.Context) (*ExchangeManagementResponse, error)
	AddExchangeFunc                 func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	CloseFunc                       func() error
	BaseURLFunc                     func() string
}

func (m *MockClient) HealthCheck(ctx context.Context) (*HealthResponse, error) {
	if m.HealthCheckFunc != nil {
		err := m.HealthCheckFunc(ctx)
		if err != nil {
			return nil, err
		}
		return &HealthResponse{Status: "ok"}, nil
	}
	return &HealthResponse{Status: "ok"}, nil
}

func (m *MockClient) GetExchanges(ctx context.Context) (*ExchangesResponse, error) {
	if m.GetExchangesFunc != nil {
		return m.GetExchangesFunc(ctx)
	}
	return &ExchangesResponse{Exchanges: []ExchangeInfo{}}, nil
}

func (m *MockClient) GetTicker(ctx context.Context, exchange, symbol string) (*TickerResponse, error) {
	if m.GetTickerFunc != nil {
		return m.GetTickerFunc(ctx, exchange, symbol)
	}
	return &TickerResponse{}, nil
}

func (m *MockClient) GetTickers(ctx context.Context, req *TickersRequest) (*TickersResponse, error) {
	if m.GetTickersFunc != nil {
		return m.GetTickersFunc(ctx, req)
	}
	return &TickersResponse{}, nil
}

func (m *MockClient) GetOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error) {
	if m.GetOrderBookFunc != nil {
		return m.GetOrderBookFunc(ctx, exchange, symbol, limit)
	}
	return &OrderBookResponse{}, nil
}

func (m *MockClient) GetOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error) {
	if m.GetOHLCVFunc != nil {
		return m.GetOHLCVFunc(ctx, exchange, symbol, timeframe, limit)
	}
	return &OHLCVResponse{}, nil
}

func (m *MockClient) GetTrades(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error) {
	if m.GetTradesFunc != nil {
		return m.GetTradesFunc(ctx, exchange, symbol, limit)
	}
	return &TradesResponse{}, nil
}

func (m *MockClient) GetMarkets(ctx context.Context, exchange string) (*MarketsResponse, error) {
	if m.GetMarketsFunc != nil {
		return m.GetMarketsFunc(ctx, exchange)
	}
	return &MarketsResponse{}, nil
}

func (m *MockClient) GetFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error) {
	if m.GetFundingRateFunc != nil {
		return m.GetFundingRateFunc(ctx, exchange, symbol)
	}
	return &FundingRate{}, nil
}

func (m *MockClient) GetFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
	if m.GetFundingRatesFunc != nil {
		return m.GetFundingRatesFunc(ctx, exchange, symbols)
	}
	return []FundingRate{}, nil
}

func (m *MockClient) GetAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error) {
	if m.GetAllFundingRatesFunc != nil {
		return m.GetAllFundingRatesFunc(ctx, exchange)
	}
	return []FundingRate{}, nil
}

func (m *MockClient) GetExchangeConfig(ctx context.Context) (*ExchangeConfigResponse, error) {
	if m.GetExchangeConfigFunc != nil {
		return m.GetExchangeConfigFunc(ctx)
	}
	return &ExchangeConfigResponse{}, nil
}

func (m *MockClient) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	if m.AddExchangeToBlacklistFunc != nil {
		return m.AddExchangeToBlacklistFunc(ctx, exchange)
	}
	return &ExchangeManagementResponse{}, nil
}

func (m *MockClient) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	if m.RemoveExchangeFromBlacklistFunc != nil {
		return m.RemoveExchangeFromBlacklistFunc(ctx, exchange)
	}
	return &ExchangeManagementResponse{}, nil
}

func (m *MockClient) RefreshExchanges(ctx context.Context) (*ExchangeManagementResponse, error) {
	if m.RefreshExchangesFunc != nil {
		return m.RefreshExchangesFunc(ctx)
	}
	return &ExchangeManagementResponse{}, nil
}

func (m *MockClient) AddExchange(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	if m.AddExchangeFunc != nil {
		return m.AddExchangeFunc(ctx, exchange)
	}
	return &ExchangeManagementResponse{}, nil
}

func (m *MockClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockClient) BaseURL() string {
	if m.BaseURLFunc != nil {
		return m.BaseURLFunc()
	}
	return "http://localhost:3001"
}

// MockBlacklistCache implements the BlacklistCache interface for testing
type MockBlacklistCache struct {
	IsBlacklistedFunc         func(symbol string) (bool, string)
	AddFunc                   func(symbol, reason string, ttl time.Duration)
	RemoveFunc                func(symbol string)
	ClearFunc                 func()
	GetStatsFunc              func() cache.BlacklistCacheStats
	LogStatsFunc              func()
	CloseFunc                 func() error
	LoadFromDatabaseFunc      func(ctx context.Context) error
	GetBlacklistedSymbolsFunc func() ([]cache.BlacklistCacheEntry, error)
}

func (m *MockBlacklistCache) IsBlacklisted(symbol string) (bool, string) {
	if m.IsBlacklistedFunc != nil {
		return m.IsBlacklistedFunc(symbol)
	}
	return false, ""
}

func (m *MockBlacklistCache) Add(symbol, reason string, ttl time.Duration) {
	if m.AddFunc != nil {
		m.AddFunc(symbol, reason, ttl)
	}
}

func (m *MockBlacklistCache) Remove(symbol string) {
	if m.RemoveFunc != nil {
		m.RemoveFunc(symbol)
	}
}

func (m *MockBlacklistCache) Clear() {
	if m.ClearFunc != nil {
		m.ClearFunc()
	}
}

func (m *MockBlacklistCache) GetStats() cache.BlacklistCacheStats {
	if m.GetStatsFunc != nil {
		return m.GetStatsFunc()
	}
	return cache.BlacklistCacheStats{}
}

func (m *MockBlacklistCache) LogStats() {
	if m.LogStatsFunc != nil {
		m.LogStatsFunc()
	}
}

func (m *MockBlacklistCache) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockBlacklistCache) LoadFromDatabase(ctx context.Context) error {
	if m.LoadFromDatabaseFunc != nil {
		return m.LoadFromDatabaseFunc(ctx)
	}
	return nil
}

func (m *MockBlacklistCache) GetBlacklistedSymbols() ([]cache.BlacklistCacheEntry, error) {
	if m.GetBlacklistedSymbolsFunc != nil {
		return m.GetBlacklistedSymbolsFunc()
	}
	return []cache.BlacklistCacheEntry{}, nil
}

// Test Service struct initialization
func TestService_Struct(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Test initial state
	assert.NotNil(t, service.client)
	assert.NotNil(t, service.supportedExchanges)
	assert.Empty(t, service.supportedExchanges)
	assert.True(t, service.lastUpdate.IsZero())
}

// Test GetSupportedExchanges with empty exchanges
func TestService_GetSupportedExchanges_Empty(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)
	exchanges := service.GetSupportedExchanges()

	assert.Empty(t, exchanges)
	assert.NotNil(t, exchanges) // Should return empty slice, not nil
}

// Test GetSupportedExchanges with populated exchanges
func TestService_GetSupportedExchanges_Populated(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Manually populate supported exchanges for testing
	service.mu.Lock()
	service.supportedExchanges["binance"] = ExchangeInfo{ID: "binance", Name: "Binance"}
	service.supportedExchanges["coinbase"] = ExchangeInfo{ID: "coinbase", Name: "Coinbase"}
	service.mu.Unlock()

	exchanges := service.GetSupportedExchanges()

	assert.Len(t, exchanges, 2)
	assert.Contains(t, exchanges, "binance")
	assert.Contains(t, exchanges, "coinbase")
}

// Test Service Initialize function with successful response
func TestService_Initialize_Success(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := &MockBlacklistCache{
		LoadFromDatabaseFunc: func(ctx context.Context) error {
			return nil // Successful load
		},
	}

	client := &MockClient{
		GetExchangesFunc: func(ctx context.Context) (*ExchangesResponse, error) {
			return &ExchangesResponse{
				Exchanges: []ExchangeInfo{
					{ID: "binance", Name: "Binance"},
					{ID: "coinbase", Name: "Coinbase"},
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	err := service.Initialize(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 2, len(service.supportedExchanges))
	assert.Contains(t, service.supportedExchanges, "binance")
	assert.Contains(t, service.supportedExchanges, "coinbase")
	assert.False(t, service.lastUpdate.IsZero())
}

// Test Service Initialize function with client error
func TestService_Initialize_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := &MockBlacklistCache{}

	client := &MockClient{
		GetExchangesFunc: func(ctx context.Context) (*ExchangesResponse, error) {
			return nil, assert.AnError
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	err := service.Initialize(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch supported exchanges")
	assert.Empty(t, service.supportedExchanges)
}

// Test Service Initialize function with blacklist cache error
func TestService_Initialize_BlacklistCacheError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := &MockBlacklistCache{
		LoadFromDatabaseFunc: func(ctx context.Context) error {
			return assert.AnError
		},
	}

	client := &MockClient{
		GetExchangesFunc: func(ctx context.Context) (*ExchangesResponse, error) {
			return &ExchangesResponse{
				Exchanges: []ExchangeInfo{
					{ID: "binance", Name: "Binance"},
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	err := service.Initialize(context.Background())
	assert.NoError(t, err) // Should not fail due to blacklist cache error
	assert.Equal(t, 1, len(service.supportedExchanges))
	assert.Contains(t, service.supportedExchanges, "binance")
}

// Test Service IsHealthy function with healthy client
func TestService_IsHealthy_Healthy(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		HealthCheckFunc: func(ctx context.Context) error {
			return nil // Healthy
		},
	}

	service := NewService(&config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}, logger, blacklistCache)

	// Replace the client with our mock
	service.client = client

	healthy := service.IsHealthy(context.Background())
	assert.True(t, healthy)
}

// Test Service IsHealthy function with unhealthy client
func TestService_IsHealthy_Unhealthy(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		HealthCheckFunc: func(ctx context.Context) error {
			return assert.AnError // Unhealthy
		},
	}

	service := NewService(&config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}, logger, blacklistCache)

	// Replace the client with our mock
	service.client = client

	healthy := service.IsHealthy(context.Background())
	assert.False(t, healthy)
}

// Test Service FetchSingleTicker function with successful response
func TestService_FetchSingleTicker_Success(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetTickerFunc: func(ctx context.Context, exchange, symbol string) (*TickerResponse, error) {
			return &TickerResponse{
				Exchange: exchange,
				Ticker: Ticker{
					Symbol:    symbol,
					Last:      decimal.NewFromFloat(50000.0),
					Volume:    decimal.NewFromFloat(1000.0),
					Timestamp: UnixTimestamp(time.Now()),
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	marketPrice, err := service.FetchSingleTicker(context.Background(), "binance", "BTC/USDT")
	assert.NoError(t, err)
	mp, ok := marketPrice.(*models.MarketPrice)
	if !ok {
		t.Fatalf("expected *models.MarketPrice, got %T", marketPrice)
	}
	assert.Equal(t, "binance", mp.ExchangeName)
	assert.Equal(t, "BTC/USDT", mp.Symbol)
	assert.Equal(t, decimal.NewFromFloat(50000.0), mp.Price)
	assert.Equal(t, decimal.NewFromFloat(1000.0), mp.Volume)
}

// Test Service FetchSingleTicker function with client error
func TestService_FetchSingleTicker_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetTickerFunc: func(ctx context.Context, exchange, symbol string) (*TickerResponse, error) {
			return nil, assert.AnError
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	marketPrice, err := service.FetchSingleTicker(context.Background(), "binance", "BTC/USDT")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch ticker for binance:BTC/USDT")
	assert.Nil(t, marketPrice)
}

// Test Service FetchOrderBook function with successful response
func TestService_FetchOrderBook_Success(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetOrderBookFunc: func(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error) {
			return &OrderBookResponse{
				Exchange: exchange,
				Symbol:   symbol,
				OrderBook: OrderBook{
					Symbol:    symbol,
					Bids:      []OrderBookEntry{{Price: decimal.NewFromFloat(50000.0), Amount: decimal.NewFromFloat(1.0)}},
					Asks:      []OrderBookEntry{{Price: decimal.NewFromFloat(50010.0), Amount: decimal.NewFromFloat(0.5)}},
					Timestamp: time.Now(),
					Nonce:     12345,
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	orderBook, err := service.FetchOrderBook(context.Background(), "binance", "BTC/USDT", 10)
	assert.NoError(t, err)
	assert.Equal(t, "binance", orderBook.Exchange)
	assert.Equal(t, "BTC/USDT", orderBook.Symbol)
	assert.Len(t, orderBook.OrderBook.Bids, 1)
	assert.Len(t, orderBook.OrderBook.Asks, 1)
	assert.Equal(t, decimal.NewFromFloat(50000.0), orderBook.OrderBook.Bids[0].Price)
	assert.Equal(t, decimal.NewFromFloat(50010.0), orderBook.OrderBook.Asks[0].Price)
}

// Test Service FetchOrderBook function with client error
func TestService_FetchOrderBook_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetOrderBookFunc: func(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error) {
			return nil, assert.AnError
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	orderBook, err := service.FetchOrderBook(context.Background(), "binance", "BTC/USDT", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch order book for BTC/USDT on binance")
	assert.Nil(t, orderBook)
}

// Test Service FetchOHLCV function with successful response
func TestService_FetchOHLCV_Success(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetOHLCVFunc: func(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error) {
			return &OHLCVResponse{
				Exchange:  exchange,
				Symbol:    symbol,
				Timeframe: timeframe,
				OHLCV: []OHLCV{
					{
						Timestamp: time.Now(),
						Open:      decimal.NewFromFloat(49000.0),
						High:      decimal.NewFromFloat(51000.0),
						Low:       decimal.NewFromFloat(48500.0),
						Close:     decimal.NewFromFloat(50500.0),
						Volume:    decimal.NewFromFloat(1000.0),
					},
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	ohlcv, err := service.FetchOHLCV(context.Background(), "binance", "BTC/USDT", "1h", 10)
	assert.NoError(t, err)
	assert.Equal(t, "binance", ohlcv.Exchange)
	assert.Equal(t, "BTC/USDT", ohlcv.Symbol)
	assert.Equal(t, "1h", ohlcv.Timeframe)
	assert.Len(t, ohlcv.OHLCV, 1)
	assert.Equal(t, decimal.NewFromFloat(49000.0), ohlcv.OHLCV[0].Open)
	assert.Equal(t, decimal.NewFromFloat(51000.0), ohlcv.OHLCV[0].High)
	assert.Equal(t, decimal.NewFromFloat(48500.0), ohlcv.OHLCV[0].Low)
	assert.Equal(t, decimal.NewFromFloat(50500.0), ohlcv.OHLCV[0].Close)
	assert.Equal(t, decimal.NewFromFloat(1000.0), ohlcv.OHLCV[0].Volume)
}

// Test Service FetchOHLCV function with client error
func TestService_FetchOHLCV_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetOHLCVFunc: func(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error) {
			return nil, assert.AnError
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	ohlcv, err := service.FetchOHLCV(context.Background(), "binance", "BTC/USDT", "1h", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch OHLCV for BTC/USDT on binance")
	assert.Nil(t, ohlcv)
}

// Test Service FetchTrades function with successful response
func TestService_FetchTrades_Success(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetTradesFunc: func(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error) {
			return &TradesResponse{
				Exchange: exchange,
				Symbol:   symbol,
				Trades: []Trade{
					{
						ID:        "12345",
						Timestamp: time.Now(),
						Symbol:    symbol,
						Side:      "buy",
						Amount:    decimal.NewFromFloat(0.1),
						Price:     decimal.NewFromFloat(50000.0),
						Cost:      decimal.NewFromFloat(5000.0),
					},
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	trades, err := service.FetchTrades(context.Background(), "binance", "BTC/USDT", 10)
	assert.NoError(t, err)
	assert.Equal(t, "binance", trades.Exchange)
	assert.Equal(t, "BTC/USDT", trades.Symbol)
	assert.Len(t, trades.Trades, 1)
	assert.Equal(t, "12345", trades.Trades[0].ID)
	assert.Equal(t, "buy", trades.Trades[0].Side)
	assert.Equal(t, decimal.NewFromFloat(0.1), trades.Trades[0].Amount)
	assert.Equal(t, decimal.NewFromFloat(50000.0), trades.Trades[0].Price)
	assert.Equal(t, decimal.NewFromFloat(5000.0), trades.Trades[0].Cost)
}

// Test Service FetchTrades function with client error
func TestService_FetchTrades_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetTradesFunc: func(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error) {
			return nil, assert.AnError
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	trades, err := service.FetchTrades(context.Background(), "binance", "BTC/USDT", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch trades for BTC/USDT on binance")
	assert.Nil(t, trades)
}

// Test Service FetchMarkets function with successful response
func TestService_FetchMarkets_Success(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetMarketsFunc: func(ctx context.Context, exchange string) (*MarketsResponse, error) {
			return &MarketsResponse{
				Exchange: exchange,
				Symbols:  []string{"BTC/USDT", "ETH/USDT", "BNB/USDT"},
				Count:    3,
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	markets, err := service.FetchMarkets(context.Background(), "binance")
	assert.NoError(t, err)
	assert.Equal(t, "binance", markets.Exchange)
	assert.Len(t, markets.Symbols, 3)
	assert.Equal(t, 3, markets.Count)
	assert.Equal(t, "BTC/USDT", markets.Symbols[0])
	assert.Equal(t, "ETH/USDT", markets.Symbols[1])
	assert.Equal(t, "BNB/USDT", markets.Symbols[2])
}

// Test Service FetchMarkets function with client error
func TestService_FetchMarkets_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetMarketsFunc: func(ctx context.Context, exchange string) (*MarketsResponse, error) {
			return nil, assert.AnError
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	markets, err := service.FetchMarkets(context.Background(), "binance")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch markets for binance")
	assert.Nil(t, markets)
}

// Test Service CalculateArbitrageOpportunities function with successful arbitrage detection
func TestService_CalculateArbitrageOpportunities_Success(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetTickersFunc: func(ctx context.Context, req *TickersRequest) (*TickersResponse, error) {
			return &TickersResponse{
				Tickers: []TickerData{
					{
						Exchange: "binance",
						Ticker: Ticker{
							Symbol: "BTC/USDT",
							Bid:    decimal.NewFromFloat(50000.0),
							Ask:    decimal.NewFromFloat(50100.0),
						},
					},
					{
						Exchange: "kraken",
						Ticker: Ticker{
							Symbol: "BTC/USDT",
							Bid:    decimal.NewFromFloat(51000.0), // Higher bid to create arbitrage opportunity
							Ask:    decimal.NewFromFloat(51100.0),
						},
					},
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	minProfit := decimal.NewFromFloat(1.0) // 1% minimum profit
	opportunities, err := service.CalculateArbitrageOpportunities(context.Background(), []string{"binance", "kraken"}, []string{"BTC/USDT"}, minProfit)
	assert.NoError(t, err)
	assert.Len(t, opportunities, 1)

	opportunity := opportunities[0]
	assert.Equal(t, "BTC/USDT", opportunity.Symbol)
	assert.Equal(t, "binance", opportunity.BuyExchange)
	assert.Equal(t, "kraken", opportunity.SellExchange)
	assert.Equal(t, decimal.NewFromFloat(50100.0), opportunity.BuyPrice)
	assert.Equal(t, decimal.NewFromFloat(51000.0), opportunity.SellPrice)
	assert.True(t, opportunity.ProfitPercentage.GreaterThanOrEqual(minProfit))
}

// Test Service CalculateArbitrageOpportunities function with insufficient profit
func TestService_CalculateArbitrageOpportunities_InsufficientProfit(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetTickersFunc: func(ctx context.Context, req *TickersRequest) (*TickersResponse, error) {
			return &TickersResponse{
				Tickers: []TickerData{
					{
						Exchange: "binance",
						Ticker: Ticker{
							Symbol: "BTC/USDT",
							Bid:    decimal.NewFromFloat(50000.0),
							Ask:    decimal.NewFromFloat(50050.0),
						},
					},
					{
						Exchange: "kraken",
						Ticker: Ticker{
							Symbol: "BTC/USDT",
							Bid:    decimal.NewFromFloat(50060.0),
							Ask:    decimal.NewFromFloat(50100.0),
						},
					},
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	minProfit := decimal.NewFromFloat(2.0) // 2% minimum profit (too high for this scenario)
	opportunities, err := service.CalculateArbitrageOpportunities(context.Background(), []string{"binance", "kraken"}, []string{"BTC/USDT"}, minProfit)
	assert.NoError(t, err)
	assert.Empty(t, opportunities)
}

// Test Service CalculateArbitrageOpportunities function with client error
func TestService_CalculateArbitrageOpportunities_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetTickersFunc: func(ctx context.Context, req *TickersRequest) (*TickersResponse, error) {
			return nil, assert.AnError
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	minProfit := decimal.NewFromFloat(1.0)
	opportunities, err := service.CalculateArbitrageOpportunities(context.Background(), []string{"binance", "kraken"}, []string{"BTC/USDT"}, minProfit)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch tickers for arbitrage calculation")
	assert.Nil(t, opportunities)
}

// Test Service CalculateArbitrageOpportunities function with empty exchanges
func TestService_CalculateArbitrageOpportunities_EmptyExchanges(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	service := &Service{
		client:         &MockClient{},
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	minProfit := decimal.NewFromFloat(1.0)
	opportunities, err := service.CalculateArbitrageOpportunities(context.Background(), []string{}, []string{"BTC/USDT"}, minProfit)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exchanges and symbols cannot be empty")
	assert.Nil(t, opportunities)
}

// Test Service FetchFundingRate function
func TestService_FetchFundingRate(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	expectedFundingRate := &FundingRate{
		Symbol:           "BTC/USDT",
		FundingRate:      0.0001,
		FundingTimestamp: UnixTimestamp(time.Now()),
		NextFundingTime:  UnixTimestamp(time.Now().Add(8 * time.Hour)),
		MarkPrice:        50000.0,
		IndexPrice:       50000.0,
		Timestamp:        UnixTimestamp(time.Now()),
	}

	client := &MockClient{
		GetFundingRateFunc: func(ctx context.Context, exchange, symbol string) (*FundingRate, error) {
			return expectedFundingRate, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	result, err := service.FetchFundingRate(context.Background(), "binance", "BTC/USDT")
	assert.NoError(t, err)
	assert.Equal(t, expectedFundingRate, result)
}

// Test Service FetchFundingRate function with error
func TestService_FetchFundingRate_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetFundingRateFunc: func(ctx context.Context, exchange, symbol string) (*FundingRate, error) {
			return nil, fmt.Errorf("exchange not found")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	result, err := service.FetchFundingRate(context.Background(), "binance", "BTC/USDT")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "exchange not found")
}

// Test Service FetchFundingRates function
func TestService_FetchFundingRates(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	expectedFundingRates := []FundingRate{
		{
			Symbol:           "BTC/USDT",
			FundingRate:      0.0001,
			FundingTimestamp: UnixTimestamp(time.Now()),
			NextFundingTime:  UnixTimestamp(time.Now().Add(8 * time.Hour)),
			MarkPrice:        50000.0,
			IndexPrice:       50000.0,
			Timestamp:        UnixTimestamp(time.Now()),
		},
		{
			Symbol:           "ETH/USDT",
			FundingRate:      0.0002,
			FundingTimestamp: UnixTimestamp(time.Now()),
			NextFundingTime:  UnixTimestamp(time.Now().Add(8 * time.Hour)),
			MarkPrice:        3000.0,
			IndexPrice:       3000.0,
			Timestamp:        UnixTimestamp(time.Now()),
		},
	}

	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			return expectedFundingRates, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	result, err := service.FetchFundingRates(context.Background(), "binance", []string{"BTC/USDT", "ETH/USDT"})
	assert.NoError(t, err)
	assert.Equal(t, expectedFundingRates, result)
}

// Test Service FetchFundingRates function with empty symbols
func TestService_FetchFundingRates_EmptySymbols(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			return []FundingRate{}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	result, err := service.FetchFundingRates(context.Background(), "binance", []string{})
	assert.NoError(t, err)
	assert.Empty(t, result)
}

// Test Service FetchFundingRates function with error
func TestService_FetchFundingRates_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			return nil, fmt.Errorf("exchange not found")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	result, err := service.FetchFundingRates(context.Background(), "binance", []string{"BTC/USDT"})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "exchange not found")
}

// Test Service FetchAllFundingRates function
func TestService_FetchAllFundingRates(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	expectedFundingRates := []FundingRate{
		{
			Symbol:           "BTC/USDT",
			FundingRate:      0.0001,
			FundingTimestamp: UnixTimestamp(time.Now()),
			NextFundingTime:  UnixTimestamp(time.Now().Add(8 * time.Hour)),
			MarkPrice:        50000.0,
			IndexPrice:       50000.0,
			Timestamp:        UnixTimestamp(time.Now()),
		},
		{
			Symbol:           "ETH/USDT",
			FundingRate:      0.0002,
			FundingTimestamp: UnixTimestamp(time.Now()),
			NextFundingTime:  UnixTimestamp(time.Now().Add(8 * time.Hour)),
			MarkPrice:        3000.0,
			IndexPrice:       3000.0,
			Timestamp:        UnixTimestamp(time.Now()),
		},
	}

	client := &MockClient{
		GetAllFundingRatesFunc: func(ctx context.Context, exchange string) ([]FundingRate, error) {
			return expectedFundingRates, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	result, err := service.FetchAllFundingRates(context.Background(), "binance")
	assert.NoError(t, err)
	assert.Equal(t, expectedFundingRates, result)
}

// Test Service FetchAllFundingRates function with error
func TestService_FetchAllFundingRates_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetAllFundingRatesFunc: func(ctx context.Context, exchange string) ([]FundingRate, error) {
			return nil, fmt.Errorf("exchange not found")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	result, err := service.FetchAllFundingRates(context.Background(), "binance")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "exchange not found")
}

// Test Service CalculateFundingRateArbitrage function
func TestService_CalculateFundingRateArbitrage(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client with funding rate data that creates arbitrage opportunity
	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			switch exchange {
			case "binance":
				return []FundingRate{
					{
						Symbol:      "BTC/USDT",
						FundingRate: 0.0001, // 0.01%
						MarkPrice:   50000.0,
						Timestamp:   UnixTimestamp(time.Now()),
					},
				}, nil
			case "bybit":
				return []FundingRate{
					{
						Symbol:      "BTC/USDT",
						FundingRate: -0.002, // -0.2% (negative = you receive funding)
						MarkPrice:   50000.0,
						Timestamp:   UnixTimestamp(time.Now()),
					},
				}, nil
			default:
				return []FundingRate{}, nil
			}
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	symbols := []string{"BTC/USDT"}
	exchanges := []string{"binance", "bybit"}
	minProfit := 0.5 // 0.5% minimum profit

	opportunities, err := service.CalculateFundingRateArbitrage(context.Background(), symbols, exchanges, minProfit)
	assert.NoError(t, err)
	assert.Len(t, opportunities, 1)

	opportunity := opportunities[0]
	assert.Equal(t, "BTC/USDT", opportunity.Symbol)
	assert.Equal(t, "bybit", opportunity.LongExchange)    // Negative funding rate (receive funding)
	assert.Equal(t, "binance", opportunity.ShortExchange) // Positive funding rate (pay funding)
	assert.Equal(t, -0.002, opportunity.LongFundingRate)
	assert.Equal(t, 0.0001, opportunity.ShortFundingRate)
	assert.True(t, opportunity.EstimatedProfitDaily >= minProfit)
	assert.Equal(t, 0.0, opportunity.PriceDifference) // Same mark price
	assert.Equal(t, 1.0, opportunity.RiskScore)       // No price difference
}

// Test Service CalculateFundingRateArbitrage function with insufficient profit
func TestService_CalculateFundingRateArbitrage_InsufficientProfit(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client with funding rate data that has very small difference
	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			switch exchange {
			case "binance":
				return []FundingRate{
					{
						Symbol:      "BTC/USDT",
						FundingRate: 0.0001, // 0.01%
						MarkPrice:   50000.0,
						Timestamp:   UnixTimestamp(time.Now()),
					},
				}, nil
			case "bybit":
				return []FundingRate{
					{
						Symbol:      "BTC/USDT",
						FundingRate: 0.00005, // 0.005% (very small difference)
						MarkPrice:   50000.0,
						Timestamp:   UnixTimestamp(time.Now()),
					},
				}, nil
			default:
				return []FundingRate{}, nil
			}
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	symbols := []string{"BTC/USDT"}
	exchanges := []string{"binance", "bybit"}
	minProfit := 1.0 // 1% minimum profit (too high for this scenario)

	opportunities, err := service.CalculateFundingRateArbitrage(context.Background(), symbols, exchanges, minProfit)
	assert.NoError(t, err)
	assert.Empty(t, opportunities)
}

// Test Service CalculateFundingRateArbitrage function with price difference risk
func TestService_CalculateFundingRateArbitrage_PriceDifferenceRisk(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client with funding rate data but different mark prices
	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			switch exchange {
			case "binance":
				return []FundingRate{
					{
						Symbol:      "BTC/USDT",
						FundingRate: 0.0001, // 0.01%
						MarkPrice:   50000.0,
						Timestamp:   UnixTimestamp(time.Now()),
					},
				}, nil
			case "bybit":
				return []FundingRate{
					{
						Symbol:      "BTC/USDT",
						FundingRate: -0.002,  // -0.2% (creates good arbitrage opportunity)
						MarkPrice:   50500.0, // 1% higher price
						Timestamp:   UnixTimestamp(time.Now()),
					},
				}, nil
			default:
				return []FundingRate{}, nil
			}
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	symbols := []string{"BTC/USDT"}
	exchanges := []string{"binance", "bybit"}
	minProfit := 0.5

	opportunities, err := service.CalculateFundingRateArbitrage(context.Background(), symbols, exchanges, minProfit)
	assert.NoError(t, err)
	assert.Len(t, opportunities, 1)

	opportunity := opportunities[0]
	assert.Equal(t, -500.0, opportunity.PriceDifference)                       // 50000 - 50500 (short - long)
	assert.Equal(t, 0.9900990099009901, opportunity.PriceDifferencePercentage) // |50000-50500|/50500 * 100
	assert.Equal(t, 2.0, opportunity.RiskScore)                                // Higher risk due to price difference > 0.5%
}

// Test Service CalculateFundingRateArbitrage function with insufficient exchanges
func TestService_CalculateFundingRateArbitrage_InsufficientExchanges(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			return []FundingRate{
				{
					Symbol:      "BTC/USDT",
					FundingRate: 0.0001,
					MarkPrice:   50000.0,
					Timestamp:   UnixTimestamp(time.Now()),
				},
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	symbols := []string{"BTC/USDT"}
	exchanges := []string{"binance"} // Only one exchange
	minProfit := 0.5

	opportunities, err := service.CalculateFundingRateArbitrage(context.Background(), symbols, exchanges, minProfit)
	assert.NoError(t, err)
	assert.Empty(t, opportunities) // Need at least 2 exchanges
}

// Test Service CalculateFundingRateArbitrage function with client error
func TestService_CalculateFundingRateArbitrage_ClientError(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	client := &MockClient{
		GetFundingRatesFunc: func(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
			return nil, fmt.Errorf("exchange error")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	symbols := []string{"BTC/USDT"}
	exchanges := []string{"binance", "bybit"}
	minProfit := 0.5

	opportunities, err := service.CalculateFundingRateArbitrage(context.Background(), symbols, exchanges, minProfit)
	assert.NoError(t, err) // Function should continue even if one exchange fails
	assert.Empty(t, opportunities)
}

// Exchange Management Tests

// Test Service GetExchangeConfig function
func TestService_GetExchangeConfig(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client with exchange config response
	client := &MockClient{
		GetExchangeConfigFunc: func(ctx context.Context) (*ExchangeConfigResponse, error) {
			return &ExchangeConfigResponse{
				Config: ExchangeConfig{
					BlacklistedExchanges: []string{"disabled_exchange"},
					PriorityExchanges:    []string{"binance", "bybit"},
					ExchangeConfigs:      map[string]interface{}{"binance": map[string]string{"api_key": "test"}},
				},
				ActiveExchanges:    []string{"binance", "bybit", "okx"},
				AvailableExchanges: []string{"binance", "bybit", "okx", "coinbase"},
				Timestamp:          "2024-01-01T00:00:00Z",
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	config, err := service.GetExchangeConfig(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, config)

	assert.Equal(t, []string{"disabled_exchange"}, config.Config.BlacklistedExchanges)
	assert.Equal(t, []string{"binance", "bybit"}, config.Config.PriorityExchanges)
	assert.Equal(t, []string{"binance", "bybit", "okx"}, config.ActiveExchanges)
	assert.Equal(t, []string{"binance", "bybit", "okx", "coinbase"}, config.AvailableExchanges)
	assert.Equal(t, "2024-01-01T00:00:00Z", config.Timestamp)
}

// Test Service GetExchangeConfig error
func TestService_GetExchangeConfig_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client that returns error
	client := &MockClient{
		GetExchangeConfigFunc: func(ctx context.Context) (*ExchangeConfigResponse, error) {
			return nil, fmt.Errorf("failed to get exchange config")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	config, err := service.GetExchangeConfig(context.Background())
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to get exchange config")
}

// Test Service AddExchangeToBlacklist function
func TestService_AddExchangeToBlacklist(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client
	client := &MockClient{
		AddExchangeToBlacklistFunc: func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
			return &ExchangeManagementResponse{
				Message:              "Exchange added to blacklist successfully",
				BlacklistedExchanges: []string{"problematic_exchange"},
				ActiveExchanges:      []string{"binance", "bybit"},
				AvailableExchanges:   []string{"binance", "bybit", "okx"},
				Timestamp:            "2024-01-01T00:00:00Z",
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	// Test adding exchange to blacklist
	exchange := "problematic_exchange"
	response, err := service.AddExchangeToBlacklist(context.Background(), exchange)
	assert.NoError(t, err)
	assert.NotNil(t, response)

	assert.Equal(t, "Exchange added to blacklist successfully", response.Message)
	assert.Contains(t, response.BlacklistedExchanges, exchange)

	// Verify exchange was added to blacklist cache
	isBlacklisted, _ := blacklistCache.IsBlacklisted(exchange)
	assert.True(t, isBlacklisted)
}

// Test Service AddExchangeToBlacklist error
func TestService_AddExchangeToBlacklist_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client that returns error
	client := &MockClient{
		AddExchangeToBlacklistFunc: func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
			return nil, fmt.Errorf("exchange not found")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	exchange := "nonexistent_exchange"
	response, err := service.AddExchangeToBlacklist(context.Background(), exchange)
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "exchange not found")

	// Note: The service adds to blacklist cache before calling client, so it will be in cache even on error
	isBlacklisted, _ := blacklistCache.IsBlacklisted(exchange)
	assert.True(t, isBlacklisted) // Service adds to cache first, then calls client
}

// Test Service RemoveExchangeFromBlacklist function
func TestService_RemoveExchangeFromBlacklist(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Pre-add exchange to blacklist cache
	blacklistCache.Add("problematic_exchange", "Manual blacklist", 0)
	isBlacklisted, _ := blacklistCache.IsBlacklisted("problematic_exchange")
	assert.True(t, isBlacklisted)

	// Setup mock client
	client := &MockClient{
		RemoveExchangeFromBlacklistFunc: func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
			return &ExchangeManagementResponse{
				Message:              "Exchange removed from blacklist successfully",
				BlacklistedExchanges: []string{},
				ActiveExchanges:      []string{"binance", "bybit", "problematic_exchange"},
				AvailableExchanges:   []string{"binance", "bybit", "okx", "problematic_exchange"},
				Timestamp:            "2024-01-01T00:00:00Z",
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	// Test removing exchange from blacklist
	exchange := "problematic_exchange"
	response, err := service.RemoveExchangeFromBlacklist(context.Background(), exchange)
	assert.NoError(t, err)
	assert.NotNil(t, response)

	assert.Equal(t, "Exchange removed from blacklist successfully", response.Message)
	assert.NotContains(t, response.BlacklistedExchanges, exchange)

	// Verify exchange was removed from blacklist cache
	isBlacklistedRemoved, _ := blacklistCache.IsBlacklisted(exchange)
	assert.False(t, isBlacklistedRemoved)
}

// Test Service RemoveExchangeFromBlacklist error
func TestService_RemoveExchangeFromBlacklist_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client that returns error
	client := &MockClient{
		RemoveExchangeFromBlacklistFunc: func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
			return nil, fmt.Errorf("exchange not in blacklist")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	exchange := "nonexistent_exchange"
	response, err := service.RemoveExchangeFromBlacklist(context.Background(), exchange)
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "exchange not in blacklist")
}

// Test Service RefreshExchanges function
func TestService_RefreshExchanges(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client
	client := &MockClient{
		RefreshExchangesFunc: func(ctx context.Context) (*ExchangeManagementResponse, error) {
			return &ExchangeManagementResponse{
				Message:              "Exchanges refreshed successfully",
				BlacklistedExchanges: []string{"disabled_exchange"},
				ActiveExchanges:      []string{"binance", "bybit", "okx"},
				AvailableExchanges:   []string{"binance", "bybit", "okx", "coinbase"},
				Timestamp:            "2024-01-01T00:00:00Z",
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	response, err := service.RefreshExchanges(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, response)

	assert.Equal(t, "Exchanges refreshed successfully", response.Message)
	assert.Equal(t, []string{"binance", "bybit", "okx"}, response.ActiveExchanges)
	assert.Equal(t, []string{"binance", "bybit", "okx", "coinbase"}, response.AvailableExchanges)
}

// Test Service RefreshExchanges error
func TestService_RefreshExchanges_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client that returns error
	client := &MockClient{
		RefreshExchangesFunc: func(ctx context.Context) (*ExchangeManagementResponse, error) {
			return nil, fmt.Errorf("failed to refresh exchanges")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	response, err := service.RefreshExchanges(context.Background())
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "failed to refresh exchanges")
}

// Test Service AddExchange function
func TestService_AddExchange(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client
	client := &MockClient{
		AddExchangeFunc: func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
			return &ExchangeManagementResponse{
				Message:              "Exchange added successfully",
				BlacklistedExchanges: []string{"disabled_exchange"},
				ActiveExchanges:      []string{"binance", "bybit", "okx", "new_exchange"},
				AvailableExchanges:   []string{"binance", "bybit", "okx", "coinbase", "new_exchange"},
				Timestamp:            "2024-01-01T00:00:00Z",
			}, nil
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	// Test adding a new exchange
	exchange := "new_exchange"
	response, err := service.AddExchange(context.Background(), exchange)
	assert.NoError(t, err)
	assert.NotNil(t, response)

	assert.Equal(t, "Exchange added successfully", response.Message)
	assert.Contains(t, response.ActiveExchanges, exchange)
	assert.Contains(t, response.AvailableExchanges, exchange)
}

// Test Service FetchMarketData error
func TestService_FetchMarketData_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client that returns error
	client := &MockClient{
		GetTickersFunc: func(ctx context.Context, req *TickersRequest) (*TickersResponse, error) {
			return nil, fmt.Errorf("network error")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	exchanges := []string{"binance"}
	symbols := []string{"BTC/USDT"}

	marketData, err := service.FetchMarketData(context.Background(), exchanges, symbols)
	assert.Error(t, err)
	assert.Nil(t, marketData)
	assert.Contains(t, err.Error(), "failed to fetch tickers")
}

// Test Service AddExchange error
func TestService_AddExchange_Error(t *testing.T) {
	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Setup mock client that returns error
	client := &MockClient{
		AddExchangeFunc: func(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
			return nil, fmt.Errorf("exchange already exists")
		},
	}

	service := &Service{
		client:         client,
		blacklistCache: blacklistCache,
		logger:         logger,
	}

	exchange := "existing_exchange"
	response, err := service.AddExchange(context.Background(), exchange)
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "exchange already exists")
}
