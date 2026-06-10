package ccxt

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTickerMarketPriceAdapter_DecimalPrecision verifies that TickerMarketPriceAdapter
// returns exact decimal values without float64 precision loss.
func TestTickerMarketPriceAdapter_DecimalPrecision(t *testing.T) {
	tests := []struct {
		name           string
		last           string
		bid            string
		ask            string
		high           string
		low            string
		volume         string
		expectedPrice  string
		expectedBid    string
		expectedAsk    string
		expectedHigh   string
		expectedLow    string
		expectedVolume string
	}{
		{
			name:           "exact BTC price with many decimals",
			last:           "67123.45678901",
			bid:            "67123.45678900",
			ask:            "67123.45678902",
			high:           "68000.12345678",
			low:            "66000.12345678",
			volume:         "1234.56789012",
			expectedPrice:  "67123.45678901",
			expectedBid:    "67123.45678900",
			expectedAsk:    "67123.45678902",
			expectedHigh:   "68000.12345678",
			expectedLow:    "66000.12345678",
			expectedVolume: "1234.56789012",
		},
		{
			name:           "small USDT price",
			last:           "0.00000123",
			bid:            "0.00000122",
			ask:            "0.00000124",
			high:           "0.00000150",
			low:            "0.00000100",
			volume:         "99999999.99",
			expectedPrice:  "0.00000123",
			expectedBid:    "0.00000122",
			expectedAsk:    "0.00000124",
			expectedHigh:   "0.00000150",
			expectedLow:    "0.00000100",
			expectedVolume: "99999999.99",
		},
		{
			name:           "large notional value",
			last:           "999999999.99999999",
			bid:            "999999999.99999998",
			ask:            "1000000000.00000001",
			high:           "1000000000.00000001",
			low:            "999999999.99999998",
			volume:         "0.00000001",
			expectedPrice:  "999999999.99999999",
			expectedBid:    "999999999.99999998",
			expectedAsk:    "1000000000.00000001",
			expectedHigh:   "1000000000.00000001",
			expectedLow:    "999999999.99999998",
			expectedVolume: "0.00000001",
		},
		{
			name:           "zero values",
			last:           "0",
			bid:            "0",
			ask:            "0",
			high:           "0",
			low:            "0",
			volume:         "0",
			expectedPrice:  "0",
			expectedBid:    "0",
			expectedAsk:    "0",
			expectedHigh:   "0",
			expectedLow:    "0",
			expectedVolume: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			last, err := decimal.NewFromString(tt.last)
			require.NoError(t, err)
			bid, err := decimal.NewFromString(tt.bid)
			require.NoError(t, err)
			ask, err := decimal.NewFromString(tt.ask)
			require.NoError(t, err)
			high, err := decimal.NewFromString(tt.high)
			require.NoError(t, err)
			low, err := decimal.NewFromString(tt.low)
			require.NoError(t, err)
			volume, err := decimal.NewFromString(tt.volume)
			require.NoError(t, err)

			adapter := &TickerMarketPriceAdapter{
				data: &TickerData{
					Exchange: "test",
					Ticker: Ticker{
						Symbol:    "BTC/USDT",
						Last:      last,
						Bid:       bid,
						Ask:       ask,
						High:      high,
						Low:       low,
						Volume:    volume,
						Timestamp: UnixTimestamp(time.Now()),
					},
				},
			}

			assert.True(t, adapter.GetPrice().Equal(last), "GetPrice: expected %s got %s", tt.expectedPrice, adapter.GetPrice())
			assert.True(t, adapter.GetBid().Equal(bid), "GetBid: expected %s got %s", tt.expectedBid, adapter.GetBid())
			assert.True(t, adapter.GetAsk().Equal(ask), "GetAsk: expected %s got %s", tt.expectedAsk, adapter.GetAsk())
			assert.True(t, adapter.GetHigh().Equal(high), "GetHigh: expected %s got %s", tt.expectedHigh, adapter.GetHigh())
			assert.True(t, adapter.GetLow().Equal(low), "GetLow: expected %s got %s", tt.expectedLow, adapter.GetLow())
			assert.True(t, adapter.GetVolume().Equal(volume), "GetVolume: expected %s got %s", tt.expectedVolume, adapter.GetVolume())
		})
	}
}

