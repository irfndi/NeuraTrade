package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestADX(t *testing.T) {
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

	adx := NewADXIndicator(s, 14)
	result := adx.Calculate(29)

	if result.IsNegative() {
		t.Errorf("ADX() should not be negative, got %v", result)
	}
}

func TestADXInsufficientData(t *testing.T) {
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

	adx := NewADXIndicator(s, 14)
	result := adx.Calculate(9)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("ADX() should return ZERO for insufficient data, got %v", result)
	}
}

func TestADXTrending(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 50; i++ {
		upward := decimal.New(float64(100 + i))
		s.AddCandle(&series.Candle{
			OpenPrice:  upward.Sub(decimal.New(2)),
			MaxPrice:   upward.Add(decimal.New(5)),
			MinPrice:   upward.Sub(decimal.New(5)),
			ClosePrice: upward,
			Volume:     decimal.New(1000),
		})
	}

	adx := NewADXIndicator(s, 14)
	result := adx.Calculate(49)

	if result.LT(decimal.New(25)) {
		t.Errorf("ADX() should be above 25 in strong trend, got %v", result)
	}
}
