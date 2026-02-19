package trading

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

type stopLossRule struct {
	indicators.Indicator
	tolerance decimal.Decimal
}

// NewStopLossRule returns a new rule that is satisfied when the given loss tolerance (a percentage) is met or exceeded.
// Loss tolerance should be a value between -1 and 1.
func NewStopLossRule(series *series.TimeSeries, lossTolerance float64) Rule {
	return stopLossRule{
		Indicator: indicators.NewClosePriceIndicator(series),
		tolerance: decimal.New(lossTolerance),
	}
}

func (slr stopLossRule) IsSatisfied(index int, record *TradingRecord) bool {
	if !record.CurrentPosition().IsOpen() {
		return false
	}

	openPrice := record.CurrentPosition().CostBasis()
	loss := slr.Indicator.Calculate(index).Div(openPrice).Sub(decimal.ONE)
	return loss.LTE(slr.tolerance)
}

type trailingStopLossRule struct {
	series    *series.TimeSeries
	tolerance decimal.Decimal
}

// NewTrailingStopLossRule returns a new rule that is satisfied when the price drops by a percentage
// from its peak since the position was opened.
func NewTrailingStopLossRule(series *series.TimeSeries, tolerance float64) Rule {
	return trailingStopLossRule{
		series:    series,
		tolerance: decimal.New(tolerance),
	}
}

func (tsl trailingStopLossRule) IsSatisfied(index int, record *TradingRecord) bool {
	if !record.CurrentPosition().IsOpen() {
		return false
	}

	pos := record.CurrentPosition()
	entryIndex := -1
	// We need to find the entry index.
	// This is a bit inefficient without it being stored in Position.
	// For now, let's assume we can't easily find it without adding it to Position.
	// Actually, we can check the record's trades and the execution time.

	// Better yet, let's just look back in the series for the candle that matches entry time.
	entryTime := pos.EntranceOrder().ExecutionTime
	for i := index; i >= 0; i-- {
		if tsl.series.GetCandle(i).Period.End.Equal(entryTime) {
			entryIndex = i
			break
		}
	}

	if entryIndex == -1 {
		return false
	}

	maxPrice := decimal.ZERO
	for i := entryIndex; i <= index; i++ {
		price := tsl.series.GetCandle(i).MaxPrice
		if price.GT(maxPrice) {
			maxPrice = price
		}
	}

	currentPrice := tsl.series.GetCandle(index).ClosePrice
	loss := currentPrice.Div(maxPrice).Sub(decimal.ONE)

	return loss.LTE(tsl.tolerance)
}

type fixedProfitRule struct {
	indicators.Indicator
	target decimal.Decimal
}

// NewFixedProfitRule returns a new rule that is satisfied when the given profit target (a percentage) is met or exceeded.
// Profit target should be a value between 0 and 1 (e.g. 0.1 for 10% gain).
func NewFixedProfitRule(series *series.TimeSeries, profitTarget float64) Rule {
	return fixedProfitRule{
		Indicator: indicators.NewClosePriceIndicator(series),
		target:    decimal.New(profitTarget),
	}
}

func (fpr fixedProfitRule) IsSatisfied(index int, record *TradingRecord) bool {
	if !record.CurrentPosition().IsOpen() {
		return false
	}

	openPrice := record.CurrentPosition().CostBasis()
	gain := fpr.Indicator.Calculate(index).Div(openPrice).Sub(decimal.ONE)
	return gain.GTE(fpr.target)
}

type trailingTakeProfitRule struct {
	series    *series.TimeSeries
	threshold decimal.Decimal
	trailing  decimal.Decimal
}

// NewTrailingTakeProfitRule returns a new rule that is satisfied when the price reaches a threshold profit
// and then drops by a trailing percentage from its peak since that threshold was hit.
func NewTrailingTakeProfitRule(series *series.TimeSeries, threshold, trailing float64) Rule {
	return trailingTakeProfitRule{
		series:    series,
		threshold: decimal.New(threshold),
		trailing:  decimal.New(trailing),
	}
}

func (ttp trailingTakeProfitRule) IsSatisfied(index int, record *TradingRecord) bool {
	if !record.CurrentPosition().IsOpen() {
		return false
	}

	pos := record.CurrentPosition()
	openPrice := pos.EntranceOrder().Price
	currentPrice := ttp.series.GetCandle(index).ClosePrice

	entryTime := pos.EntranceOrder().ExecutionTime
	entryIndex := -1
	for i := index; i >= 0; i-- {
		if ttp.series.GetCandle(i).Period.End.Equal(entryTime) {
			entryIndex = i
			break
		}
	}

	if entryIndex == -1 {
		return false
	}

	hitThreshold := false
	maxPriceSinceThreshold := decimal.ZERO

	for i := entryIndex; i <= index; i++ {
		price := ttp.series.GetCandle(i).MaxPrice
		gainAtI := price.Div(openPrice).Sub(decimal.ONE)
		if gainAtI.GTE(ttp.threshold) {
			hitThreshold = true
		}
		if hitThreshold {
			if price.GT(maxPriceSinceThreshold) {
				maxPriceSinceThreshold = price
			}
		}
	}

	if !hitThreshold {
		return false
	}

	drop := currentPrice.Div(maxPriceSinceThreshold).Sub(decimal.ONE)
	return drop.LTE(ttp.trailing.Neg())
}
