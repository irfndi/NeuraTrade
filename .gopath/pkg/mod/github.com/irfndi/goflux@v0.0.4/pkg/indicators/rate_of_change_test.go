package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestROC(t *testing.T) {
	s := series.NewTimeSeries()

	prices := []float64{100, 102, 105, 103, 108, 110, 107, 112, 115, 113}
	for _, price := range prices {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(price),
			MaxPrice:   decimal.New(price + 2),
			MinPrice:   decimal.New(price - 2),
			ClosePrice: decimal.New(price),
			Volume:     decimal.New(1000),
		})
	}

	roc := NewROCIndicator(s, 5)

	tests := []struct {
		name   string
		index  int
		expect decimal.Decimal
	}{
		{
			name:   "insufficient data",
			index:  3,
			expect: decimal.ZERO,
		},
		{
			name:   "positive ROC",
			index:  9,
			expect: decimal.New(4.6296),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := roc.Calculate(tt.index)
			diff := result.Sub(tt.expect).Abs()
			if diff.GT(decimal.New(0.1)) {
				t.Errorf("ROC() = %v, want %v", result, tt.expect)
			}
		})
	}
}

func TestMomentum(t *testing.T) {
	s := series.NewTimeSeries()

	prices := []float64{100, 102, 105, 103, 108, 110, 107, 112, 115, 113}
	for _, price := range prices {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(price),
			MaxPrice:   decimal.New(price + 2),
			MinPrice:   decimal.New(price - 2),
			ClosePrice: decimal.New(price),
			Volume:     decimal.New(1000),
		})
	}

	mom := NewMomentumIndicator(s, 5)

	tests := []struct {
		name   string
		index  int
		expect decimal.Decimal
	}{
		{
			name:   "insufficient data",
			index:  3,
			expect: decimal.ZERO,
		},
		{
			name:   "positive momentum",
			index:  9,
			expect: decimal.New(5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mom.Calculate(tt.index)
			diff := result.Sub(tt.expect).Abs()
			if diff.GT(decimal.New(0.1)) {
				t.Errorf("Momentum() = %v, want %v", result, tt.expect)
			}
		})
	}
}

func TestROCNegative(t *testing.T) {
	s := series.NewTimeSeries()

	prices := []float64{100, 105, 110, 108, 105, 100, 95, 90}
	for _, price := range prices {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(price),
			MaxPrice:   decimal.New(price + 2),
			MinPrice:   decimal.New(price - 2),
			ClosePrice: decimal.New(price),
			Volume:     decimal.New(1000),
		})
	}

	roc := NewROCIndicator(s, 5)
	result := roc.Calculate(7)

	if !result.IsNegative() {
		t.Errorf("ROC() should be negative for declining prices, got %v", result)
	}
}
