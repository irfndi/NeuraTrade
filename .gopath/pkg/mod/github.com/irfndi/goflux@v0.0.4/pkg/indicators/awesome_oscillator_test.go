package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestAwesomeOscillator(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 50; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(102 + i)),
			Volume:     decimal.New(1000),
		})
	}

	ao := NewDefaultAwesomeOscillatorIndicator(s)
	result := ao.Calculate(49)

	if result.IsZero() {
		t.Errorf("AwesomeOscillator() should not be zero")
	}
}

func TestAwesomeOscillatorInsufficientData(t *testing.T) {
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

	ao := NewDefaultAwesomeOscillatorIndicator(s)
	result := ao.Calculate(9)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("AwesomeOscillator() should return ZERO for insufficient data, got %v", result)
	}
}

func TestAwesomeOscillatorSaucer(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 10; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(100),
			MaxPrice:   decimal.New(101),
			MinPrice:   decimal.New(99),
			ClosePrice: decimal.New(100),
			Volume:     decimal.New(1000),
		})
	}

	for i := 10; i < 20; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + (i - 10))),
			MaxPrice:   decimal.New(float64(105 + (i - 10))),
			MinPrice:   decimal.New(float64(95 + (i - 10))),
			ClosePrice: decimal.New(float64(102 + (i - 10))),
			Volume:     decimal.New(1000),
		})
	}

	ao := NewAwesomeOscillatorIndicator(s, 5, 10)
	result := ao.Calculate(19)

	if result.IsZero() {
		t.Errorf("AwesomeOscillator() should show trend")
	}
}
