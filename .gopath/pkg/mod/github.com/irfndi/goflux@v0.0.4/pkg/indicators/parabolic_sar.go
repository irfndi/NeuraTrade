package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type parabolicSARIndicator struct {
	Indicator
	series      *series.TimeSeries
	high        Indicator
	low         Indicator
	af          decimal.Decimal
	maxAF       decimal.Decimal
	prevSAR     decimal.Decimal
	prevEP      decimal.Decimal
	prevAF      decimal.Decimal
	trend       int
	initialized bool
}

func NewParabolicSARIndicator(s *series.TimeSeries) Indicator {
	return &parabolicSARIndicator{
		series: s,
		high:   NewHighPriceIndicator(s),
		low:    NewLowPriceIndicator(s),
		af:     decimal.New(0.02),
		maxAF:  decimal.New(0.2),
		trend:  0,
	}
}

func (ps *parabolicSARIndicator) Calculate(index int) decimal.Decimal {
	if index < 1 {
		return ps.high.Calculate(index)
	}

	if !ps.initialized {
		ps.initialize()
		return ps.prevSAR
	}

	currentHigh := ps.high.Calculate(index)
	currentLow := ps.low.Calculate(index)
	previousHigh := ps.high.Calculate(index - 1)
	previousLow := ps.low.Calculate(index - 1)

	switch ps.trend {
	case 1:
		if currentHigh.GT(ps.prevEP) {
			ps.prevEP = currentHigh
			ps.prevAF = ps.prevAF.Add(ps.af)
			if ps.prevAF.GT(ps.maxAF) {
				ps.prevAF = ps.maxAF
			}
		}
		ps.prevSAR = ps.prevSAR.Add(ps.prevAF.Mul(ps.prevEP.Sub(ps.prevSAR)))

		if currentLow.LT(ps.prevSAR) {
			ps.trend = -1
			ps.prevAF = ps.af
			ps.prevEP = previousLow
			ps.prevSAR = ps.prevEP
		}
	case -1:
		if currentLow.LT(ps.prevEP) {
			ps.prevEP = currentLow
			ps.prevAF = ps.prevAF.Add(ps.af)
			if ps.prevAF.GT(ps.maxAF) {
				ps.prevAF = ps.maxAF
			}
		}
		ps.prevSAR = ps.prevSAR.Add(ps.prevAF.Mul(ps.prevEP.Sub(ps.prevSAR)))

		if currentHigh.GT(ps.prevSAR) {
			ps.trend = 1
			ps.prevAF = ps.af
			ps.prevEP = previousHigh
			ps.prevSAR = ps.prevEP
		}
	}

	return ps.prevSAR
}

func (ps *parabolicSARIndicator) initialize() {
	firstHigh := ps.high.Calculate(0)
	firstLow := ps.low.Calculate(0)
	secondHigh := ps.high.Calculate(1)
	secondLow := ps.low.Calculate(1)

	if secondHigh.GT(firstHigh) && secondLow.GT(firstLow) {
		ps.trend = 1
		ps.prevEP = secondHigh
		ps.prevSAR = firstLow
	} else if secondHigh.LT(firstHigh) && secondLow.LT(firstLow) {
		ps.trend = -1
		ps.prevEP = secondLow
		ps.prevSAR = firstHigh
	} else if secondHigh.GT(firstHigh) {
		ps.trend = 1
		ps.prevEP = secondHigh
		ps.prevSAR = secondLow
	} else {
		ps.trend = -1
		ps.prevEP = secondLow
		ps.prevSAR = secondHigh
	}

	ps.prevAF = ps.af
	ps.initialized = true
}

func (ps *parabolicSARIndicator) Trend() int {
	return ps.trend
}

func (ps *parabolicSARIndicator) EP() decimal.Decimal {
	return ps.prevEP
}

func (ps *parabolicSARIndicator) AF() decimal.Decimal {
	return ps.prevAF
}
