package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

const (
	SignalNeutral = 0
	SignalBuy     = 1
	SignalSell    = -1
	CrossAbove    = 1
	CrossBelow    = -1
)

type SignalIndicator interface {
	CalculateSignal(int) int
}

type crossoverSignal struct {
	shortTerm Indicator
	longTerm  Indicator
	prevShort decimal.Decimal
	prevLong  decimal.Decimal
	crossType int
}

func NewCrossoverSignal(shortTerm, longTerm Indicator, crossType int) SignalIndicator {
	var prevShort, prevLong decimal.Decimal
	return &crossoverSignal{
		shortTerm: shortTerm,
		longTerm:  longTerm,
		prevShort: prevShort,
		prevLong:  prevLong,
		crossType: crossType,
	}
}

func (cs *crossoverSignal) CalculateSignal(index int) int {
	if index == 0 {
		return SignalNeutral
	}

	currentShort := cs.shortTerm.Calculate(index)
	currentLong := cs.longTerm.Calculate(index)
	prevShort := cs.shortTerm.Calculate(index - 1)
	prevLong := cs.longTerm.Calculate(index - 1)

	if cs.crossType == CrossAbove {
		if prevShort.LTE(prevLong) && currentShort.GT(currentLong) {
			return SignalBuy
		}
		if prevShort.GTE(prevLong) && currentShort.LT(currentLong) {
			return SignalSell
		}
	}

	if cs.crossType == CrossBelow {
		if prevShort.GTE(prevLong) && currentShort.LT(currentLong) {
			return SignalSell
		}
		if prevShort.LTE(prevLong) && currentShort.GT(currentLong) {
			return SignalBuy
		}
	}

	return SignalNeutral
}

type thresholdSignal struct {
	indicator Indicator
	upper     decimal.Decimal
	lower     decimal.Decimal
}

func NewThresholdSignal(indicator Indicator, upper, lower float64) SignalIndicator {
	return &thresholdSignal{
		indicator: indicator,
		upper:     decimal.New(upper),
		lower:     decimal.New(lower),
	}
}

func (ts *thresholdSignal) CalculateSignal(index int) int {
	value := ts.indicator.Calculate(index)

	if value.GT(ts.upper) {
		return SignalSell
	}
	if value.LT(ts.lower) {
		return SignalBuy
	}
	return SignalNeutral
}

type rsiSignal struct {
	rsi        Indicator
	overbought decimal.Decimal
	oversold   decimal.Decimal
}

func NewRSISignal(rsi Indicator, overbought, oversold float64) SignalIndicator {
	return &rsiSignal{
		rsi:        rsi,
		overbought: decimal.New(overbought),
		oversold:   decimal.New(oversold),
	}
}

func (rs *rsiSignal) CalculateSignal(index int) int {
	value := rs.rsi.Calculate(index)

	if value.LT(rs.oversold) {
		return SignalBuy
	}
	if value.GT(rs.overbought) {
		return SignalSell
	}
	return SignalNeutral
}

type macdSignal struct {
	macd   Indicator
	signal Indicator
}

func NewMACDSignal(macd, signal Indicator) SignalIndicator {
	return &macdSignal{
		macd:   macd,
		signal: signal,
	}
}

func (ms *macdSignal) CalculateSignal(index int) int {
	if index == 0 {
		return SignalNeutral
	}

	currentMacd := ms.macd.Calculate(index)
	currentSignal := ms.signal.Calculate(index)
	prevMacd := ms.macd.Calculate(index - 1)
	prevSignal := ms.signal.Calculate(index - 1)

	if prevMacd.LTE(prevSignal) && currentMacd.GT(currentSignal) {
		return SignalBuy
	}
	if prevMacd.GTE(prevSignal) && currentMacd.LT(currentSignal) {
		return SignalSell
	}

	if currentMacd.GT(currentSignal) {
		return SignalBuy
	}
	if currentMacd.LT(currentSignal) {
		return SignalSell
	}

	return SignalNeutral
}

type supertrendSignal struct {
	supertrend Indicator
}

func NewSupertrendSignal(supertrend Indicator) SignalIndicator {
	return &supertrendSignal{
		supertrend: supertrend,
	}
}

func (ss *supertrendSignal) CalculateSignal(index int) int {
	value := ss.supertrend.Calculate(index)

	if value.IsPositive() {
		return SignalBuy
	}
	if value.IsNegative() {
		return SignalSell
	}
	return SignalNeutral
}

type multiSignal struct {
	signals       []SignalIndicator
	voteThreshold int
}

func NewMultiSignal(signals []SignalIndicator, voteThreshold int) SignalIndicator {
	return &multiSignal{
		signals:       signals,
		voteThreshold: voteThreshold,
	}
}

func (ms *multiSignal) CalculateSignal(index int) int {
	if len(ms.signals) == 0 {
		return SignalNeutral
	}

	buyVotes := 0
	sellVotes := 0

	for _, signal := range ms.signals {
		sig := signal.CalculateSignal(index)
		if sig == SignalBuy {
			buyVotes++
		} else if sig == SignalSell {
			sellVotes++
		}
	}

	if buyVotes >= ms.voteThreshold && buyVotes > sellVotes {
		return SignalBuy
	}
	if sellVotes >= ms.voteThreshold && sellVotes > buyVotes {
		return SignalSell
	}

	return SignalNeutral
}

func CombineSignals(signals ...SignalIndicator) []SignalIndicator {
	if signals == nil {
		return []SignalIndicator{}
	}
	return signals
}
