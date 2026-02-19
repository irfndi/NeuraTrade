package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
)

func TestGenericSMA_Float64(t *testing.T) {
	values := []float64{10, 20, 30, 40, 50}
	fi := indicators.NewFloatIndicator(values)

	add := func(a, b float64) float64 { return a + b }
	div := func(a float64, b float64) float64 { return a / b }

	sma := indicators.NewGenericSMA[float64](fi, 3, 0.0, add, div)

	// SMA(3) at index 2: (10+20+30)/3 = 20
	assert.Equal(t, 20.0, sma.Calculate(2))
	// SMA(3) at index 4: (30+40+50)/3 = 40
	assert.Equal(t, 40.0, sma.Calculate(4))
}
