package talib

import (
	"sync"

	"github.com/cinar/indicator/v2/helper"
	"github.com/cinar/indicator/v2/momentum"
	"github.com/cinar/indicator/v2/trend"
	"github.com/cinar/indicator/v2/volatility"
	"github.com/cinar/indicator/v2/volume"
)

const (
	SMA = 0
	EMA = 1
)

func Sma(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}
	c := helper.SliceToChan(prices)
	sma := trend.NewSmaWithPeriod[float64](period)
	return helper.ChanToSlice(sma.Compute(c))
}

func Ema(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}
	c := helper.SliceToChan(prices)
	ema := trend.NewEmaWithPeriod[float64](period)
	return helper.ChanToSlice(ema.Compute(c))
}

func Rsi(prices []float64, period int) []float64 {
	if len(prices) < period+1 {
		return nil
	}
	c := helper.SliceToChan(prices)
	rsi := momentum.NewRsiWithPeriod[float64](period)
	return helper.ChanToSlice(rsi.Compute(c))
}

func Macd(prices []float64, fastPeriod, slowPeriod, signalPeriod int) ([]float64, []float64, []float64) {
	if len(prices) < slowPeriod {
		return nil, nil, nil
	}
	c := helper.SliceToChan(prices)
	macd := trend.NewMacdWithPeriod[float64](fastPeriod, slowPeriod, signalPeriod)
	macdLine, signal := macd.Compute(c)

	var (
		macdValues   []float64
		signalValues []float64
		wg           sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		macdValues = helper.ChanToSlice(macdLine)
	}()
	go func() {
		defer wg.Done()
		signalValues = helper.ChanToSlice(signal)
	}()
	wg.Wait()

	histogramLength := len(macdValues)
	if len(signalValues) < histogramLength {
		histogramLength = len(signalValues)
	}

	histogram := make([]float64, histogramLength)
	for i := range histogramLength {
		histogram[i] = macdValues[i] - signalValues[i]
	}

	return macdValues, signalValues, histogram
}

func BBands(prices []float64, period int, _, _ float64, _ int) ([]float64, []float64, []float64) {
	if len(prices) < period {
		return nil, nil, nil
	}
	c := helper.SliceToChan(prices)
	bb := volatility.NewBollingerBandsWithPeriod[float64](period)
	upper, middle, lower := bb.Compute(c)

	var (
		upperValues  []float64
		middleValues []float64
		lowerValues  []float64
		wg           sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		upperValues = helper.ChanToSlice(upper)
	}()
	go func() {
		defer wg.Done()
		middleValues = helper.ChanToSlice(middle)
	}()
	go func() {
		defer wg.Done()
		lowerValues = helper.ChanToSlice(lower)
	}()
	wg.Wait()

	return upperValues, middleValues, lowerValues
}

func Atr(high, low, close []float64, period int) []float64 {
	if len(high) < period || len(low) < period || len(close) < period {
		return nil
	}
	h := helper.SliceToChan(high)
	l := helper.SliceToChan(low)
	c := helper.SliceToChan(close)
	atr := volatility.NewAtrWithPeriod[float64](period)
	return helper.ChanToSlice(atr.Compute(h, l, c))
}

func StochF(high, low, close []float64, kPeriod, _, _ int) ([]float64, []float64) {
	if len(high) < kPeriod || len(low) < kPeriod || len(close) < kPeriod {
		return nil, nil
	}
	h := helper.SliceToChan(high)
	l := helper.SliceToChan(low)
	c := helper.SliceToChan(close)
	stoch := momentum.NewStochasticOscillator[float64]()
	k, d := stoch.Compute(h, l, c)

	var (
		kValues []float64
		dValues []float64
		wg      sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		kValues = helper.ChanToSlice(k)
	}()
	go func() {
		defer wg.Done()
		dValues = helper.ChanToSlice(d)
	}()
	wg.Wait()

	return kValues, dValues
}

func Obv(prices, volumes []float64) []float64 {
	if len(prices) == 0 || len(volumes) == 0 {
		return nil
	}
	p := helper.SliceToChan(prices)
	v := helper.SliceToChan(volumes)
	obv := volume.NewObv[float64]()
	return helper.ChanToSlice(obv.Compute(p, v))
}
