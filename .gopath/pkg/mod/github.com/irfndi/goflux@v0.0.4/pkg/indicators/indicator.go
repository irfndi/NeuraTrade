package indicators

import "github.com/irfndi/goflux/pkg/decimal"

// Indicator is an interface that describes a methodology by which to analyze a trading record for a specific property
// or trend. For example. MovingAverageIndicator implements the Indicator interface and, for a given index in the timeSeries,
// returns the current moving average of the prices in that series.
type Indicator interface {
	Calculate(int) decimal.Decimal
}

// GenericIndicator is a generic interface for indicators
type GenericIndicator[T any] interface {
	Calculate(int) T
}

// SelfDescribingIndicator is an Indicator that can describe its requirements and properties
type SelfDescribingIndicator interface {
	Indicator
	Lookback() int
	Metadata() IndicatorMetadata
}
