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

type mockCCXTServiceForReplay struct {
	ohlcvResponse *ccxt.OHLCVResponse
	err           error
}

func (m *mockCCXTServiceForReplay) Initialize(ctx context.Context) error { return nil }
func (m *mockCCXTServiceForReplay) IsHealthy(ctx context.Context) bool   { return true }
func (m *mockCCXTServiceForReplay) Close() error                         { return nil }
func (m *mockCCXTServiceForReplay) GetServiceURL() string                { return "http://localhost" }
func (m *mockCCXTServiceForReplay) GetSupportedExchanges() []string      { return []string{"binance"} }
func (m *mockCCXTServiceForReplay) GetExchangeInfo(exchangeID string) (ccxt.ExchangeInfo, bool) {
	return ccxt.ExchangeInfo{}, false
}
func (m *mockCCXTServiceForReplay) GetExchangeConfig(ctx context.Context) (*ccxt.ExchangeConfigResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) AddExchangeToBlacklist(ctx context.Context, ex string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) RemoveExchangeFromBlacklist(ctx context.Context, ex string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) RefreshExchanges(ctx context.Context) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) AddExchange(ctx context.Context, ex string) (*ccxt.ExchangeManagementResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchMarketData(ctx context.Context, ex []string, syms []string) ([]ccxt.MarketPriceInterface, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchSingleTicker(ctx context.Context, ex, sym string) (ccxt.MarketPriceInterface, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchOrderBook(ctx context.Context, ex, sym string, limit int) (*ccxt.OrderBookResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) CalculateOrderBookMetrics(ctx context.Context, ex, sym string, limit int) (*ccxt.OrderBookMetrics, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchTrades(ctx context.Context, ex, sym string, limit int) (*ccxt.TradesResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchMarkets(ctx context.Context, ex string) (*ccxt.MarketsResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchFundingRate(ctx context.Context, ex, sym string) (*ccxt.FundingRate, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchFundingRates(ctx context.Context, ex string, syms []string) ([]ccxt.FundingRate, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchAllFundingRates(ctx context.Context, ex string) ([]ccxt.FundingRate, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) CalculateArbitrageOpportunities(ctx context.Context, ex []string, syms []string, min decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) CalculateFundingRateArbitrage(ctx context.Context, syms []string, ex []string, min float64) ([]ccxt.FundingArbitrageOpportunity, error) {
	return nil, nil
}
func (m *mockCCXTServiceForReplay) FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*ccxt.OHLCVResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.ohlcvResponse, nil
}

func TestOHLCVReplayEngine_New(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	require.NotNil(t, engine)
	assert.Equal(t, ReplayStatusIdle, engine.GetStatus())
}

func TestOHLCVReplayEngine_Configure(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)

	config := ReplayConfig{
		Symbol:    "BTC/USDT",
		Exchange:  "binance",
		Timeframe: "1h",
		Speed:     1.0,
	}

	engine.Configure(config)
	assert.Equal(t, "BTC/USDT", engine.config.Symbol)
	assert.Equal(t, "binance", engine.config.Exchange)
}

func TestOHLCVReplayEngine_GetStatus(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	assert.Equal(t, ReplayStatusIdle, engine.GetStatus())

	engine.status = ReplayStatusPlaying
	assert.Equal(t, ReplayStatusPlaying, engine.GetStatus())
}

func TestOHLCVReplayEngine_GetProgress(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	assert.Equal(t, 0.0, engine.GetProgress())

	engine.candles = make([]ReplayCandle, 10)
	engine.currentIndex = 5
	assert.Equal(t, 50.0, engine.GetProgress())

	engine.currentIndex = 10
	assert.Equal(t, 100.0, engine.GetProgress())
}

func TestOHLCVReplayEngine_GetCurrentCandle(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	assert.Nil(t, engine.GetCurrentCandle())

	engine.candles = []ReplayCandle{
		{Timestamp: time.Now(), Open: decimal.NewFromInt(100)},
		{Timestamp: time.Now().Add(time.Hour), Open: decimal.NewFromInt(101)},
	}
	engine.currentIndex = 0

	candle := engine.GetCurrentCandle()
	require.NotNil(t, candle)
	assert.Equal(t, decimal.NewFromInt(100), candle.Open)
}

func TestOHLCVReplayEngine_Seek(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	engine.candles = make([]ReplayCandle, 10)

	err := engine.Seek(5)
	require.NoError(t, err)
	assert.Equal(t, 5, engine.currentIndex)

	err = engine.Seek(-1)
	assert.Error(t, err)

	err = engine.Seek(10)
	assert.Error(t, err)
}

func TestOHLCVReplayEngine_PauseResume(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)

	err := engine.Pause()
	assert.Error(t, err)

	engine.status = ReplayStatusPlaying
	err = engine.Pause()
	require.NoError(t, err)
	assert.Equal(t, ReplayStatusPaused, engine.GetStatus())

	err = engine.Resume()
	require.NoError(t, err)
	assert.Equal(t, ReplayStatusPlaying, engine.GetStatus())
}

func TestOHLCVReplayEngine_Stop(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	engine.candles = make([]ReplayCandle, 10)
	engine.currentIndex = 5
	engine.status = ReplayStatusPlaying

	err := engine.Stop()
	require.NoError(t, err)
	assert.Equal(t, ReplayStatusComplete, engine.GetStatus())
	assert.Equal(t, 0, engine.currentIndex)
}

func TestOHLCVReplayEngine_SetSpeed(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)

	engine.SetSpeed(2.0)
	assert.Equal(t, 2.0, engine.config.Speed)
}

func TestOHLCVReplayEngine_GetSummary(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	engine.config = ReplayConfig{
		Symbol:    "BTC/USDT",
		Exchange:  "binance",
		Timeframe: "1h",
	}

	summary := engine.GetSummary()
	assert.Equal(t, "BTC/USDT", summary.Symbol)
	assert.Equal(t, 0, summary.TotalCandles)

	engine.candles = []ReplayCandle{
		{Open: decimal.NewFromFloat(100), High: decimal.NewFromFloat(105), Low: decimal.NewFromFloat(98), Close: decimal.NewFromFloat(102), Volume: decimal.NewFromFloat(1000)},
		{Open: decimal.NewFromFloat(102), High: decimal.NewFromFloat(110), Low: decimal.NewFromFloat(100), Close: decimal.NewFromFloat(108), Volume: decimal.NewFromFloat(1500)},
	}
	engine.currentIndex = 1

	summary = engine.GetSummary()
	assert.Equal(t, 2, summary.TotalCandles)
	assert.Equal(t, decimal.NewFromFloat(100), summary.FirstPrice)
	assert.Equal(t, decimal.NewFromFloat(108), summary.LastPrice)
}

func TestOHLCVReplayEngine_TimeframeToDuration(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)

	tests := []struct {
		timeframe string
		expected  time.Duration
	}{
		{"1m", time.Minute},
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"4h", 4 * time.Hour},
		{"1d", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.timeframe, func(t *testing.T) {
			result := engine.timeframeToDuration(tt.timeframe)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOHLCVReplayEngine_Load_NoData(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)
	engine.config = ReplayConfig{
		Symbol:    "BTC/USDT",
		Exchange:  "binance",
		Timeframe: "1h",
		StartTime: time.Now().Add(-24 * time.Hour),
		EndTime:   time.Now(),
	}

	ctx := context.Background()
	err := engine.Load(ctx)
	assert.Error(t, err)
	assert.Equal(t, ReplayStatusError, engine.GetStatus())
}

func TestOHLCVReplayEngine_Play_NotLoaded(t *testing.T) {
	engine := NewOHLCVReplayEngine(nil, nil)

	ctx := context.Background()
	err := engine.Play(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no candles loaded")
}

func TestOHLCVReplayEngine_PlayWithCandles(t *testing.T) {
	mockService := &mockCCXTServiceForReplay{
		ohlcvResponse: &ccxt.OHLCVResponse{
			Exchange: "binance",
			Symbol:   "BTC/USDT",
			OHLCV: []ccxt.OHLCV{
				{Timestamp: time.Now().Add(-2 * time.Hour), Open: decimal.NewFromFloat(100), High: decimal.NewFromFloat(105), Low: decimal.NewFromFloat(98), Close: decimal.NewFromFloat(102), Volume: decimal.NewFromFloat(1000)},
				{Timestamp: time.Now().Add(-1 * time.Hour), Open: decimal.NewFromFloat(102), High: decimal.NewFromFloat(108), Low: decimal.NewFromFloat(100), Close: decimal.NewFromFloat(106), Volume: decimal.NewFromFloat(1200)},
			},
		},
	}

	engine := NewOHLCVReplayEngine(nil, mockService)
	engine.config = ReplayConfig{
		Symbol:    "BTC/USDT",
		Exchange:  "binance",
		Timeframe: "1h",
		StartTime: time.Now().Add(-24 * time.Hour),
		EndTime:   time.Now(),
		Speed:     1000,
	}

	ctx := context.Background()
	err := engine.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, ReplayStatusIdle, engine.GetStatus())
	assert.Len(t, engine.candles, 2)
}

func TestReplaySummary_Calculations(t *testing.T) {
	summary := &ReplaySummary{
		FirstPrice: decimal.NewFromFloat(100),
		LastPrice:  decimal.NewFromFloat(110),
	}

	summary.PriceChange = summary.LastPrice.Sub(summary.FirstPrice)
	summary.PriceChangePct = summary.PriceChange.Div(summary.FirstPrice).Mul(decimal.NewFromInt(100))

	assert.True(t, summary.PriceChange.Equal(decimal.NewFromInt(10)))
	assert.True(t, summary.PriceChangePct.Equal(decimal.NewFromInt(10)))
}
