package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestWilliamsR(t *testing.T) {
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

	williamsR := NewWilliamsRIndicator(s, 14)

	tests := []struct {
		name   string
		index  int
		expect decimal.Decimal
	}{
		{
			name:   "insufficient data",
			index:  5,
			expect: decimal.ZERO,
		},
		{
			name:   "valid calculation at index 13",
			index:  13,
			expect: decimal.New(-13.0435),
		},
		{
			name:   "valid calculation at index 19",
			index:  19,
			expect: decimal.New(-13.0435),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := williamsR.Calculate(tt.index)
			if result.LT(tt.expect.Sub(decimal.New(1))) || result.GT(tt.expect.Add(decimal.New(1))) {
				t.Errorf("WilliamsR() = %v, want %v", result, tt.expect)
			}
		})
	}
}

func TestWilliamsROverboughtOversold(t *testing.T) {
	s := series.NewTimeSeries()

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(100),
		MaxPrice:   decimal.New(100),
		MinPrice:   decimal.New(100),
		ClosePrice: decimal.New(100),
		Volume:     decimal.New(1000),
	})

	for i := 1; i <= 14; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(110 + i)),
			MinPrice:   decimal.New(float64(90 + i)),
			ClosePrice: decimal.New(float64(105 + i)),
			Volume:     decimal.New(1000),
		})
	}

	williamsR := NewWilliamsRIndicator(s, 14)
	result := williamsR.Calculate(14)

	if !result.LT(decimal.ZERO) {
		t.Errorf("WilliamsR() should be negative, got %v", result)
	}

	if result.GT(decimal.New(-10)) {
		t.Errorf("WilliamsR() should be below -10 for overbought, got %v", result)
	}
}
