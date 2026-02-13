package indicators

import (
	"time"

	"github.com/shopspring/decimal"
)

// IndicatorProvider defines the interface for technical indicator calculations.
// This abstraction allows strategy and signal code to switch between different
// indicator implementations (e.g., current talib wrapper vs GoFlux) without
// changing business logic.
type IndicatorProvider interface {
	// Trend Indicators
	SMA(prices []decimal.Decimal, period int) []decimal.Decimal
	EMA(prices []decimal.Decimal, period int) []decimal.Decimal

	// Momentum Indicators
	RSI(prices []decimal.Decimal, period int) []decimal.Decimal
	Stochastic(high, low, close []decimal.Decimal, kPeriod, dPeriod int) (k, d []decimal.Decimal)

	// Trend/Momentum Indicators
	MACD(prices []decimal.Decimal, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram []decimal.Decimal)

	// Volatility Indicators
	BollingerBands(prices []decimal.Decimal, period int, stdDev float64) (upper, middle, lower []decimal.Decimal)
	ATR(high, low, close []decimal.Decimal, period int) []decimal.Decimal

	// Volume Indicators
	OBV(prices, volumes []decimal.Decimal) []decimal.Decimal
	VWAP(high, low, close, volume []decimal.Decimal) []decimal.Decimal

	// Provider metadata
	Name() string
	Version() string
}

// IndicatorType represents the category of a technical indicator.
type IndicatorType string

const (
	IndicatorTypeTrend      IndicatorType = "trend"
	IndicatorTypeMomentum   IndicatorType = "momentum"
	IndicatorTypeVolatility IndicatorType = "volatility"
	IndicatorTypeVolume     IndicatorType = "volume"
)

// IndicatorMetadata provides information about a specific indicator.
type IndicatorMetadata struct {
	Name        string        `json:"name"`
	Type        IndicatorType `json:"type"`
	Description string        `json:"description"`
	Parameters  []Parameter   `json:"parameters"`
}

// Parameter describes a configurable parameter for an indicator.
type Parameter struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // "int", "float", "bool"
	Default     interface{} `json:"default"`
	Min         interface{} `json:"min,omitempty"`
	Max         interface{} `json:"max,omitempty"`
	Description string      `json:"description"`
}

