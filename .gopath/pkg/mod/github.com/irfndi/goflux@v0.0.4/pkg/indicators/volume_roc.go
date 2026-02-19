package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type volumeROCIndicator struct {
	Indicator
	volume Indicator
	period int
}

// NewVolumeROCIndicator returns an indicator that calculates the Volume Rate of Change.
func NewVolumeROCIndicator(s *series.TimeSeries, period int) Indicator {
	return &volumeROCIndicator{
		volume: NewVolumeIndicator(s),
		period: period,
	}
}

func (v *volumeROCIndicator) Calculate(index int) decimal.Decimal {
	prevIdx := index - v.period
	if prevIdx < 0 {
		return decimal.ZERO
	}

	currVol := v.volume.Calculate(index)
	prevVol := v.volume.Calculate(prevIdx)

	if prevVol.IsZero() {
		return decimal.ZERO
	}

	return currVol.Sub(prevVol).Div(prevVol).Mul(decimal.New(100))
}