// TestBalanceResponse_DecimalMaps verifies BalanceResponse uses decimal.Decimal maps.
func TestBalanceResponse_DecimalMaps(t *testing.T) {
	tests := []struct {
		name  string
		total map[string]decimal.Decimal
		free  map[string]decimal.Decimal
		used  map[string]decimal.Decimal
	}{
		{
			name: "standard BTC + USDT balance",
			total: map[string]decimal.Decimal{
				"BTC":  decimal.RequireFromString("1.23456789"),
				"USDT": decimal.RequireFromString("10000.50"),
			},
			free: map[string]decimal.Decimal{
				"BTC":  decimal.RequireFromString("0.5"),
				"USDT": decimal.RequireFromString("8000.50"),
			},
			used: map[string]decimal.Decimal{
				"BTC":  decimal.RequireFromString("0.73456789"),
				"USDT": decimal.RequireFromString("2000.00"),
			},
		},
		{
			name: "fractional precision preserved",
			total: map[string]decimal.Decimal{
				"USDT": decimal.RequireFromString("999999.999999999999"),
			},
			free: map[string]decimal.Decimal{
				"USDT": decimal.RequireFromString("999999.999999999999"),
			},
			used: map[string]decimal.Decimal{
				"USDT": decimal.Zero,
			},
		},
		{
			name:  "empty balance",
			total: map[string]decimal.Decimal{},
			free:  map[string]decimal.Decimal{},
			used:  map[string]decimal.Decimal{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &BalanceResponse{
				Exchange:  "test",
				Timestamp: time.Now(),
				Total:     tt.total,
				Free:      tt.free,
				Used:      tt.used,
			}

			for key, expected := range tt.total {
				assert.True(t, resp.Total[key].Equal(expected),
					"Total[%s]: expected %s got %s", key, expected, resp.Total[key])
			}
			for key, expected := range tt.free {
				assert.True(t, resp.Free[key].Equal(expected),
					"Free[%s]: expected %s got %s", key, expected, resp.Free[key])
			}
			for key, expected := range tt.used {
				assert.True(t, resp.Used[key].Equal(expected),
					"Used[%s]: expected %s got %s", key, expected, resp.Used[key])
			}

			// Verify Total = Free + Used
			for key := range resp.Total {
				sum := resp.Free[key].Add(resp.Used[key])
				assert.True(t, resp.Total[key].Equal(sum),
					"Total[%s] should equal Free + Used: %s != %s + %s",
					key, resp.Total[key], resp.Free[key], resp.Used[key])
			}
		})
	}
}

// TestCalculateOrderBookMetrics_DecimalPrecision verifies that order book metrics
// maintain decimal precision through spread and depth calculations.
func TestCalculateOrderBookMetrics_DecimalPrecision(t *testing.T) {
	client := &Client{}

	resp := &OrderBookResponse{
		Exchange: "test",
		Symbol:   "BTC/USDT",
		OrderBook: OrderBook{
			Symbol: "BTC/USDT",
			Bids: []OrderBookEntry{
				{Price: decimal.RequireFromString("50000.12345678"), Amount: decimal.RequireFromString("1.5")},
				{Price: decimal.RequireFromString("49999.12345678"), Amount: decimal.RequireFromString("2.0")},
			},
			Asks: []OrderBookEntry{
				{Price: decimal.RequireFromString("50000.12345679"), Amount: decimal.RequireFromString("1.5")},
				{Price: decimal.RequireFromString("50001.12345679"), Amount: decimal.RequireFromString("2.0")},
			},
			Timestamp: time.Now(),
		},
		Timestamp: "1234567890",
	}

	metrics := client.CalculateOrderBookMetrics(resp)
	require.NotNil(t, metrics)

	// Mid price should be exact average
	expectedMid := decimal.RequireFromString("50000.123456785")
	assert.True(t, metrics.MidPrice.Equal(expectedMid),
		"MidPrice: expected %s got %s", expectedMid, metrics.MidPrice)

	// Spread should be exact
	expectedSpread := decimal.RequireFromString("0.00000001")
	spread := metrics.BestAsk.Sub(metrics.BestBid)
	assert.True(t, spread.Equal(expectedSpread),
		"Spread: expected %s got %s", expectedSpread, spread)
}
