package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestMFI(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 20; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(102 + i)),
			Volume:     decimal.New(1000),
		})
	}

	mfi := NewMFIIndicator(s, 14)
	result := mfi.Calculate(19)

	if result.LT(decimal.ZERO) || result.GT(decimal.New(100)) {
		t.Errorf("MFI() should be between 0 and 100, got %v", result)
	}
}

func TestMFIInsufficientData(t *testing.T) {
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

	mfi := NewMFIIndicator(s, 14)
	result := mfi.Calculate(9)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("MFI() should return ZERO for insufficient data, got %v", result)
	}
}

func TestMFIOverbought(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 15; i++ {
		increasingClose := decimal.New(float64(100 + i*2))
		s.AddCandle(&series.Candle{
			OpenPrice:  increasingClose.Sub(decimal.New(2)),
			MaxPrice:   increasingClose.Add(decimal.New(5)),
			MinPrice:   increasingClose.Sub(decimal.New(5)),
			ClosePrice: increasingClose,
			Volume:     decimal.New(1000),
		})
	}

	mfi := NewMFIIndicator(s, 14)
	result := mfi.Calculate(14)

	if result.LT(decimal.New(80)) {
		t.Errorf("MFI() should be above 80 for overbought conditions, got %v", result)
	}
}

func TestMFIOversold(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 15; i++ {
		decreasingClose := decimal.New(float64(130 - i*2))
		s.AddCandle(&series.Candle{
			OpenPrice:  decreasingClose.Add(decimal.New(2)),
			MaxPrice:   decreasingClose.Add(decimal.New(5)),
			MinPrice:   decreasingClose.Sub(decimal.New(5)),
			ClosePrice: decreasingClose,
			Volume:     decimal.New(1000),
		})
	}

	mfi := NewMFIIndicator(s, 14)
	result := mfi.Calculate(14)

	if result.GT(decimal.New(20)) {
		t.Errorf("MFI() should be below 20 for oversold conditions, got %v", result)
	}
}
