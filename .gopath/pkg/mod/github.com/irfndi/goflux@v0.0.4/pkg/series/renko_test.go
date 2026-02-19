package series_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestRenko(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 102, 105, 103, 110, 104)
	brickSize := decimal.New(2)

	renko := series.Renko(ts, brickSize)

	// 100 -> 102: +1 brick (Close 102)
	// 102 -> 105: +1 brick (Close 104)
	// 104 -> 103: no brick
	// 103 -> 110: +3 bricks (Close 106, 108, 110)
	// 110 -> 104: -3 bricks (Close 108, 106, 104)

	// Total bricks: 1 + 1 + 3 + 3 = 8
	assert.Equal(t, 8, renko.Length())

	assert.Equal(t, "100.00", renko.Candles[0].OpenPrice.FormattedString(2))
	assert.Equal(t, "102.00", renko.Candles[0].ClosePrice.FormattedString(2))

	assert.Equal(t, "102.00", renko.Candles[1].OpenPrice.FormattedString(2))
	assert.Equal(t, "104.00", renko.Candles[1].ClosePrice.FormattedString(2))

	assert.Equal(t, "104.00", renko.Candles[2].OpenPrice.FormattedString(2))
	assert.Equal(t, "106.00", renko.Candles[2].ClosePrice.FormattedString(2))
}
