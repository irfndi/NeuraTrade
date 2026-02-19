package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestOBV(t *testing.T) {
	s := series.NewTimeSeries()

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(100),
		MaxPrice:   decimal.New(105),
		MinPrice:   decimal.New(95),
		ClosePrice: decimal.New(102),
		Volume:     decimal.New(1000),
	})

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(102),
		MaxPrice:   decimal.New(108),
		MinPrice:   decimal.New(101),
		ClosePrice: decimal.New(105),
		Volume:     decimal.New(1500),
	})

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(105),
		MaxPrice:   decimal.New(107),
		MinPrice:   decimal.New(103),
		ClosePrice: decimal.New(104),
		Volume:     decimal.New(1200),
	})

	obv := NewOBVIndicator(s)

	tests := []struct {
		name   string
		index  int
		expect decimal.Decimal
	}{
		{
			name:   "first bar - volume only",
			index:  0,
			expect: decimal.New(1000),
		},
		{
			name:   "up bar - add volume",
			index:  1,
			expect: decimal.New(2500),
		},
		{
			name:   "down bar - subtract volume",
			index:  2,
			expect: decimal.New(1300),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := obv.Calculate(tt.index)
			if !result.EQ(tt.expect) {
				t.Errorf("OBV() = %v, want %v", result, tt.expect)
			}
		})
	}
}

func TestOBVContinuation(t *testing.T) {
	s := series.NewTimeSeries()

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(100),
		MaxPrice:   decimal.New(105),
		MinPrice:   decimal.New(95),
		ClosePrice: decimal.New(102),
		Volume:     decimal.New(1000),
	})

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(102),
		MaxPrice:   decimal.New(108),
		MinPrice:   decimal.New(101),
		ClosePrice: decimal.New(103),
		Volume:     decimal.New(1500),
	})

	obv := NewOBVIndicator(s)
	result := obv.Calculate(1)

	if result.LT(decimal.New(2500)) {
		t.Errorf("OBV should accumulate on continuation, got %v", result)
	}
}

func TestOBVAllDown(t *testing.T) {
	s := series.NewTimeSeries()

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(100),
		MaxPrice:   decimal.New(105),
		MinPrice:   decimal.New(95),
		ClosePrice: decimal.New(98),
		Volume:     decimal.New(1000),
	})

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(98),
		MaxPrice:   decimal.New(100),
		MinPrice:   decimal.New(95),
		ClosePrice: decimal.New(96),
		Volume:     decimal.New(1500),
	})

	s.AddCandle(&series.Candle{
		OpenPrice:  decimal.New(96),
		MaxPrice:   decimal.New(98),
		MinPrice:   decimal.New(94),
		ClosePrice: decimal.New(95),
		Volume:     decimal.New(1200),
	})

	obv := NewOBVIndicator(s)
	result := obv.Calculate(2)

	if result.GT(decimal.ZERO) {
		t.Errorf("OBV should decrease when price falls, got %v", result)
	}
}
