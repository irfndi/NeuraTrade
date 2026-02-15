package testmocks

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
)

// ExchangeMockConfig contains configuration for an exchange mock
type ExchangeMockConfig struct {
	ExchangeID      string
	Latency         time.Duration // Simulated API latency
	RateLimitCalls  int           // Calls per interval
	RateLimitWindow time.Duration // Window duration
	ErrorRate       float64       // Probability of returning an error (0-1)
	Symbols         []string
	Prices          map[string]decimal.Decimal // Base prices for symbols
	VolumeRange     [2]float64                 // Min/max volume
	SpreadBPS       int                        // Bid-ask spread in basis points
}

// NewExchangeMockConfig creates a default config for an exchange
func NewExchangeMockConfig(exchangeID string) *ExchangeMockConfig {
	basePrices := map[string]decimal.Decimal{
		"BTC/USDT":  decimal.NewFromInt(67500),
		"ETH/USDT":  decimal.NewFromInt(3450),
		"SOL/USDT":  decimal.NewFromInt(185),
		"BNB/USDT":  decimal.NewFromInt(580),
		"XRP/USDT":  decimal.NewFromFloat(0.52),
		"ADA/USDT":  decimal.NewFromFloat(0.45),
		"DOGE/USDT": decimal.NewFromFloat(0.08),
		"AVAX/USDT": decimal.NewFromInt(35),
	}

	return &ExchangeMockConfig{
		ExchangeID:      exchangeID,
		Latency:         50 * time.Millisecond,
		RateLimitCalls:  1200,
		RateLimitWindow: time.Minute,
		ErrorRate:       0.0,
		Symbols:         []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "XRP/USDT"},
		Prices:          basePrices,
		VolumeRange:     [2]float64{100, 10000},
		SpreadBPS:       5, // 0.05% spread
	}
}

// BinanceMock creates a Binance exchange mock config
func BinanceMock() *ExchangeMockConfig {
	cfg := NewExchangeMockConfig("binance")
	cfg.RateLimitCalls = 1200
	cfg.SpreadBPS = 5
	return cfg
}

// KrakenMock creates a Kraken exchange mock config
func KrakenMock() *ExchangeMockConfig {
	cfg := NewExchangeMockConfig("kraken")
	cfg.Latency = 100 * time.Millisecond
	cfg.RateLimitCalls = 900
	cfg.SpreadBPS = 8
	return cfg
}

// CoinbaseMock creates a Coinbase exchange mock config
func CoinbaseMock() *ExchangeMockConfig {
	cfg := NewExchangeMockConfig("coinbase")
	cfg.Latency = 80 * time.Millisecond
	cfg.RateLimitCalls = 10 // Highly rate limited
	cfg.SpreadBPS = 10
	return cfg
}

// PolymarketMock creates a Polymarket exchange mock config
func PolymarketMock() *ExchangeMockConfig {
	cfg := NewExchangeMockConfig("polymarket")
	cfg.Latency = 150 * time.Millisecond
	cfg.RateLimitCalls = 300
	cfg.SpreadBPS = 15
	// Polymarket uses different symbol format
	cfg.Prices = map[string]decimal.Decimal{
		"TRUMP_WIN": decimal.NewFromFloat(0.65),
		"BTC_70K":   decimal.NewFromFloat(0.45),
		"ETH_4K":    decimal.NewFromFloat(0.35),
	}
	cfg.Symbols = []string{"TRUMP_WIN", "BTC_70K", "ETH_4K"}
	return cfg
}

// mockTicker holds internal ticker data for the mock
type mockTicker struct {
	symbol    string
	bid       decimal.Decimal
	ask       decimal.Decimal
	last      decimal.Decimal
	high      decimal.Decimal
	low       decimal.Decimal
	volume    decimal.Decimal
	timestamp time.Time
}

// ExchangeMock implements a mock exchange for testing
type ExchangeMock struct {
	config    *ExchangeMockConfig
	callCount int
	lastReset time.Time
	data      map[string]*mockTicker
}

// NewExchangeMock creates a new exchange mock
func NewExchangeMock(config *ExchangeMockConfig) *ExchangeMock {
	mock := &ExchangeMock{
		config:    config,
		callCount: 0,
		lastReset: time.Now(),
		data:      make(map[string]*mockTicker),
	}

	// Initialize with base prices
	for symbol, price := range config.Prices {
		mock.data[symbol] = mock.generateTickerData(symbol, price)
	}

	return mock
}

