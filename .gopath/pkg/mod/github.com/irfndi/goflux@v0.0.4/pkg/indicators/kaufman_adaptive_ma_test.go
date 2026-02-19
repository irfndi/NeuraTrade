package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestKAMA(t *testing.T) {
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

	kama := NewKAMAIndicator(s, 14)
	result := kama.Calculate(49)

	if result.IsZero() {
		t.Errorf("KAMA() should not be zero")
	}
}

func TestKAMAInsufficientData(t *testing.T) {
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

	kama := NewKAMAIndicator(s, 14)
	result := kama.Calculate(9)

	if result.IsZero() {
		t.Errorf("KAMA() should return close price for insufficient data")
	}
}

func TestKAMAAdaptive(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 30; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(100),
			MaxPrice:   decimal.New(105),
			MinPrice:   decimal.New(95),
			ClosePrice: decimal.New(100),
			Volume:     decimal.New(1000),
		})
	}

	for i := 30; i < 50; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i - 30)),
			MaxPrice:   decimal.New(float64(105 + i - 30)),
			MinPrice:   decimal.New(float64(95 + i - 30)),
			ClosePrice: decimal.New(float64(100 + i - 30)),
			Volume:     decimal.New(1000),
		})
	}

	kama := NewKAMAIndicator(s, 14)
	result := kama.Calculate(49)

	if result.LT(decimal.New(110)) {
		t.Errorf("KAMA should adapt to upward trend, got %v", result)
	}
}

func TestDEMA(t *testing.T) {
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

	dema := NewDEMAIndicator(s, 14)
	result := dema.Calculate(49)

	if result.IsZero() {
		t.Errorf("DEMA() should not be zero")
	}
}

func TestDEMAInsufficientData(t *testing.T) {
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

	dema := NewDEMAIndicator(s, 14)
	result := dema.Calculate(9)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("DEMA() should return zero for insufficient data, got %v", result)
	}
}

func TestDEMALessLagThanSMA(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 30; i++ {
		price := float64(100)
		if i >= 15 {
			price = float64(120)
		}
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(price),
			MaxPrice:   decimal.New(price + 5),
			MinPrice:   decimal.New(price - 5),
			ClosePrice: decimal.New(price),
			Volume:     decimal.New(1000),
		})
	}

	dema := NewDEMAIndicator(s, 14)
	sma := NewSimpleMovingAverage(NewClosePriceIndicator(s), 14)

	demaValue := dema.Calculate(29)
	smaValue := sma.Calculate(29)

	if demaValue.LT(smaValue) {
		t.Errorf("DEMA should respond faster to trend changes than SMA, DEMA: %v, SMA: %v", demaValue, smaValue)
	}
}

func TestTEMA(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 60; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(100 + i)),
			Volume:     decimal.New(1000),
		})
	}

	tema := NewTEMAIndicator(s, 14)
	result := tema.Calculate(59)

	if result.IsZero() {
		t.Errorf("TEMA() should not be zero")
	}
}

func TestTEMAInsufficientData(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 20; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(100),
			MaxPrice:   decimal.New(105),
			MinPrice:   decimal.New(95),
			ClosePrice: decimal.New(100),
			Volume:     decimal.New(1000),
		})
	}

	tema := NewTEMAIndicator(s, 14)
	result := tema.Calculate(5)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("TEMA() should return zero for insufficient data, got %v", result)
	}
}

func TestTEMAMoreLagReduction(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 40; i++ {
		price := float64(100)
		if i >= 20 {
			price = float64(130)
		}
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(price),
			MaxPrice:   decimal.New(price + 5),
			MinPrice:   decimal.New(price - 5),
			ClosePrice: decimal.New(price),
			Volume:     decimal.New(1000),
		})
	}

	tema := NewTEMAIndicator(s, 14)
	sma := NewSimpleMovingAverage(NewClosePriceIndicator(s), 14)

	temaValue := tema.Calculate(39)
	smaValue := sma.Calculate(39)

	if temaValue.LT(smaValue) {
		t.Errorf("TEMA should respond faster to trend changes than SMA, TEMA: %v, SMA: %v", temaValue, smaValue)
	}
}
