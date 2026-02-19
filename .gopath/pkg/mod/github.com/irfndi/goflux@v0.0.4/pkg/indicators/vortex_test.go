package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestVortex(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 30; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(102 + i)),
			Volume:     decimal.New(1000),
		})
	}

	vortex := NewVortexIndicator(s, 14)
	result := vortex.Calculate(29)

	if result.IsZero() {
		t.Errorf("Vortex() should not be zero")
	}
}

func TestVortexInsufficientData(t *testing.T) {
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

	vortex := NewVortexIndicator(s, 14)
	result := vortex.Calculate(9)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("Vortex() should return ZERO for insufficient data, got %v", result)
	}
}

func TestVortexPositiveTrend(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 30; i++ {
		upward := decimal.New(float64(100 + i))
		s.AddCandle(&series.Candle{
			OpenPrice:  upward.Sub(decimal.New(2)),
			MaxPrice:   upward.Add(decimal.New(5)),
			MinPrice:   upward.Sub(decimal.New(3)),
			ClosePrice: upward,
			Volume:     decimal.New(1000),
		})
	}

	vortex := NewVortexIndicator(s, 14)
	result := vortex.Calculate(29)

	if result.LT(decimal.ZERO) {
		t.Errorf("Vortex() should be positive in uptrend, got %v", result)
	}
}

func TestVortexNegativeTrend(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 30; i++ {
		downward := decimal.New(float64(130 - i))
		s.AddCandle(&series.Candle{
			OpenPrice:  downward.Add(decimal.New(2)),
			MaxPrice:   downward.Add(decimal.New(5)),
			MinPrice:   downward.Sub(decimal.New(3)),
			ClosePrice: downward,
			Volume:     decimal.New(1000),
		})
	}

	vortex := NewVortexIndicator(s, 14)
	result := vortex.Calculate(29)

	if result.GT(decimal.ZERO) {
		t.Errorf("Vortex() should be negative in downtrend, got %v", result)
	}
}
