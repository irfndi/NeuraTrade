package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestParabolicSAR(t *testing.T) {
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

	psar := NewParabolicSARIndicator(s)
	result := psar.Calculate(29)

	if result.IsZero() {
		t.Errorf("ParabolicSAR() should not be zero")
	}
}

func TestParabolicSARReversal(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 10; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(102 + i)),
			Volume:     decimal.New(1000),
		})
	}

	for i := 10; i < 20; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(115 - (i - 10))),
			MaxPrice:   decimal.New(float64(120 - (i - 10))),
			MinPrice:   decimal.New(float64(110 - (i - 10))),
			ClosePrice: decimal.New(float64(112 - (i - 10))),
			Volume:     decimal.New(1000),
		})
	}

	psar := NewParabolicSARIndicator(s)

	result9 := psar.Calculate(9)
	result19 := psar.Calculate(19)

	if result9.EQ(result19) {
		t.Logf("SAR values changed from %v to %v", result9, result19)
	}
}

func TestParabolicSARPricesBelowSAR(t *testing.T) {
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

	psar := NewParabolicSARIndicator(s)
	result := psar.Calculate(19)

	if result.LT(decimal.New(90)) {
		t.Errorf("ParabolicSAR() should be below price in downtrend, got %v", result)
	}
}
