package services

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func newCloseTestEngine() *ScalpingBacktestEngine {
	return NewScalpingBacktestEngine(nil, ScalpingBacktestConfig{
		StartTime:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Symbols:            []string{"X/USDT"},
		Exchange:           "binance",
		InitialCapital:     decimal.NewFromInt(10000),
		FeeRate:            decimal.NewFromFloat(0.001),
		SlippagePct:        decimal.NewFromFloat(0.000001),
		MaxBidAskSpreadPct: 1,
		MinConfidence:      0.55,
		MinExpectancyN:     99,
		MaxCapitalPct:      100,
		DefaultHoldPeriod:  24 * time.Hour,
	})
}

func makeCloseSignal(price, low, high float64) HistoricalSignal {
	return HistoricalSignal{
		Timestamp: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		Symbol:    "X/USDT",
		Exchange:  "binance",
		Signal: MarketSignal{
			Symbol: "X/USDT",
			Price:  price,
			Low:    low,
			High:   high,
		},
	}
}

func makeCloseTestPosition(side string, entry, sl, tp decimal.Decimal) *SimulatedPosition {
	return &SimulatedPosition{
		Symbol:      "X/USDT",
		Side:        side,
		EntryPrice:  entry,
		StopLoss:    sl,
		TakeProfit:  tp,
		HoldCandles: 0,
		Size:        decimal.NewFromInt(1),
		Notional:    entry,
	}
}

func assertPriceEqual(t *testing.T, expected, actual decimal.Decimal, msgAndArgs ...interface{}) {
	t.Helper()
	assert.True(t, expected.Sub(actual).Abs().LessThan(decimal.NewFromFloat(0.01)),
		"expected %s, got %s (diff > 0.01) %v", expected.String(), actual.String(), msgAndArgs)
}

func TestCloseSimulatedPosition_BuyMarkPriceTriggersMaxLoss(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("buy", decimal.NewFromInt(100), decimal.Zero, decimal.NewFromInt(120))
	signal := makeCloseSignal(98.0, 98.5, 99.0)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "max_loss", trade.ExitReason)
	assertPriceEqual(t, decimal.NewFromInt(98), trade.ExitPrice, "markPrice (98) is below maxLossPrice (98.5), so exit at markPrice")
}

func TestCloseSimulatedPosition_BuyMarkPriceTriggersStopLossWhenTighter(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("buy", decimal.NewFromInt(100), decimal.NewFromFloat(99.0), decimal.NewFromInt(120))
	signal := makeCloseSignal(98.7, 99.0, 99.5)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "stop_loss", trade.ExitReason, "markPrice=98.7 is in (max_loss=98.5, SL=99.0), so SL fires")
	assertPriceEqual(t, decimal.NewFromFloat(98.7), trade.ExitPrice, "markPrice (98.7) is below SL (99.0), exit at markPrice")
}

func TestCloseSimulatedPosition_BuyCandleLowStillTriggersMaxLoss(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("buy", decimal.NewFromInt(100), decimal.NewFromInt(120), decimal.NewFromInt(120))
	signal := makeCloseSignal(99.0, 98.0, 100.5)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "max_loss", trade.ExitReason, "candleLow 98.0 is below maxLossPrice 98.5")
	assertPriceEqual(t, decimal.NewFromFloat(98.5), trade.ExitPrice, "exit at maxLossPrice since markPrice is above it")
}

func TestCloseSimulatedPosition_BuyConservativeExitAtSLWhenCandleLowTriggersSL(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("buy", decimal.NewFromInt(100), decimal.NewFromFloat(99.0), decimal.NewFromInt(120))
	signal := makeCloseSignal(99.5, 98.7, 99.7)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "stop_loss", trade.ExitReason, "candleLow=98.7 is in (max_loss=98.5, SL=99.0)")
	assertPriceEqual(t, decimal.NewFromFloat(99.0), trade.ExitPrice, "markPrice (99.5) is above SL (99.0), exit at SL")
}

func TestCloseSimulatedPosition_BuyNoTriggerWhenPriceInRange(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("buy", decimal.NewFromInt(100), decimal.NewFromFloat(95.0), decimal.NewFromInt(120))
	signal := makeCloseSignal(99.5, 99.0, 100.0)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "mark_to_market", trade.ExitReason)
	assertPriceEqual(t, decimal.NewFromFloat(99.5), trade.ExitPrice)
}

func TestCloseSimulatedPosition_SellMarkPriceTriggersMaxLoss(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("sell", decimal.NewFromInt(100), decimal.Zero, decimal.NewFromInt(80))
	signal := makeCloseSignal(102.0, 99.5, 102.5)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "max_loss", trade.ExitReason)
	assertPriceEqual(t, decimal.NewFromInt(102), trade.ExitPrice, "markPrice (102) is above maxLossPrice (101.5), exit at markPrice")
}

func TestCloseSimulatedPosition_SellMarkPriceTriggersStopLossWhenTighter(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("sell", decimal.NewFromInt(100), decimal.NewFromFloat(101.0), decimal.NewFromInt(80))
	signal := makeCloseSignal(101.2, 100.0, 101.3)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "stop_loss", trade.ExitReason, "markPrice=101.2 is in (SL=101.0, max_loss=101.5), so SL fires")
	assertPriceEqual(t, decimal.NewFromFloat(101.2), trade.ExitPrice, "markPrice (101.2) is above SL (101.0), exit at markPrice")
}

func TestCloseSimulatedPosition_BuyTakeProfitUsesMarkPrice(t *testing.T) {
	engine := newCloseTestEngine()
	position := makeCloseTestPosition("buy", decimal.NewFromInt(100), decimal.NewFromFloat(95.0), decimal.NewFromInt(107))
	signal := makeCloseSignal(107.5, 106.0, 108.0)

	trade := engine.closeSimulatedPosition(signal, position)

	assert.Equal(t, "take_profit", trade.ExitReason)
	assertPriceEqual(t, decimal.NewFromInt(107), trade.ExitPrice, "exit at TP even when markPrice is above")
}
