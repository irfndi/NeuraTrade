package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type PivotPointResult struct {
	Pivot      decimal.Decimal
	R1, R2, R3 decimal.Decimal
	S1, S2, S3 decimal.Decimal
}

type pivotPointsIndicator struct {
	Indicator
	series *series.TimeSeries
}

// NewPivotPointsIndicator returns an indicator that calculates standard Pivot Points.
// It usually uses the previous day's H, L, C to calculate today's levels.
func NewPivotPointsIndicator(s *series.TimeSeries) *pivotPointsIndicator {
	return &pivotPointsIndicator{series: s}
}

func (p *pivotPointsIndicator) Calculate(index int) decimal.Decimal {
	// Standard Calculate returns the Pivot (P)
	if index < 0 || index >= len(p.series.Candles) {
		return decimal.ZERO
	}
	res := p.GetLevels(index)
	return res.Pivot
}

func (p *pivotPointsIndicator) GetLevels(index int) PivotPointResult {
	if index <= 0 {
		return PivotPointResult{}
	}

	// Traditionally uses PREVIOUS candle's H, L, C
	prev := p.series.Candles[index-1]
	high := prev.MaxPrice
	low := prev.MinPrice
	close := prev.ClosePrice

	pivot := high.Add(low).Add(close).Div(decimal.New(3))

	// R1 = 2P - L
	r1 := pivot.Mul(decimal.New(2)).Sub(low)
	// S1 = 2P - H
	s1 := pivot.Mul(decimal.New(2)).Sub(high)

	// R2 = P + (H - L)
	r2 := pivot.Add(high.Sub(low))
	// S2 = P - (H - L)
	s2 := pivot.Sub(high.Sub(low))

	// R3 = H + 2(P - L)
	r3 := high.Add(pivot.Sub(low).Mul(decimal.New(2)))
	// S3 = L - 2(H - P)
	s3 := low.Sub(high.Sub(pivot).Mul(decimal.New(2)))

	return PivotPointResult{
		Pivot: pivot,
		R1:    r1,
		R2:    r2,
		R3:    r3,
		S1:    s1,
		S2:    s2,
		S3:    s3,
	}
}
