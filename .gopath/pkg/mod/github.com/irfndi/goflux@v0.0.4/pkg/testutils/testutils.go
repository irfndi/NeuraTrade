package testutils

import (
	"fmt"
	"math"
	mrand "math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

var MockedTimeSeries = MockTimeSeriesFl(
	64.75, 63.79, 63.73,
	63.73, 63.55, 63.19,
	63.91, 63.85, 62.95,
	63.37, 61.33, 61.51)

type Indicator interface {
	Calculate(int) decimal.Decimal
}

func RandomTimeSeries(size int) *series.TimeSeries {
	vals := make([]string, size)
	rng := mrand.New(mrand.NewSource(time.Now().UnixNano())) //nolint:gosec // Use of weak random is acceptable for test utilities
	for i := 0; i < size; i++ {
		val := rng.Float64() * 100
		if i == 0 {
			vals[i] = fmt.Sprint(val)
		} else {
			last, _ := strconv.ParseFloat(vals[i-1], 64)
			if i%2 == 0 {
				vals[i] = fmt.Sprint(last + (val / 10))
			} else {
				vals[i] = fmt.Sprint(last - (val / 10))
			}
		}
	}

	return MockTimeSeries(vals...)
}

func MockTimeSeriesOCHL(values ...[]float64) *series.TimeSeries {
	ts := series.NewTimeSeries()
	for i, ochl := range values {
		candle := series.NewCandle(series.NewTimePeriod(time.Unix(int64(i), 0), time.Second))
		candle.OpenPrice = decimal.New(ochl[0])
		candle.ClosePrice = decimal.New(ochl[1])
		candle.MaxPrice = decimal.New(ochl[2])
		candle.MinPrice = decimal.New(ochl[3])
		candle.Volume = decimal.New(float64(i))

		ts.AddCandle(candle)
	}

	return ts
}

func MockTimeSeries(values ...string) *series.TimeSeries {
	ts := series.NewTimeSeries()
	for i, val := range values {
		candle := series.NewCandle(series.NewTimePeriod(time.Unix(int64(i), 0), time.Second))
		candle.OpenPrice = decimal.NewFromString(val)
		candle.ClosePrice = decimal.NewFromString(val)
		candle.MaxPrice = decimal.NewFromString(val).Add(decimal.ONE)
		candle.MinPrice = decimal.NewFromString(val).Sub(decimal.ONE)
		candle.Volume = decimal.NewFromString(val)

		ts.AddCandle(candle)
	}

	return ts
}

func MockTimeSeriesFl(values ...float64) *series.TimeSeries {
	strVals := make([]string, len(values))

	for i, val := range values {
		strVals[i] = fmt.Sprint(val)
	}

	return MockTimeSeries(strVals...)
}

func DecimalEquals(t *testing.T, expected float64, actual decimal.Decimal) {
	assert.Equal(t, fmt.Sprintf("%.4f", expected), fmt.Sprintf("%.4f", actual.Float()))
}

func Dump(indicator Indicator) (values []float64) {
	precision := 4.0
	m := math.Pow(10, precision)

	for index := 0; ; index++ {
		val, ok := safeCalculate(indicator, index)
		if !ok {
			break
		}
		values = append(values, math.Round(val.Float()*m)/m)
	}

	return
}

func safeCalculate(indicator Indicator, index int) (value decimal.Decimal, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	return indicator.Calculate(index), true
}

func IndicatorEquals(t *testing.T, expected []float64, indicator Indicator) {
	actualValues := Dump(indicator)
	assert.EqualValues(t, expected, actualValues)
}
