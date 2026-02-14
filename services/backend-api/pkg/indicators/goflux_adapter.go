package indicators

import (
	"time"

	godecimal "github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
	"github.com/shopspring/decimal"
)

const (
	// gofluxVersion is the version of the GoFlux library being used
	gofluxVersion = "0.0.4"
)

// GoFluxAdapter implements IndicatorProvider using the GoFlux library.
// This provides a pure Go implementation of technical indicators without
// relying on the C-based TA-Lib library.
type GoFluxAdapter struct {
	name    string
	version string
}

// NewGoFluxAdapter creates a new GoFluxAdapter.
func NewGoFluxAdapter() *GoFluxAdapter {
	return &GoFluxAdapter{
		name:    "goflux",
		version: gofluxVersion,
	}
}

func (a *GoFluxAdapter) Name() string {
	return a.name
}

func (a *GoFluxAdapter) Version() string {
	return a.version
}

// SMA calculates Simple Moving Average using GoFlux.
func (a *GoFluxAdapter) SMA(prices []decimal.Decimal, period int) []decimal.Decimal {
	if len(prices) < period {
		return nil
	}

	ts := a.createTimeSeries(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	sma := indicators.NewSimpleMovingAverage(closePrice, period)

	return a.extractValues(ts.Candles, sma, period-1)
}

// EMA calculates Exponential Moving Average using GoFlux.
func (a *GoFluxAdapter) EMA(prices []decimal.Decimal, period int) []decimal.Decimal {
	if len(prices) < period {
		return nil
	}

	ts := a.createTimeSeries(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	ema := indicators.NewEMAIndicator(closePrice, period)

	return a.extractValues(ts.Candles, ema, period-1)
}

// RSI calculates Relative Strength Index using GoFlux.
func (a *GoFluxAdapter) RSI(prices []decimal.Decimal, period int) []decimal.Decimal {
	if len(prices) < period+1 {
		return nil
	}

	ts := a.createTimeSeries(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	rsi := indicators.NewRelativeStrengthIndexIndicator(closePrice, period)

	return a.extractValues(ts.Candles, rsi, period)
}

// Stochastic calculates Stochastic Oscillator using GoFlux.
func (a *GoFluxAdapter) Stochastic(high, low, close []decimal.Decimal, kPeriod, dPeriod int) (k, d []decimal.Decimal) {
	if len(high) < kPeriod || len(low) < kPeriod || len(close) < kPeriod {
		return nil, nil
	}

	ts := a.createOHLCTimeSeries(high, low, close)
	fastK := indicators.NewFastStochasticIndicator(ts, kPeriod)
	fastD := indicators.NewSimpleMovingAverage(fastK, dPeriod)

	k = a.extractValues(ts.Candles, fastK, kPeriod-1)
	d = a.extractValues(ts.Candles, fastD, kPeriod+dPeriod-2)

	// Ensure equal lengths
	minLen := len(k)
	if len(d) < minLen {
		minLen = len(d)
	}
	return k[:minLen], d[:minLen]
}

// MACD calculates Moving Average Convergence Divergence using GoFlux.
func (a *GoFluxAdapter) MACD(prices []decimal.Decimal, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram []decimal.Decimal) {
	if len(prices) < slowPeriod {
		return nil, nil, nil
	}

	ts := a.createTimeSeries(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	macdIndicator := indicators.NewMACDIndicator(closePrice, fastPeriod, slowPeriod)
	signalIndicator := indicators.NewEMAIndicator(macdIndicator, signalPeriod)
	histogramIndicator := indicators.NewDifferenceIndicator(macdIndicator, signalIndicator)

	offset := slowPeriod - 1
	macd = a.extractValues(ts.Candles, macdIndicator, offset)
	signal = a.extractValues(ts.Candles, signalIndicator, offset+signalPeriod-1)
	histogram = a.extractValues(ts.Candles, histogramIndicator, offset+signalPeriod-1)

	// Ensure equal lengths
	minLen := len(macd)
	if len(signal) < minLen {
		minLen = len(signal)
	}
	if len(histogram) < minLen {
		minLen = len(histogram)
	}
	return macd[:minLen], signal[:minLen], histogram[:minLen]
}

// BollingerBands calculates Bollinger Bands using GoFlux.
func (a *GoFluxAdapter) BollingerBands(prices []decimal.Decimal, period int, stdDev float64) (upper, middle, lower []decimal.Decimal) {
	if len(prices) < period {
		return nil, nil, nil
	}

	ts := a.createTimeSeries(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)

	upperInd := indicators.NewBollingerUpperBandIndicator(closePrice, period, stdDev)
	middleInd := indicators.NewSimpleMovingAverage(closePrice, period)
	lowerInd := indicators.NewBollingerLowerBandIndicator(closePrice, period, stdDev)

	offset := period - 1
	return a.extractValues(ts.Candles, upperInd, offset),
		a.extractValues(ts.Candles, middleInd, offset),
		a.extractValues(ts.Candles, lowerInd, offset)
}

// ATR calculates Average True Range using GoFlux.
func (a *GoFluxAdapter) ATR(high, low, close []decimal.Decimal, period int) []decimal.Decimal {
	if len(high) < period || len(low) < period || len(close) < period {
		return nil
	}

	ts := a.createOHLCTimeSeries(high, low, close)
	atr := indicators.NewAverageTrueRangeIndicator(ts, period)

	return a.extractValues(ts.Candles, atr, period-1)
}

// OBV calculates On-Balance Volume using GoFlux.
func (a *GoFluxAdapter) OBV(prices, volumes []decimal.Decimal) []decimal.Decimal {
	if len(prices) == 0 || len(volumes) == 0 || len(prices) != len(volumes) {
		return nil
	}

	ts := a.createVolumeTimeSeries(prices, volumes)
	obv := indicators.NewOBVIndicator(ts)

	return a.extractValues(ts.Candles, obv, 0)
}

// VWAP calculates Volume Weighted Average Price.
// This is calculated manually as GoFlux doesn't have a native VWAP implementation.
func (a *GoFluxAdapter) VWAP(high, low, close, volume []decimal.Decimal) []decimal.Decimal {
	n := len(close)
	if n == 0 || len(high) != n || len(low) != n || len(volume) != n {
		return nil
	}

	result := make([]decimal.Decimal, n)
	cumulativeTPV := decimal.Zero
	cumulativeVolume := decimal.Zero

	for i := 0; i < n; i++ {
		tp := high[i].Add(low[i]).Add(close[i]).Div(decimal.NewFromInt(3))
		tpv := tp.Mul(volume[i])
		cumulativeTPV = cumulativeTPV.Add(tpv)
		cumulativeVolume = cumulativeVolume.Add(volume[i])

		if !cumulativeVolume.IsZero() {
			result[i] = cumulativeTPV.Div(cumulativeVolume)
		}
	}

	return result
}

// Helper functions

// createTimeSeries creates a GoFlux TimeSeries from price data.
func (a *GoFluxAdapter) createTimeSeries(prices []decimal.Decimal) *series.TimeSeries {
	ts := series.NewTimeSeries()

	for i, price := range prices {
		// Use hourly candles with incremental timestamps
		period := series.NewTimePeriod(time.Unix(int64(i)*3600, 0), time.Hour)
		candle := series.NewCandle(period)
		candle.OpenPrice = toGoDecimal(price)
		candle.ClosePrice = toGoDecimal(price)
		candle.MaxPrice = toGoDecimal(price)
		candle.MinPrice = toGoDecimal(price)
		candle.Volume = godecimal.New(0)
		ts.AddCandle(candle)
	}

	return ts
}

// createOHLCTimeSeries creates a GoFlux TimeSeries from OHLC data.
func (a *GoFluxAdapter) createOHLCTimeSeries(high, low, close []decimal.Decimal) *series.TimeSeries {
	ts := series.NewTimeSeries()

	for i := range close {
		period := series.NewTimePeriod(time.Unix(int64(i)*3600, 0), time.Hour)
		candle := series.NewCandle(period)
		candle.OpenPrice = toGoDecimal(close[i])
		candle.ClosePrice = toGoDecimal(close[i])
		candle.MaxPrice = toGoDecimal(high[i])
		candle.MinPrice = toGoDecimal(low[i])
		candle.Volume = godecimal.New(0)
		ts.AddCandle(candle)
	}

	return ts
}

// createVolumeTimeSeries creates a GoFlux TimeSeries from price and volume data.
func (a *GoFluxAdapter) createVolumeTimeSeries(prices, volumes []decimal.Decimal) *series.TimeSeries {
	ts := series.NewTimeSeries()

	for i, price := range prices {
		period := series.NewTimePeriod(time.Unix(int64(i)*3600, 0), time.Hour)
		candle := series.NewCandle(period)
		candle.OpenPrice = toGoDecimal(price)
		candle.ClosePrice = toGoDecimal(price)
		candle.MaxPrice = toGoDecimal(price)
		candle.MinPrice = toGoDecimal(price)
		candle.Volume = toGoDecimal(volumes[i])
		ts.AddCandle(candle)
	}

	return ts
}

// extractValues extracts decimal values from an indicator.
func (a *GoFluxAdapter) extractValues(candles []*series.Candle, indicator indicators.Indicator, startOffset int) []decimal.Decimal {
	if len(candles) == 0 {
		return nil
	}

	if startOffset >= len(candles) {
		return nil
	}

	values := make([]decimal.Decimal, 0, len(candles)-startOffset)
	for i := startOffset; i < len(candles); i++ {
		val := indicator.Calculate(i)
		values = append(values, fromGoDecimal(val))
	}

	return values
}

// toGoDecimal converts shopspring decimal to goflux decimal.
func toGoDecimal(d decimal.Decimal) godecimal.Decimal {
	result := godecimal.NewFromString(d.String())
	return result
}

// fromGoDecimal converts goflux decimal to shopspring decimal using string-based conversion for precision.
func fromGoDecimal(d godecimal.Decimal) decimal.Decimal {
	result, err := decimal.NewFromString(d.String())
	if err != nil {
		return decimal.Zero
	}
	return result
}
