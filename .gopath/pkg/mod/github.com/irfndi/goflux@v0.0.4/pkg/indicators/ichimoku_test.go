package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestIchimoku(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 60; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(102 + i)),
			Volume:     decimal.New(1000),
		})
	}

	ichimoku := NewIchimokuIndicator(s)
	result := ichimoku.Calculate(59)

	if result.IsZero() {
		t.Errorf("Ichimoku() should not be zero")
	}
}

func TestIchimokuInsufficientData(t *testing.T) {
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

	ichimoku := NewIchimokuIndicator(s)
	result := ichimoku.Calculate(5)

	if !result.EQ(decimal.ZERO) {
		t.Errorf("Ichimoku() should return ZERO for insufficient data, got %v", result)
	}
}

func TestIchimokuComponents(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 60; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(102 + i)),
			Volume:     decimal.New(1000),
		})
	}

	ichimoku := NewIchimokuIndicator(s)

	tenkan := ichimoku.TenkanSen(59)
	kijun := ichimoku.KijunSen(59)
	spanA := ichimoku.SenkouSpanA(59)
	spanB := ichimoku.SenkouSpanB(59)
	chikou := ichimoku.ChikouSpan(59)

	if tenkan.IsZero() {
		t.Errorf("TenkanSen should not be zero")
	}
	if kijun.IsZero() {
		t.Errorf("KijunSen should not be zero")
	}
	if spanA.IsZero() {
		t.Errorf("SenkouSpanA should not be zero")
	}
	if spanB.IsZero() {
		t.Errorf("SenkouSpanB should not be zero")
	}
	if chikou.IsZero() {
		t.Errorf("ChikouSpan should not be zero")
	}
}

func TestIchimokuCloud(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 60; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(float64(100 + i)),
			MaxPrice:   decimal.New(float64(105 + i)),
			MinPrice:   decimal.New(float64(95 + i)),
			ClosePrice: decimal.New(float64(102 + i)),
			Volume:     decimal.New(1000),
		})
	}

	ichimoku := NewIchimokuIndicator(s)
	cloud := ichimoku.Cloud(59)

	if cloud.TenkanSen.IsZero() {
		t.Errorf("Cloud TenkanSen should not be zero")
	}
	if cloud.KijunSen.IsZero() {
		t.Errorf("Cloud KijunSen should not be zero")
	}
	if cloud.SenkouSpanA.IsZero() {
		t.Errorf("Cloud SenkouSpanA should not be zero")
	}
	if cloud.SenkouSpanB.IsZero() {
		t.Errorf("Cloud SenkouSpanB should not be zero")
	}
	if cloud.ChikouSpan.IsZero() {
		t.Errorf("Cloud ChikouSpan should not be zero")
	}
}