// generateTickerData generates realistic ticker data for a symbol
func (m *ExchangeMock) generateTickerData(symbol string, basePrice decimal.Decimal) *mockTicker {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Add some randomness to price (±2%)
	priceVariation := basePrice.Mul(decimal.NewFromFloat(0.98 + r.Float64()*0.04))
	spread := priceVariation.Mul(decimal.NewFromInt(int64(m.config.SpreadBPS))).Div(decimal.NewFromInt(10000))

	bid := priceVariation.Sub(spread)
	ask := priceVariation.Add(spread)

	// Generate volume
	volume := m.config.VolumeRange[0] + r.Float64()*(m.config.VolumeRange[1]-m.config.VolumeRange[0])

	// Calculate 24h stats
	highPrice := basePrice.Mul(decimal.NewFromFloat(1.0 + r.Float64()*0.03))
	lowPrice := basePrice.Mul(decimal.NewFromFloat(1.0 - r.Float64()*0.03))

	return &mockTicker{
		symbol:    symbol,
		bid:       bid,
		ask:       ask,
		last:      priceVariation,
		high:      highPrice,
		low:       lowPrice,
		volume:    decimal.NewFromFloat(volume),
		timestamp: time.Now(),
	}
}

// checkRateLimit checks if we're within rate limits
func (m *ExchangeMock) checkRateLimit() error {
	// Reset counter if window has passed
	if time.Since(m.lastReset) > m.config.RateLimitWindow {
		m.callCount = 0
		m.lastReset = time.Now()
	}

	m.callCount++
	if m.callCount > m.config.RateLimitCalls {
		return fmt.Errorf("rate limit exceeded: %d calls in %v", m.callCount, m.config.RateLimitWindow)
	}

	return nil
}

// checkError randomly returns an error based on error rate
func (m *ExchangeMock) checkError() error {
	if m.config.ErrorRate > 0 && rand.Float64() < m.config.ErrorRate {
		return fmt.Errorf("simulated exchange error")
	}
	return nil
}

