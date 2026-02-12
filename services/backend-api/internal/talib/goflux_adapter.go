package talib

import (
	"time"

	godecimal "github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

const (
	SMA = 0
	EMA = 1
)

var baseTimestamp = time.Unix(0, 0)

func Sma(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}

	ts := createSeriesFromPrices(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	sma := indicators.NewSimpleMovingAverage(closePrice, period)

	return extractIndicatorValues(ts.Candles, sma, period-1)
}

func Ema(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}

	ts := createSeriesFromPrices(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	ema := indicators.NewEMAIndicator(closePrice, period)

	return extractIndicatorValues(ts.Candles, ema, period-1)
}

func Rsi(prices []float64, period int) []float64 {
	if len(prices) < period+1 {
		return nil
	}

	ts := createSeriesFromPrices(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	rsi := indicators.NewRelativeStrengthIndexIndicator(closePrice, period)

	return extractIndicatorValues(ts.Candles, rsi, period)
}

func Macd(prices []float64, fastPeriod, slowPeriod, signalPeriod int) ([]float64, []float64, []float64) {
	if len(prices) < slowPeriod {
		return nil, nil, nil
	}

	ts := createSeriesFromPrices(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)
	macd := indicators.NewMACDIndicator(closePrice, fastPeriod, slowPeriod)
	signal := indicators.NewEMAIndicator(macd, signalPeriod)
	histogram := indicators.NewDifferenceIndicator(macd, signal)

	offset := slowPeriod - 1
	macdValues := extractIndicatorValues(ts.Candles, macd, offset)
	signalValues := extractIndicatorValues(ts.Candles, signal, offset+signalPeriod-1)
	histogramValues := extractIndicatorValues(ts.Candles, histogram, offset+signalPeriod-1)

	minLen := len(macdValues)
	if len(signalValues) < minLen {
		minLen = len(signalValues)
	}
	if len(histogramValues) < minLen {
		minLen = len(histogramValues)
	}

	return macdValues[:minLen], signalValues[:minLen], histogramValues[:minLen]
}

func BBands(prices []float64, period int, stdDevUp, stdDevDown float64, _ int) ([]float64, []float64, []float64) {
	if len(prices) < period {
		return nil, nil, nil
	}

	ts := createSeriesFromPrices(prices)
	closePrice := indicators.NewClosePriceIndicator(ts)

	upper := indicators.NewBollingerUpperBandIndicator(closePrice, period, stdDevUp)
	middle := indicators.NewSimpleMovingAverage(closePrice, period)
	lower := indicators.NewBollingerLowerBandIndicator(closePrice, period, stdDevDown)

	offset := period - 1
	return extractIndicatorValues(ts.Candles, upper, offset),
		extractIndicatorValues(ts.Candles, middle, offset),
		extractIndicatorValues(ts.Candles, lower, offset)
}

func Atr(high, low, close []float64, period int) []float64 {
	if len(high) < period || len(low) < period || len(close) < period {
		return nil
	}

	ts := createSeriesFromOHLC(high, low, close)
	atr := indicators.NewAverageTrueRangeIndicator(ts, period)

	return extractIndicatorValues(ts.Candles, atr, period-1)
}

func StochF(high, low, close []float64, kPeriod, dPeriod, _ int) ([]float64, []float64) {
	if len(high) < kPeriod || len(low) < kPeriod || len(close) < kPeriod {
		return nil, nil
	}

	ts := createSeriesFromOHLC(high, low, close)
	fastK := indicators.NewFastStochasticIndicator(ts, kPeriod)
	fastD := indicators.NewSimpleMovingAverage(fastK, dPeriod)

	offset := kPeriod - 1
	kValues := extractIndicatorValues(ts.Candles, fastK, offset)
	dValues := extractIndicatorValues(ts.Candles, fastD, offset+dPeriod-1)

	minLen := len(kValues)
	if len(dValues) < minLen {
		minLen = len(dValues)
	}

	return kValues[:minLen], dValues[:minLen]
}

func Obv(prices, volumes []float64) []float64 {
	if len(prices) == 0 || len(volumes) == 0 || len(prices) != len(volumes) {
		return nil
	}

	ts := createSeriesFromPricesAndVolume(prices, volumes)
	obv := indicators.NewOBVIndicator(ts)

	return extractIndicatorValues(ts.Candles, obv, 0)
}

func createSeriesFromPrices(prices []float64) *series.TimeSeries {
	ts := series.NewTimeSeries()

	for i, price := range prices {
		period := series.NewTimePeriod(baseTimestamp.Add(time.Duration(i)*time.Hour), time.Hour)
		candle := series.NewCandle(period)
		candle.OpenPrice = godecimal.New(price)
		candle.ClosePrice = godecimal.New(price)
		candle.MaxPrice = godecimal.New(price)
		candle.MinPrice = godecimal.New(price)
		candle.Volume = godecimal.New(0)
		ts.AddCandle(candle)
	}

	return ts
}

func createSeriesFromOHLC(high, low, close []float64) *series.TimeSeries {
	ts := series.NewTimeSeries()

	for i := range close {
		period := series.NewTimePeriod(baseTimestamp.Add(time.Duration(i)*time.Hour), time.Hour)
		candle := series.NewCandle(period)
		candle.OpenPrice = godecimal.New(close[i])
		candle.ClosePrice = godecimal.New(close[i])
		candle.MaxPrice = godecimal.New(high[i])
		candle.MinPrice = godecimal.New(low[i])
		candle.Volume = godecimal.New(0)
		ts.AddCandle(candle)
	}

	return ts
}

func createSeriesFromPricesAndVolume(prices, volumes []float64) *series.TimeSeries {
	ts := series.NewTimeSeries()

	for i, price := range prices {
		period := series.NewTimePeriod(baseTimestamp.Add(time.Duration(i)*time.Hour), time.Hour)
		candle := series.NewCandle(period)
		candle.OpenPrice = godecimal.New(price)
		candle.ClosePrice = godecimal.New(price)
		candle.MaxPrice = godecimal.New(price)
		candle.MinPrice = godecimal.New(price)
		candle.Volume = godecimal.New(volumes[i])
		ts.AddCandle(candle)
	}

	return ts
}

func extractIndicatorValues(candles []*series.Candle, indicator indicators.Indicator, startOffset int) []float64 {
	if len(candles) == 0 {
		return nil
	}

	if startOffset >= len(candles) {
		return nil
	}

	capacity := len(candles) - startOffset
	if capacity < 0 {
		capacity = 0
	}

	values := make([]float64, 0, capacity)
	for i := startOffset; i < len(candles); i++ {
		val := indicator.Calculate(i)
		values = append(values, val.Float())
	}

	return values
}
