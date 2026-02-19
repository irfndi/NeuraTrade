package indicators

import (
	"github.com/irfndi/goflux/pkg/series"
)

// IndicatorBuilder is a fluent API for building indicators
type IndicatorBuilder struct {
	indicator Indicator
}

// NewIndicatorBuilder starts a new indicator pipeline with a close price indicator
func NewIndicatorBuilder(s *series.TimeSeries) *IndicatorBuilder {
	return &IndicatorBuilder{
		indicator: NewClosePriceIndicator(s),
	}
}

// SMA adds a Simple Moving Average to the pipeline
func (b *IndicatorBuilder) SMA(window int) *IndicatorBuilder {
	b.indicator = NewSimpleMovingAverage(b.indicator, window)
	return b
}

// EMA adds an Exponential Moving Average to the pipeline
func (b *IndicatorBuilder) EMA(window int) *IndicatorBuilder {
	b.indicator = NewEMAIndicator(b.indicator, window)
	return b
}

// RSI adds a Relative Strength Index to the pipeline
func (b *IndicatorBuilder) RSI(window int) *IndicatorBuilder {
	b.indicator = NewRelativeStrengthIndexIndicator(b.indicator, window)
	return b
}

// MACD adds a MACD indicator to the pipeline
func (b *IndicatorBuilder) MACD(fast, slow int) *IndicatorBuilder {
	b.indicator = NewMACDIndicator(b.indicator, fast, slow)
	return b
}

// BollingerUpper adds a Bollinger Upper Band to the pipeline
func (b *IndicatorBuilder) BollingerUpper(window int, sigma float64) *IndicatorBuilder {
	b.indicator = NewBollingerUpperBandIndicator(b.indicator, window, sigma)
	return b
}

// BollingerLower adds a Bollinger Lower Band to the pipeline
func (b *IndicatorBuilder) BollingerLower(window int, sigma float64) *IndicatorBuilder {
	b.indicator = NewBollingerLowerBandIndicator(b.indicator, window, sigma)
	return b
}

// Build returns the final indicator
func (b *IndicatorBuilder) Build() Indicator {
	return b.indicator
}