// GetTicker returns a ticker for a symbol
func (m *ExchangeMock) GetTicker(ctx context.Context, symbol string) (*ccxt.TickerResponse, error) {
	// Check rate limit
	if err := m.checkRateLimit(); err != nil {
		return nil, err
	}

	// Check for random errors
	if err := m.checkError(); err != nil {
		return nil, err
	}

	// Simulate latency
	time.Sleep(m.config.Latency)

	// Get or generate ticker data
	tickerData, exists := m.data[symbol]
	if !exists {
		// Generate new symbol if not in base config
		basePrice := decimal.NewFromInt(100)
		tickerData = m.generateTickerData(symbol, basePrice)
		m.data[symbol] = tickerData
	}

	// Update with new random data occasionally
	if rand.Float64() > 0.7 {
		tickerData = m.generateTickerData(symbol, tickerData.last)
		m.data[symbol] = tickerData
	}

	return &ccxt.TickerResponse{
		Exchange: m.config.ExchangeID,
		Symbol:   tickerData.symbol,
		Ticker: ccxt.Ticker{
			Last:      tickerData.last,
			High:      tickerData.high,
			Low:       tickerData.low,
			Bid:       tickerData.bid,
			Ask:       tickerData.ask,
			Volume:    tickerData.volume,
			Timestamp: ccxt.UnixTimestamp(tickerData.timestamp),
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// GetOrderBook returns order book data
func (m *ExchangeMock) GetOrderBook(ctx context.Context, symbol string, limit int) (*ccxt.OrderBookResponse, error) {
	if err := m.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := m.checkError(); err != nil {
		return nil, err
	}

	time.Sleep(m.config.Latency)

	tickerData, exists := m.data[symbol]
	if !exists {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}

	// Generate order book around current price
	bid := tickerData.bid
	ask := tickerData.ask
	bidStep := bid.Mul(decimal.NewFromFloat(0.001)) // 0.1% steps
	askStep := ask.Mul(decimal.NewFromFloat(0.001))

	bids := make([]ccxt.OrderBookEntry, 0, limit)
	asks := make([]ccxt.OrderBookEntry, 0, limit)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < limit; i++ {
		// Bids (price descending)
		bidPrice := bid.Sub(bidStep.Mul(decimal.NewFromInt(int64(i))))
		bidQty := decimal.NewFromFloat(r.Float64() * 10)
		bids = append(bids, ccxt.OrderBookEntry{Price: bidPrice, Amount: bidQty})

		// Asks (price ascending)
		askPrice := ask.Add(askStep.Mul(decimal.NewFromInt(int64(i))))
		askQty := decimal.NewFromFloat(r.Float64() * 10)
		asks = append(asks, ccxt.OrderBookEntry{Price: askPrice, Amount: askQty})
	}

	return &ccxt.OrderBookResponse{
		Exchange: m.config.ExchangeID,
		Symbol:   symbol,
		OrderBook: ccxt.OrderBook{
			Symbol:    symbol,
			Bids:      bids,
			Asks:      asks,
			Timestamp: time.Now(),
			Nonce:     time.Now().UnixMilli(),
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// GetOHLCV returns OHLCV data
func (m *ExchangeMock) GetOHLCV(ctx context.Context, symbol, timeframe string, limit int) (*ccxt.OHLCVResponse, error) {
	if err := m.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := m.checkError(); err != nil {
		return nil, err
	}

	time.Sleep(m.config.Latency)

	tickerData, exists := m.data[symbol]
	if !exists {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}

	// Generate fake OHLCV candles
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	candles := make([]ccxt.OHLCV, 0, limit)

	intervalMinutes := map[string]int{"1m": 1, "5m": 5, "15m": 15, "1h": 60, "4h": 240, "1d": 1440}
	minutes, ok := intervalMinutes[timeframe]
	if !ok {
		minutes = 1
	}

	baseTime := time.Now().Add(-time.Duration(limit*minutes) * time.Minute)
	currentPrice := tickerData.last

	for i := 0; i < limit; i++ {
		// Random walk for price
		open := currentPrice
		change := open.Mul(decimal.NewFromFloat((r.Float64() - 0.5) * 0.02))
		close := open.Add(change)
		high := open.Add(change.Abs())
		low := open.Sub(change.Abs())
		volume := decimal.NewFromFloat(r.Float64() * 1000)

		candles = append(candles, ccxt.OHLCV{
			Timestamp: baseTime.Add(time.Duration(i*minutes) * time.Minute),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		})

		currentPrice = close
	}

	return &ccxt.OHLCVResponse{
		Exchange:  m.config.ExchangeID,
		Symbol:    symbol,
		Timeframe: timeframe,
		OHLCV:     candles,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// MockExchangeServer manages multiple exchange mocks
type MockExchangeServer struct {
	exchanges map[string]*ExchangeMock
}

// NewMockExchangeServer creates a new mock server with default exchanges
func NewMockExchangeServer() *MockExchangeServer {
	server := &MockExchangeServer{
		exchanges: make(map[string]*ExchangeMock),
	}

	// Add default exchanges
	server.exchanges["binance"] = NewExchangeMock(BinanceMock())
	server.exchanges["kraken"] = NewExchangeMock(KrakenMock())
	server.exchanges["coinbase"] = NewExchangeMock(CoinbaseMock())
	server.exchanges["polymarket"] = NewExchangeMock(PolymarketMock())

	return server
}

// GetExchange returns an exchange mock by name
func (s *MockExchangeServer) GetExchange(name string) (*ExchangeMock, bool) {
	mock, exists := s.exchanges[name]
	return mock, exists
}

// AddExchange adds a custom exchange mock
func (s *MockExchangeServer) AddExchange(name string, config *ExchangeMockConfig) {
	s.exchanges[name] = NewExchangeMock(config)
}

// SetErrorRate sets the error rate for an exchange
func (s *MockExchangeServer) SetErrorRate(exchange string, rate float64) error {
	mock, exists := s.exchanges[exchange]
	if !exists {
		return fmt.Errorf("exchange not found: %s", exchange)
	}
	mock.config.ErrorRate = rate
	return nil
}

// SetLatency sets the latency for an exchange
func (s *MockExchangeServer) SetLatency(exchange string, latency time.Duration) error {
	mock, exists := s.exchanges[exchange]
	if !exists {
		return fmt.Errorf("exchange not found: %s", exchange)
	}
	mock.config.Latency = latency
	return nil
}

// LoadFixtures loads test data from fixture files
func (s *MockExchangeServer) LoadFixtures(fixturePath string) error {
	// This would load actual fixture data if needed
	// For now, just use the default generated data
	return nil
}
