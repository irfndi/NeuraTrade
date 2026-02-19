package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

// Numeric is a constraint for types that support basic arithmetic
type Numeric interface {
	~float64 | ~int | ~int64 | decimal.Decimal
}

// GenericSMA is a generic Simple Moving Average
type GenericSMA[T Numeric] struct {
	indicator GenericIndicator[T]
	window    int
	add       func(T, T) T
	div       func(T, float64) T
	zero      T
}

func NewGenericSMA[T Numeric](indicator GenericIndicator[T], window int, zero T, add func(T, T) T, div func(T, float64) T) *GenericSMA[T] {
	return &GenericSMA[T]{
		indicator: indicator,
		window:    window,
		add:       add,
		div:       div,
		zero:      zero,
	}
}

func (s *GenericSMA[T]) Calculate(index int) T {
	if index < s.window-1 {
		return s.zero
	}

	sum := s.zero
	for i := index; i > index-s.window; i-- {
		sum = s.add(sum, s.indicator.Calculate(i))
	}

	return s.div(sum, float64(s.window))
}

// FloatIndicator wraps a slice of float64 as a GenericIndicator
type FloatIndicator struct {
	values []float64
}

func (fi FloatIndicator) Calculate(index int) float64 {
	if index < 0 || index >= len(fi.values) {
		return 0
	}
	return fi.values[index]
}

func NewFloatIndicator(values []float64) FloatIndicator {
	return FloatIndicator{values: values}
}
