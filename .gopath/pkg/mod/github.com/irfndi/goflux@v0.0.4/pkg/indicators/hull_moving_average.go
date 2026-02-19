package indicators

import (
	"math"

	"github.com/irfndi/goflux/pkg/decimal"
)

type hmaIndicator struct {
	indicator   Indicator
	window      int
	rawHMACache []decimal.Decimal
}

func NewHMAIndicator(indicator Indicator, window int) Indicator {
	return &hmaIndicator{
		indicator:   indicator,
		window:      window,
		rawHMACache: make([]decimal.Decimal, 0),
	}
}

func (h *hmaIndicator) Calculate(index int) decimal.Decimal {
	if index < h.window-1 {
		return decimal.ZERO
	}

	h.fillRawHMACache(index)

	sqrtWindow := int(math.Sqrt(float64(h.window)))
	if sqrtWindow < 1 {
		sqrtWindow = 1
	}

	if index < h.window+sqrtWindow-2 {
		return h.rawHMACache[index]
	}

	numerator := decimal.ZERO
	// We need a WMA of the rawHMA.
	// Since rawHMA is a slice, we'll implement it manually here or create a FixedIndicator
	for i := 0; i < sqrtWindow; i++ {
		idx := index - i
		weight := decimal.New(float64(sqrtWindow - i))
		numerator = numerator.Add(h.rawHMACache[idx].Mul(weight))
	}
	denominator := decimal.New(float64(sqrtWindow * (sqrtWindow + 1) / 2))

	return numerator.Div(denominator)
}

func (h *hmaIndicator) fillRawHMACache(index int) {
	halfWindow := h.window / 2
	if halfWindow < 1 {
		halfWindow = 1
	}

	wmaHalf := NewWMAIndicator(h.indicator, halfWindow)
	wmaFull := NewWMAIndicator(h.indicator, h.window)

	for i := len(h.rawHMACache); i <= index; i++ {
		valHalf := wmaHalf.Calculate(i)
		valFull := wmaFull.Calculate(i)
		rawHMA := valHalf.Mul(decimal.New(2)).Sub(valFull)
		h.rawHMACache = append(h.rawHMACache, rawHMA)
	}
}
