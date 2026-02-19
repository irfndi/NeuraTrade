package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
)

func TestStreamingSMA(t *testing.T) {
	sma := indicators.NewStreamingSMA(3)

	v1 := sma.Next(decimal.New(100))
	assert.Equal(t, "100.00", v1.FormattedString(2))

	v2 := sma.Next(decimal.New(110))
	assert.Equal(t, "105.00", v2.FormattedString(2)) // (100+110)/2

	v3 := sma.Next(decimal.New(120))
	assert.Equal(t, "110.00", v3.FormattedString(2)) // (100+110+120)/3

	v4 := sma.Next(decimal.New(130))
	assert.Equal(t, "120.00", v4.FormattedString(2)) // (110+120+130)/3
}

func TestStreamingEMA(t *testing.T) {
	ema := indicators.NewStreamingEMA(9) // window 9 => alpha = 2/10 = 0.2

	v1 := ema.Next(decimal.New(100))
	assert.Equal(t, "100.00", v1.FormattedString(2))

	v2 := ema.Next(decimal.New(110))
	// 110 * 0.2 + 100 * 0.8 = 22 + 80 = 102
	assert.Equal(t, "102.00", v2.FormattedString(2))
}
