package indicators

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestVWAP(t *testing.T) {
	s := series.NewTimeSeries()

	// Price 100, Vol 10 -> PV=1000, V=10 -> VWAP=100
	s.AddCandle(&series.Candle{
		MaxPrice:   decimal.New(105),
		MinPrice:   decimal.New(95),
		ClosePrice: decimal.New(100),
		Volume:     decimal.New(10),
	})

	// Price 110, Vol 20 -> PV=2200, V=20 -> Total PV=3200, V=30 -> VWAP=106.66
	s.AddCandle(&series.Candle{
		MaxPrice:   decimal.New(115),
		MinPrice:   decimal.New(105),
		ClosePrice: decimal.New(110),
		Volume:     decimal.New(20),
	})

	vwap := NewVWAPIndicator(s)

	val0 := vwap.Calculate(0)
	if val0.Float() != 100 {
		t.Errorf("VWAP(0) = %v, want 100", val0)
	}

	val1 := vwap.Calculate(1)
	if val1.FormattedString(2) != "106.67" {
		t.Errorf("VWAP(1) = %v, want 106.67", val1)
	}
}

func TestSuperTrend(t *testing.T) {
	s := series.NewTimeSeries()

	// Constant prices -> ATR will be small
	for i := 0; i < 20; i++ {
		s.AddCandle(&series.Candle{
			MaxPrice:   decimal.New(100 + float64(i)),
			MinPrice:   decimal.New(90 + float64(i)),
			ClosePrice: decimal.New(95 + float64(i)),
			Volume:     decimal.New(1000),
		})
	}

	st := NewSuperTrendIndicator(s, 10, 3.0)
	val := st.Calculate(19)

	if val.IsZero() {
		t.Errorf("SuperTrend should not be zero")
	}
}

func TestATRRatio(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 6; i++ {
		s.AddCandle(&series.Candle{
			MaxPrice:   decimal.New(105),
			MinPrice:   decimal.New(95),
			ClosePrice: decimal.New(100),
		})
	}

	atr := NewAverageTrueRangeIndicator(s, 3)
	price := NewClosePriceIndicator(s)
	atrRatio := NewATRRatioIndicator(atr, price)

	got := atrRatio.Calculate(3)
	want := decimal.New(0.1)
	if got.Sub(want).Abs().GT(decimal.New(0.0001)) {
		t.Errorf("ATR ratio (index 3) = %v, want %v", got, want)
	}

	atrRatio2 := NewATRRatioIndicatorFromSeries(s, 3)
	got2 := atrRatio2.Calculate(3)
	if got2.Sub(want).Abs().GT(decimal.New(0.0001)) {
		t.Errorf("ATR ratio from series (index 3) = %v, want %v", got2, want)
	}
}

func TestTRIMA(t *testing.T) {
	s := series.NewTimeSeries()
	for i := 1; i <= 10; i++ {
		s.AddCandle(&series.Candle{
			ClosePrice: decimal.New(float64(i)),
		})
	}

	closePrice := NewClosePriceIndicator(s)
	epsilon := decimal.New(0.0001)

	trima5 := NewTRIMAIndicator(closePrice, 5)
	if !trima5.Calculate(3).EQ(decimal.ZERO) {
		t.Errorf("TRIMA(5) (index 3) = %v, want 0", trima5.Calculate(3))
	}
	if trima5.Calculate(4).Sub(decimal.New(3.0)).Abs().GT(epsilon) {
		t.Errorf("TRIMA(5) (index 4) = %v, want 3", trima5.Calculate(4))
	}
	if trima5.Calculate(5).Sub(decimal.New(4.0)).Abs().GT(epsilon) {
		t.Errorf("TRIMA(5) (index 5) = %v, want 4", trima5.Calculate(5))
	}

	trima4 := NewTRIMAIndicator(closePrice, 4)
	if !trima4.Calculate(2).EQ(decimal.ZERO) {
		t.Errorf("TRIMA(4) (index 2) = %v, want 0", trima4.Calculate(2))
	}
	if trima4.Calculate(3).Sub(decimal.New(2.5)).Abs().GT(epsilon) {
		t.Errorf("TRIMA(4) (index 3) = %v, want 2.5", trima4.Calculate(3))
	}
	if trima4.Calculate(4).Sub(decimal.New(3.5)).Abs().GT(epsilon) {
		t.Errorf("TRIMA(4) (index 4) = %v, want 3.5", trima4.Calculate(4))
	}
}

func TestVWMA(t *testing.T) {
	s := series.NewTimeSeries()
	s.AddCandle(&series.Candle{ClosePrice: decimal.New(10), Volume: decimal.New(1)})
	s.AddCandle(&series.Candle{ClosePrice: decimal.New(20), Volume: decimal.New(2)})
	s.AddCandle(&series.Candle{ClosePrice: decimal.New(30), Volume: decimal.New(3)})

	vwma := NewVWMAIndicatorFromSeries(s, 3)
	if !vwma.Calculate(1).EQ(decimal.ZERO) {
		t.Errorf("VWMA(3) (index 1) = %v, want 0", vwma.Calculate(1))
	}

	got := vwma.Calculate(2)
	want := decimal.New(140.0 / 6.0) // (10*1 + 20*2 + 30*3) / (1+2+3)
	if got.Sub(want).Abs().GT(decimal.New(0.0001)) {
		t.Errorf("VWMA(3) (index 2) = %v, want %v", got, want)
	}
}

func TestRMA(t *testing.T) {
	s := series.NewTimeSeries()
	for i := 1; i <= 6; i++ {
		s.AddCandle(&series.Candle{ClosePrice: decimal.New(float64(i))})
	}

	closePrice := NewClosePriceIndicator(s)
	rma := NewRMAIndicator(closePrice, 3)
	epsilon := decimal.New(0.0001)

	if !rma.Calculate(0).EQ(decimal.ZERO) {
		t.Errorf("RMA(3) (index 0) = %v, want 0", rma.Calculate(0))
	}
	if !rma.Calculate(1).EQ(decimal.ZERO) {
		t.Errorf("RMA(3) (index 1) = %v, want 0", rma.Calculate(1))
	}

	if rma.Calculate(2).Sub(decimal.New(2.0)).Abs().GT(epsilon) {
		t.Errorf("RMA(3) (index 2) = %v, want 2", rma.Calculate(2))
	}
	if rma.Calculate(3).Sub(decimal.New(8.0 / 3.0)).Abs().GT(epsilon) {
		t.Errorf("RMA(3) (index 3) = %v, want %v", rma.Calculate(3), decimal.New(8.0/3.0))
	}
	if rma.Calculate(4).Sub(decimal.New(31.0 / 9.0)).Abs().GT(epsilon) {
		t.Errorf("RMA(3) (index 4) = %v, want %v", rma.Calculate(4), decimal.New(31.0/9.0))
	}
	if rma.Calculate(5).Sub(decimal.New(116.0 / 27.0)).Abs().GT(epsilon) {
		t.Errorf("RMA(3) (index 5) = %v, want %v", rma.Calculate(5), decimal.New(116.0/27.0))
	}
}