// IndicatorResult represents the output of a single indicator calculation.
type IndicatorResult struct {
	Name       string            `json:"name"`
	Type       IndicatorType     `json:"type"`
	Values     []decimal.Decimal `json:"values"`
	Signal     SignalType        `json:"signal"`
	Strength   decimal.Decimal   `json:"strength"`
	Timestamp  time.Time         `json:"timestamp"`
	Parameters map[string]any    `json:"parameters,omitempty"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
}

// SignalType represents the trading signal from an indicator.
type SignalType string

const (
	SignalBuy    SignalType = "buy"
	SignalSell   SignalType = "sell"
	SignalHold   SignalType = "hold"
	SignalStrong SignalType = "strong"
	SignalWeak   SignalType = "weak"
)

// MultiIndicatorResult aggregates results from multiple indicators.
type MultiIndicatorResult struct {
	Symbol        string             `json:"symbol"`
	Exchange      string             `json:"exchange"`
	Timeframe     string             `json:"timeframe"`
	Indicators    []*IndicatorResult `json:"indicators"`
	OverallSignal SignalType         `json:"overall_signal"`
	Confidence    decimal.Decimal    `json:"confidence"`
	CalculatedAt  time.Time          `json:"calculated_at"`
}

// IndicatorConfig holds configuration for indicator calculations.
type IndicatorConfig struct {
	// Moving Averages
	SMAPeriods []int `json:"sma_periods"`
	EMAPeriods []int `json:"ema_periods"`

	// Momentum Indicators
	RSIPeriod    int `json:"rsi_period"`
	StochKPeriod int `json:"stoch_k_period"`
	StochDPeriod int `json:"stoch_d_period"`

	// Trend Indicators
	MACDFast   int `json:"macd_fast"`
	MACDSlow   int `json:"macd_slow"`
	MACDSignal int `json:"macd_signal"`

	// Volatility Indicators
	BBPeriod  int     `json:"bb_period"`
	BBStdDev  float64 `json:"bb_std_dev"`
	ATRPeriod int     `json:"atr_period"`

	// Volume Indicators
	OBVEnabled  bool `json:"obv_enabled"`
	VWAPEnabled bool `json:"vwap_enabled"`
}

// DefaultIndicatorConfig returns a standard configuration for indicators.
func DefaultIndicatorConfig() *IndicatorConfig {
	return &IndicatorConfig{
		SMAPeriods:   []int{10, 20, 50},
		EMAPeriods:   []int{12, 26},
		RSIPeriod:    14,
		StochKPeriod: 14,
		StochDPeriod: 3,
		MACDFast:     12,
		MACDSlow:     26,
		MACDSignal:   9,
		BBPeriod:     20,
		BBStdDev:     2.0,
		ATRPeriod:    14,
		OBVEnabled:   true,
		VWAPEnabled:  true,
	}
}

// OHLCVData represents OHLCV (Open-High-Low-Close-Volume) price data.
type OHLCVData struct {
	Symbol     string            `json:"symbol"`
	Exchange   string            `json:"exchange"`
	Timeframe  string            `json:"timeframe"`
	Timestamps []time.Time       `json:"timestamps"`
	Open       []decimal.Decimal `json:"open"`
	High       []decimal.Decimal `json:"high"`
	Low        []decimal.Decimal `json:"low"`
	Close      []decimal.Decimal `json:"close"`
	Volume     []decimal.Decimal `json:"volume"`
}

// Validate checks if the OHLCV data is valid and consistent.
func (d *OHLCVData) Validate() error {
	n := len(d.Close)
	if n == 0 {
		return ErrEmptyData
	}

	if len(d.Open) != n || len(d.High) != n || len(d.Low) != n || len(d.Volume) != n {
		return ErrInconsistentData
	}

	if len(d.Timestamps) > 0 && len(d.Timestamps) != n {
		return ErrInconsistentData
	}

	return nil
}

// Length returns the number of data points.
func (d *OHLCVData) Length() int {
	return len(d.Close)
}

// Last returns the most recent data point.
func (d *OHLCVData) Last() (open, high, low, close, volume decimal.Decimal, ok bool) {
	if len(d.Close) == 0 {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, false
	}
	n := len(d.Close) - 1
	return d.Open[n], d.High[n], d.Low[n], d.Close[n], d.Volume[n], true
}

// Slice returns a subset of the data.
func (d *OHLCVData) Slice(start, end int) *OHLCVData {
	if start < 0 {
		start = 0
	}
	if end > len(d.Close) {
		end = len(d.Close)
	}
	if start >= end {
		return &OHLCVData{}
	}

	sliced := &OHLCVData{
		Symbol:   d.Symbol,
		Exchange: d.Exchange,
		Open:     d.Open[start:end],
		High:     d.High[start:end],
		Low:      d.Low[start:end],
		Close:    d.Close[start:end],
		Volume:   d.Volume[start:end],
	}

	if len(d.Timestamps) > 0 {
		sliced.Timestamps = d.Timestamps[start:end]
	}

	return sliced
}

// ToFloats converts decimal slices to float64 slices for compatibility
// with indicator libraries that require float64 input.
func (d *OHLCVData) ToFloats() (open, high, low, close, volume []float64) {
	n := d.Length()
	open = make([]float64, n)
	high = make([]float64, n)
	low = make([]float64, n)
	close = make([]float64, n)
	volume = make([]float64, n)

	for i := 0; i < n; i++ {
		open[i], _ = d.Open[i].Float64()
		high[i], _ = d.High[i].Float64()
		low[i], _ = d.Low[i].Float64()
		close[i], _ = d.Close[i].Float64()
		volume[i], _ = d.Volume[i].Float64()
	}

	return open, high, low, close, volume
}

// IndicatorError represents an error from indicator calculations.
type IndicatorError struct {
	Indicator string
	Message   string
	Cause     error
}

func (e *IndicatorError) Error() string {
	if e.Cause != nil {
		return e.Indicator + ": " + e.Message + ": " + e.Cause.Error()
	}
	return e.Indicator + ": " + e.Message
}

func (e *IndicatorError) Unwrap() error {
	return e.Cause
}

// Common indicator errors.
var (
	ErrEmptyData        = &IndicatorError{Message: "empty data"}
	ErrInconsistentData = &IndicatorError{Message: "inconsistent data lengths"}
	ErrInsufficientData = &IndicatorError{Message: "insufficient data points"}
	ErrInvalidPeriod    = &IndicatorError{Message: "invalid period"}
	ErrInvalidParameter = &IndicatorError{Message: "invalid parameter"}
)

// NewIndicatorError creates a new indicator error.
func NewIndicatorError(indicator, message string, cause error) *IndicatorError {
	return &IndicatorError{
		Indicator: indicator,
		Message:   message,
		Cause:     cause,
	}
}
