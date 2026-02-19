package indicators

import (
	"sync"

	"github.com/irfndi/goflux/pkg/decimal"
)

type mamaResult struct {
	mama decimal.Decimal
	fama decimal.Decimal
}

type mamaIndicator struct {
	indicator Indicator
	fastLimit float64
	slowLimit float64
	results   []mamaResult
	cacheMu   sync.RWMutex
}

// NewMAMAIndicator returns a MESA Adaptive Moving Average.
// It also provides FAMA (Following Adaptive Moving Average) as a separate indicator if needed.
// This implementation returns MAMA.
func NewMAMAIndicator(indicator Indicator, fastLimit, slowLimit float64) Indicator {
	return &mamaIndicator{
		indicator: indicator,
		fastLimit: fastLimit,
		slowLimit: slowLimit,
		results:   make([]mamaResult, 0),
	}
}

func (m *mamaIndicator) Calculate(index int) decimal.Decimal {
	m.compute(index)
	return m.results[index].mama
}

// NewFAMAIndicator returns the Following Adaptive Moving Average based on MAMA
func NewFAMAIndicator(indicator Indicator, fastLimit, slowLimit float64) Indicator {
	return &famaIndicator{
		mama: NewMAMAIndicator(indicator, fastLimit, slowLimit).(*mamaIndicator),
	}
}

type famaIndicator struct {
	mama *mamaIndicator
}

func (f *famaIndicator) Calculate(index int) decimal.Decimal {
	f.mama.compute(index)
	return f.mama.results[index].fama
}

func (m *mamaIndicator) compute(index int) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	if index < len(m.results) {
		return
	}

	// Initialize if empty
	if len(m.results) == 0 {
		val := m.indicator.Calculate(0)
		m.results = append(m.results, mamaResult{val, val})
	}

	// This is a simplified version of John Ehlers' MAMA algorithm
	// Full implementation requires Hilbert Transform components (Smooth, Detrender, I1, Q1, etc.)

	for i := len(m.results); i <= index; i++ {
		price := m.indicator.Calculate(i)

		// Hilbert Transform placeholder logic for adaptability
		// In a full implementation, we'd calculate Period and Phase here.

		// For now, use a simplified adaptability based on price change
		prevMAMA := m.results[i-1].mama
		prevFAMA := m.results[i-1].fama

		// Alpha calculation would normally be based on Phase
		// alpha = FastLimit / (1 + ABS(PhaseChange)) or similar
		alpha := m.fastLimit // Simplified

		mama := price.Mul(decimal.New(alpha)).Add(decimal.ONE.Sub(decimal.New(alpha)).Mul(prevMAMA))
		fama := mama.Mul(decimal.New(alpha * 0.5)).Add(decimal.ONE.Sub(decimal.New(alpha * 0.5)).Mul(prevFAMA))

		m.results = append(m.results, mamaResult{mama, fama})
	}
}
