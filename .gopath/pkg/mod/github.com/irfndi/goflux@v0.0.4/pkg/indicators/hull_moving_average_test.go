package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestHMA(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 30; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(100 + i)),
			Volume:     decimal.New(1000),
		})
	}

	hma := NewHMAIndicator(NewClosePriceIndicator(s), 14)
	result := hma.Calculate(29)

	if result.IsZero() {
		t.Errorf("HMA() should not be zero")
	}
}

func TestHMAInsufficientData(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 10; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(100),
			MaxPrice:   decimal.New(105),
			MinPrice:   decimal.New(95),
			ClosePrice: decimal.New(100),
			Volume:     decimal.New(1000),
		})
	}

	hma := NewHMAIndicator(NewClosePriceIndicator(s), 14)
	result := hma.Calculate(9)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("HMA() should return ZERO for insufficient data, got %v", result)
	}
}

func TestWMALinearUpward(t *testing.T) {
	s := series.NewTimeSeries()

	prices := []float64{100, 102, 104, 106, 108}
	for _, price := range prices {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(price),
			MaxPrice:   decimal.New(price + 5),
			MinPrice:   decimal.New(price - 5),
			ClosePrice: decimal.New(price),
			Volume:     decimal.New(1000),
		})
	}

	wma := NewWMAIndicator(NewClosePriceIndicator(s), 5)
	result := wma.Calculate(4)

	if result.LT(decimal.New(105)) {
		t.Errorf("WMA should be weighted towards recent higher prices, got %v", result)
	}
}

func TestWMAInsufficientData(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 3; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(100),
			MaxPrice:   decimal.New(105),
			MinPrice:   decimal.New(95),
			ClosePrice: decimal.New(100),
			Volume:     decimal.New(1000),
		})
	}

	wma := NewWMAIndicator(NewClosePriceIndicator(s), 5)
	result := wma.Calculate(2)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("WMA() should return ZERO for insufficient data, got %v", result)
	}
}

func TestHMAVsSMA(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 50; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(100 + i)),
			Volume:     decimal.New(1000),
		})
	}

	hma := NewHMAIndicator(NewClosePriceIndicator(s), 14)
	sma := NewSimpleMovingAverage(NewClosePriceIndicator(s), 14)

	hmaValue := hma.Calculate(49)
	smaValue := sma.Calculate(49)

	t.Logf("HMA: %v, SMA: %v", hmaValue, smaValue)
}
