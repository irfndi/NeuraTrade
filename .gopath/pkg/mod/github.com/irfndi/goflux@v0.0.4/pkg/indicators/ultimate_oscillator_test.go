package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestUltimateOscillator(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 30; i++ {
		open := decimal.New(float64(100 + i))
		high := decimal.New(float64(105 + i))
		low := decimal.New(float64(95 + i))
		close := decimal.New(float64(102 + i))

		s.AddCandle(&series.Candle{
			OpenPrice:  open,
			MaxPrice:   high,
			MinPrice:   low,
			ClosePrice: close,
			Volume:     decimal.New(1000),
		})
	}

	uo := NewUltimateOscillatorIndicator(s, 7, 14, 28)

	result := uo.Calculate(27)

	if result.LT(decimal.ZERO) || result.GT(decimal.New(100)) {
		t.Errorf("UltimateOscillator() should be between 0 and 100, got %v", result)
	}
}

func TestUltimateOscillatorInsufficientData(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 10; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100)),
			MaxPrice:   decimal.New(float64(105)),
			MinPrice:   decimal.New(float64(95)),
			ClosePrice: decimal.New(float64(100)),
			Volume:     decimal.New(1000),
		})
	}

	uo := NewUltimateOscillatorIndicator(s, 7, 14, 28)
	result := uo.Calculate(9)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("UltimateOscillator() should return ZERO for insufficient data, got %v", result)
	}
}

func TestUltimateOscillatorRange(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 50; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(100),
			MaxPrice:   decimal.New(110),
			MinPrice:   decimal.New(90),
			ClosePrice: decimal.New(105),
			Volume:     decimal.New(1000),
		})
	}

	uo := NewUltimateOscillatorIndicator(s, 7, 14, 28)
	result := uo.Calculate(49)

	if result.LT(decimal.New(50)) {
		t.Errorf("UltimateOscillator() should be above 50 for bullish conditions, got %v", result)
	}
}
