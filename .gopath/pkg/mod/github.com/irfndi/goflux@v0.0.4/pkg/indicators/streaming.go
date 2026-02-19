package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

// StreamingIndicator is an interface for indicators that can be updated with new values in real-time
type StreamingIndicator interface {
	Indicator
	Next(val decimal.Decimal) decimal.Decimal
}

// StreamingSMA is a streaming version of SMA
type StreamingSMA struct {
	window int
	values []decimal.Decimal
	sum    decimal.Decimal
}

func NewStreamingSMA(window int) *StreamingSMA {
	return &StreamingSMA{
		window: window,
		values: make([]decimal.Decimal, 0, window),
		sum:    decimal.ZERO,
	}
}

func (s *StreamingSMA) Calculate(index int) decimal.Decimal {
	// Not really applicable for streaming but we implement it for compatibility
	return decimal.ZERO
}

func (s *StreamingSMA) Next(val decimal.Decimal) decimal.Decimal {
	if len(s.values) >= s.window {
		s.sum = s.sum.Sub(s.values[0])
		s.values = s.values[1:]
	}
	s.values = append(s.values, val)
	s.sum = s.sum.Add(val)

	return s.sum.Div(decimal.New(float64(len(s.values))))
}

// StreamingEMA is a streaming version of EMA
type StreamingEMA struct {
	window    int
	alpha     decimal.Decimal
	lastValue decimal.Decimal
	isFirst   bool
}

func NewStreamingEMA(window int) *StreamingEMA {
	return &StreamingEMA{
		window:  window,
		alpha:   decimal.New(2).Div(decimal.New(float64(window + 1))),
		isFirst: true,
	}
}

func (s *StreamingEMA) Calculate(index int) decimal.Decimal {
	return s.lastValue
}

func (s *StreamingEMA) Next(val decimal.Decimal) decimal.Decimal {
	if s.isFirst {
		s.lastValue = val
		s.isFirst = false
		return val
	}

	// EMA = alpha * val + (1 - alpha) * lastValue
	s.lastValue = val.Mul(s.alpha).Add(s.lastValue.Mul(decimal.ONE.Sub(s.alpha)))
	return s.lastValue
}
