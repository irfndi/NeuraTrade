package trading

import (
	"time"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

// Rule is an interface describing an algorithm by which a set of criteria may be satisfied
type Rule interface {
	IsSatisfied(index int, record *TradingRecord) bool
}

// And returns a new rule whereby BOTH of the passed-in rules must be satisfied for the rule to be satisfied
func And(r1, r2 Rule) Rule {
	return andRule{r1, r2}
}

// Or returns a new rule whereby ONE OF the passed-in rules must be satisfied for the rule to be satisfied
func Or(r1, r2 Rule) Rule {
	return orRule{r1, r2}
}

// Not returns a new rule whereby the passed-in rule must NOT be satisfied for the rule to be satisfied
func Not(r Rule) Rule {
	return notRule{r}
}

// Vote returns a new rule that is satisfied when at least threshold number of passed-in rules are satisfied
func Vote(threshold int, rules ...Rule) Rule {
	return voteRule{threshold, rules}
}

type andRule struct {
	r1 Rule
	r2 Rule
}

func (ar andRule) IsSatisfied(index int, record *TradingRecord) bool {
	return ar.r1.IsSatisfied(index, record) && ar.r2.IsSatisfied(index, record)
}

type orRule struct {
	r1 Rule
	r2 Rule
}

func (or orRule) IsSatisfied(index int, record *TradingRecord) bool {
	return or.r1.IsSatisfied(index, record) || or.r2.IsSatisfied(index, record)
}

type notRule struct {
	r Rule
}

func (nr notRule) IsSatisfied(index int, record *TradingRecord) bool {
	return !nr.r.IsSatisfied(index, record)
}

type voteRule struct {
	threshold int
	rules     []Rule
}

func (vr voteRule) IsSatisfied(index int, record *TradingRecord) bool {
	count := 0
	for _, rule := range vr.rules {
		if rule.IsSatisfied(index, record) {
			count++
		}
	}
	return count >= vr.threshold
}

// SignalRule is a rule that is satisfied when the underlying SignalIndicator returns SignalBuy
type SignalRule struct {
	Signal indicators.SignalIndicator
}

// NewSignalRule returns a new rule that is satisfied when the underlying SignalIndicator returns SignalBuy
func NewSignalRule(signal indicators.SignalIndicator) Rule {
	return SignalRule{signal}
}

func (sr SignalRule) IsSatisfied(index int, record *TradingRecord) bool {
	return sr.Signal.CalculateSignal(index) == indicators.SignalBuy
}

// PositionNewRule is a rule that is satisfied when the current position is new
type PositionNewRule struct{}

func (pnr PositionNewRule) IsSatisfied(index int, record *TradingRecord) bool {
	return record.CurrentPosition().IsNew()
}

// PositionOpenRule is a rule that is satisfied when the current position is open
type PositionOpenRule struct{}

func (por PositionOpenRule) IsSatisfied(index int, record *TradingRecord) bool {
	return record.CurrentPosition().IsOpen()
}

// OverIndicatorRule is a rule where the First indicators.Indicator must be greater than the Second indicators.Indicator to be Satisfied
type OverIndicatorRule struct {
	First  indicators.Indicator
	Second indicators.Indicator
}

// NewOverIndicatorRule returns a new rule where the first indicator must be greater than the second indicator
func NewOverIndicatorRule(first, second indicators.Indicator) Rule {
	return OverIndicatorRule{first, second}
}

// IsSatisfied returns true when the First indicators.Indicator is greater than the Second indicators.Indicator
func (oir OverIndicatorRule) IsSatisfied(index int, record *TradingRecord) bool {
	return oir.First.Calculate(index).GT(oir.Second.Calculate(index))
}

// UnderIndicatorRule is a rule where the First indicators.Indicator must be less than the Second indicators.Indicator to be Satisfied
type UnderIndicatorRule struct {
	First  indicators.Indicator
	Second indicators.Indicator
}

// NewUnderIndicatorRule returns a new rule where the first indicator must be less than the second indicator
func NewUnderIndicatorRule(first, second indicators.Indicator) Rule {
	return UnderIndicatorRule{first, second}
}

// IsSatisfied returns true when the First indicators.Indicator is less than the Second indicators.Indicator
func (uir UnderIndicatorRule) IsSatisfied(index int, record *TradingRecord) bool {
	return uir.First.Calculate(index).LT(uir.Second.Calculate(index))
}

type percentChangeRule struct {
	indicator indicators.Indicator
	percent   decimal.Decimal
}

func (pgr percentChangeRule) IsSatisfied(index int, record *TradingRecord) bool {
	return pgr.indicator.Calculate(index).Abs().GT(pgr.percent.Abs())
}

// NewPercentChangeRule returns a rule whereby the given indicators.Indicator must have changed by a given percentage to be satisfied.
// You should specify percent as a float value between -1 and 1
func NewPercentChangeRule(indicator indicators.Indicator, percent float64) Rule {
	return percentChangeRule{
		indicator: indicators.NewPercentChangeIndicator(indicator),
		percent:   decimal.New(percent),
	}
}

// FixedBarExitRule is a rule that is satisfied when the position has been open for a fixed number of bars.
// This requires the TimeSeries to be passed in to find the entry index.
type FixedBarExitRule struct {
	Series *series.TimeSeries
	Bars   int
}

// NewFixedBarExitRule returns a new rule that is satisfied when a position has been open for a specified number of bars.
func NewFixedBarExitRule(series *series.TimeSeries, bars int) Rule {
	return FixedBarExitRule{
		Series: series,
		Bars:   bars,
	}
}

func (fbe FixedBarExitRule) IsSatisfied(index int, record *TradingRecord) bool {
	if !record.CurrentPosition().IsOpen() {
		return false
	}

	pos := record.CurrentPosition()
	entryTime := pos.EntranceOrder().ExecutionTime
	entryIndex := -1
	// Find entry index
	for i := index; i >= 0; i-- {
		if fbe.Series.GetCandle(i).Period.End.Equal(entryTime) {
			entryIndex = i
			break
		}
	}

	if entryIndex == -1 {
		return false
	}

	return index-entryIndex >= fbe.Bars
}

// WaitDurationRule is a rule that is satisfied when the position has been open for a certain duration.
type WaitDurationRule struct {
	Series   *series.TimeSeries
	Duration time.Duration
}

// NewWaitDurationRule returns a new rule that is satisfied when a position has been open for a specified duration.
func NewWaitDurationRule(series *series.TimeSeries, duration time.Duration) Rule {
	return WaitDurationRule{
		Series:   series,
		Duration: duration,
	}
}

func (wdr WaitDurationRule) IsSatisfied(index int, record *TradingRecord) bool {
	if !record.CurrentPosition().IsOpen() {
		return false
	}

	pos := record.CurrentPosition()
	entryTime := pos.EntranceOrder().ExecutionTime
	currentTime := wdr.Series.GetCandle(index).Period.End

	return currentTime.Sub(entryTime) >= wdr.Duration
}

// TimeOfDayExitRule is a rule that is satisfied when the current time of day is at or after a certain time.
type TimeOfDayExitRule struct {
	Series *series.TimeSeries
	Hour   int
	Minute int
}

// NewTimeOfDayExitRule returns a new rule that is satisfied when the time of day is at or after the specified hour and minute.
func NewTimeOfDayExitRule(series *series.TimeSeries, hour, minute int) Rule {
	return TimeOfDayExitRule{
		Series: series,
		Hour:   hour,
		Minute: minute,
	}
}

func (tdr TimeOfDayExitRule) IsSatisfied(index int, record *TradingRecord) bool {
	if !record.CurrentPosition().IsOpen() {
		return false
	}

	currentTime := tdr.Series.GetCandle(index).Period.End
	if currentTime.Hour() > tdr.Hour {
		return true
	}
	if currentTime.Hour() == tdr.Hour && currentTime.Minute() >= tdr.Minute {
		return true
	}

	return false
}
