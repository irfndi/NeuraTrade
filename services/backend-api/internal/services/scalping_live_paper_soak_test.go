package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestScalpingLivePaperSoakOptionNormalization(t *testing.T) {
	cycleCases := []struct {
		name string
		in   int
		want int
	}{
		{name: "zero_defaults", in: 0, want: DefaultScalpingLivePaperSoakCycles},
		{name: "passthrough", in: 3, want: 3},
		{name: "cap_max", in: MaxScalpingLivePaperSoakCycles + 1, want: MaxScalpingLivePaperSoakCycles},
	}
	for _, tc := range cycleCases {
		t.Run("cycles_"+tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeScalpingLivePaperSoakCycles(tc.in))
		})
	}

	intervalCases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{name: "negative_to_zero", in: -time.Second, want: 0},
		{name: "passthrough", in: 2 * time.Second, want: 2 * time.Second},
		{name: "cap_max", in: MaxScalpingLivePaperSoakInterval + time.Second, want: MaxScalpingLivePaperSoakInterval},
	}
	for _, tc := range intervalCases {
		t.Run("interval_"+tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeScalpingLivePaperSoakInterval(tc.in))
		})
	}
}

func TestScalpingLivePaperSoakTimeoutScalesWithCyclesAndInterval(t *testing.T) {
	cases := []struct {
		name     string
		cycles   int
		interval time.Duration
		want     time.Duration
	}{
		{name: "scales_with_cycles_and_interval", cycles: 3, interval: 2 * time.Second, want: 2*time.Minute + 4*time.Second},
		{name: "caps_cycles_and_interval", cycles: MaxScalpingLivePaperSoakCycles + 5, interval: MaxScalpingLivePaperSoakInterval + time.Second, want: 35*time.Minute + 30*time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ScalpingLivePaperSoakTimeout(tc.cycles, tc.interval))
		})
	}
}

func TestRunPublicScalpingLivePaperSoakCyclePreservesFullSignalUniverse(t *testing.T) {
	config := DefaultAIScalpingConfig()
	config.Exchange = "binance"
	config.MaxPairsToAnalyze = 2
	config.MaxCandidatePairs = 2
	config.OrderBookPairs = 2
	config.AutoExpandOrderBooks = false
	config.EnforceFutures = false

	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{
			Exchange: "binance",
			Symbols:  []string{"BTC/USDT", "ETH/USDT"},
			Count:    2,
		},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{symbol: "BTC/USDT", price: 98, volume: 2_000_000, high24h: 104, low24h: 96, bid: 97.99, ask: 98.01, exchange: "binance"},
			mockMarketPrice{symbol: "ETH/USDT", price: 100, volume: 1_500_000, high24h: 104, low24h: 96, bid: 99.99, ask: 100.01, exchange: "binance"},
		},
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"BTC/USDT": {
				OrderBook: ccxt.OrderBook{
					Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(97.99), Amount: decimal.NewFromInt(10)}},
					Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(98.01), Amount: decimal.NewFromInt(1)}},
				},
			},
			"ETH/USDT": {
				OrderBook: ccxt.OrderBook{
					Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(99.99), Amount: decimal.NewFromInt(5)}},
					Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(100.01), Amount: decimal.NewFromInt(5)}},
				},
			},
		},
	}
	svc := NewAIScalpingService(config, nil, nil, mockCCXT, nil, nil)

	result, _, err := runPublicScalpingLivePaperSoakCycle(
		context.Background(),
		svc,
		config,
		config.Exchange,
		decimal.NewFromInt(1000),
		decimal.NewFromFloat(0.0006),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Signals, 2)
	require.ElementsMatch(t, []string{"BTC/USDT", "ETH/USDT"}, []string{
		result.Signals[0].Symbol,
		result.Signals[1].Symbol,
	})
}
