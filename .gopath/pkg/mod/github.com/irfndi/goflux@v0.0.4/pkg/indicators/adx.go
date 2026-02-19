package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type adxIndicator struct {
	series       *series.TimeSeries
	high         Indicator
	low          Indicator
	close        Indicator
	period       int
	cacheTR      []decimal.Decimal
	cachePlusDM  []decimal.Decimal
	cacheMinusDM []decimal.Decimal
	cacheSmTR    []decimal.Decimal
	cacheSmPlus  []decimal.Decimal
	cacheSmMinus []decimal.Decimal
	cacheDX      []decimal.Decimal
	cacheADX     []decimal.Decimal
}

func NewADXIndicator(s *series.TimeSeries, period int) Indicator {
	return &adxIndicator{
		series:       s,
		high:         NewHighPriceIndicator(s),
		low:          NewLowPriceIndicator(s),
		close:        NewClosePriceIndicator(s),
		period:       period,
		cacheTR:      make([]decimal.Decimal, 0),
		cachePlusDM:  make([]decimal.Decimal, 0),
		cacheMinusDM: make([]decimal.Decimal, 0),
		cacheSmTR:    make([]decimal.Decimal, 0),
		cacheSmPlus:  make([]decimal.Decimal, 0),
		cacheSmMinus: make([]decimal.Decimal, 0),
		cacheDX:      make([]decimal.Decimal, 0),
		cacheADX:     make([]decimal.Decimal, 0),
	}
}

func (a *adxIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 || index > a.series.LastIndex() {
		return decimal.ZERO
	}

	a.fillCaches(index)

	if index < len(a.cacheADX) {
		return a.cacheADX[index]
	}

	return decimal.ZERO
}

func (a *adxIndicator) fillCaches(index int) {
	for i := len(a.cacheTR); i <= index; i++ {
		tr := a.calculateTrueRange(i)
		plusDM, minusDM := a.calculateDirectionalMovements(i)

		a.cacheTR = append(a.cacheTR, tr)
		a.cachePlusDM = append(a.cachePlusDM, plusDM)
		a.cacheMinusDM = append(a.cacheMinusDM, minusDM)
	}

	for i := len(a.cacheADX); i <= index; i++ {
		if i < a.period {
			a.cacheSmTR = append(a.cacheSmTR, decimal.ZERO)
			a.cacheSmPlus = append(a.cacheSmPlus, decimal.ZERO)
			a.cacheSmMinus = append(a.cacheSmMinus, decimal.ZERO)
			a.cacheDX = append(a.cacheDX, decimal.ZERO)
			a.cacheADX = append(a.cacheADX, decimal.ZERO)
			continue
		}

		periodDec := decimal.NewFromInt(int64(a.period))

		smTR := decimal.ZERO
		smPlus := decimal.ZERO
		smMinus := decimal.ZERO

		if i == a.period {
			for j := 1; j <= a.period; j++ {
				smTR = smTR.Add(a.cacheTR[j])
				smPlus = smPlus.Add(a.cachePlusDM[j])
				smMinus = smMinus.Add(a.cacheMinusDM[j])
			}
		} else {
			prevSmTR := a.cacheSmTR[i-1]
			prevSmPlus := a.cacheSmPlus[i-1]
			prevSmMinus := a.cacheSmMinus[i-1]

			smTR = prevSmTR.Sub(prevSmTR.Div(periodDec)).Add(a.cacheTR[i])
			smPlus = prevSmPlus.Sub(prevSmPlus.Div(periodDec)).Add(a.cachePlusDM[i])
			smMinus = prevSmMinus.Sub(prevSmMinus.Div(periodDec)).Add(a.cacheMinusDM[i])
		}

		a.cacheSmTR = append(a.cacheSmTR, smTR)
		a.cacheSmPlus = append(a.cacheSmPlus, smPlus)
		a.cacheSmMinus = append(a.cacheSmMinus, smMinus)

		plusDI := decimal.ZERO
		minusDI := decimal.ZERO
		if !smTR.Zero() {
			plusDI = smPlus.Div(smTR).Mul(decimal.New(100))
			minusDI = smMinus.Div(smTR).Mul(decimal.New(100))
		}

		dx := decimal.ZERO
		sumDI := plusDI.Add(minusDI)
		if !sumDI.Zero() {
			dx = plusDI.Sub(minusDI).Abs().Div(sumDI).Mul(decimal.New(100))
		}
		a.cacheDX = append(a.cacheDX, dx)

		firstADXIndex := 2*a.period - 1
		if i < firstADXIndex {
			a.cacheADX = append(a.cacheADX, decimal.ZERO)
			continue
		}

		if i == firstADXIndex {
			sumDX := decimal.ZERO
			for j := a.period; j <= firstADXIndex; j++ {
				sumDX = sumDX.Add(a.cacheDX[j])
			}
			a.cacheADX = append(a.cacheADX, sumDX.Div(periodDec))
			continue
		}

		prevADX := a.cacheADX[i-1]
		periodMinusOne := decimal.NewFromInt(int64(a.period - 1))
		adx := prevADX.Mul(periodMinusOne).Add(dx).Div(periodDec)
		a.cacheADX = append(a.cacheADX, adx)
	}
}

func (a *adxIndicator) calculateDirectionalMovements(index int) (decimal.Decimal, decimal.Decimal) {
	if index == 0 {
		return decimal.ZERO, decimal.ZERO
	}

	currentHigh := a.high.Calculate(index)
	currentLow := a.low.Calculate(index)
	prevHigh := a.high.Calculate(index - 1)
	prevLow := a.low.Calculate(index - 1)

	up := currentHigh.Sub(prevHigh)
	down := prevLow.Sub(currentLow)

	plusDM := decimal.ZERO
	minusDM := decimal.ZERO

	if up.GT(down) && up.IsPositive() {
		plusDM = up
	} else if down.GT(up) && down.IsPositive() {
		minusDM = down
	}

	return plusDM, minusDM
}

func (a *adxIndicator) calculateTrueRange(index int) decimal.Decimal {
	if index == 0 {
		return a.high.Calculate(0).Sub(a.low.Calculate(0))
	}

	currentHigh := a.high.Calculate(index)
	currentLow := a.low.Calculate(index)
	prevClose := a.close.Calculate(index - 1)

	tr := currentHigh.Sub(currentLow)
	trHighClose := currentHigh.Sub(prevClose).Abs()
	trLowClose := currentLow.Sub(prevClose).Abs()

	if trHighClose.GT(tr) {
		tr = trHighClose
	}
	if trLowClose.GT(tr) {
		tr = trLowClose
	}

	return tr
}
